package oauth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// CallbackResult holds the result of a single OAuth callback request.
type CallbackResult struct {
	Code  string
	State string
	Error string
}

// StartCallbackServer starts a local HTTP server on the specified port (or a
// random port when requestedPort is 0). It serves GET /callback and
// GET /auth/callback, extracts "code", "state", and "error" query parameters,
// sends the result on the returned channel, and then shuts down. The caller
// must invoke shutdown() when done (or rely on ctx cancellation) to release
// the port.
//
// The returned port is the actual port the listener bound to.
func StartCallbackServer(ctx context.Context, requestedPort int) (port int, result <-chan CallbackResult, shutdown func(), err error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", requestedPort))
	if err != nil {
		return 0, nil, nil, fmt.Errorf("oauth/callback: listen: %w", err)
	}
	port = ln.Addr().(*net.TCPAddr).Port

	ch := make(chan CallbackResult, 1)

	mux := http.NewServeMux()
	srv := &http.Server{
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	callbackHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		q := r.URL.Query()
		res := CallbackResult{
			Code:  q.Get("code"),
			State: q.Get("state"),
			Error: q.Get("error"),
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `<!DOCTYPE html>
<html>
<head><title>Authorization complete</title></head>
<body>
<h2>Authorization complete</h2>
<p>You can close this tab and return to the terminal.</p>
</body>
</html>`)

		// Non-blocking send: the channel is buffered with capacity 1.
		select {
		case ch <- res:
		default:
		}

		// Shut down the server in the background so the response finishes.
		go func() { _ = srv.Shutdown(context.Background()) }()
	}
	mux.HandleFunc("/callback", callbackHandler)
	mux.HandleFunc("/auth/callback", callbackHandler)

	shutdownFn := func() {
		ctx2, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx2)
	}

	go func() {
		_ = srv.Serve(ln)
	}()

	// Context cancellation shuts the server and closes the result channel.
	go func() {
		<-ctx.Done()
		shutdownFn()
		// Drain any pending result so downstream receivers don't block.
		select {
		case ch <- CallbackResult{Error: "context canceled"}:
		default:
		}
	}()

	return port, ch, shutdownFn, nil
}
