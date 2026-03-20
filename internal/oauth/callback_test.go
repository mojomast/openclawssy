package oauth

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestCallbackServer_SuccessfulCallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	port, results, shutdown, err := StartCallbackServer(ctx, 0)
	if err != nil {
		t.Fatalf("StartCallbackServer: %v", err)
	}
	defer shutdown()

	url := fmt.Sprintf("http://127.0.0.1:%d/callback?code=auth_code_123&state=state_xyz", port)
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		t.Fatalf("GET /callback: %v", err)
	}
	resp.Body.Close()

	select {
	case res := <-results:
		if res.Code != "auth_code_123" {
			t.Errorf("Code: got %q, want %q", res.Code, "auth_code_123")
		}
		if res.State != "state_xyz" {
			t.Errorf("State: got %q, want %q", res.State, "state_xyz")
		}
		if res.Error != "" {
			t.Errorf("Error: got %q, want empty", res.Error)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestCallbackServer_ErrorParam(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	port, results, shutdown, err := StartCallbackServer(ctx, 0)
	if err != nil {
		t.Fatalf("StartCallbackServer: %v", err)
	}
	defer shutdown()

	url := fmt.Sprintf("http://127.0.0.1:%d/callback?error=access_denied&state=s1", port)
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		t.Fatalf("GET /callback: %v", err)
	}
	resp.Body.Close()

	select {
	case res := <-results:
		if res.Error != "access_denied" {
			t.Errorf("Error: got %q, want %q", res.Error, "access_denied")
		}
		if res.Code != "" {
			t.Errorf("Code: got %q, want empty", res.Code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestCallbackServer_ShutdownAfterCallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	port, results, shutdown, err := StartCallbackServer(ctx, 0)
	if err != nil {
		t.Fatalf("StartCallbackServer: %v", err)
	}
	defer shutdown()

	url := fmt.Sprintf("http://127.0.0.1:%d/callback?code=shutdown_test", port)
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		t.Fatalf("GET /callback: %v", err)
	}
	resp.Body.Close()

	// Wait for the result to confirm the callback was received.
	select {
	case <-results:
		// Good.
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}

	// After the callback is handled, the server shuts down; subsequent requests
	// should fail (connection refused or similar).
	// Give the server a moment to shut down.
	time.Sleep(100 * time.Millisecond)

	_, err = http.Get(url) //nolint:noctx
	if err == nil {
		t.Error("expected error after server shutdown, got nil")
	}
}
