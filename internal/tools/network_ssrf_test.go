package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"openclawssy/internal/config"
)

// TestSSRFProtection verifies that domains resolving to localhost are blocked
// when AllowLocalhosts is false, even if the domain is allowlisted.
func TestSSRFProtection(t *testing.T) {
	// 127.0.0.1.nip.io resolves to 127.0.0.1
	// If the environment has no DNS, this test might fail to resolve, which is fine (it won't connect).
	// But if it resolves, it should be blocked by our logic if we fix it.
	// Currently, it is likely ALLOWED.

	dangerousDomain := "127.0.0.1.nip.io"

	// Create a local server that behaves like a sensitive internal service
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("secret internal data"))
	}))
	defer server.Close()

	// extracting the port from the httptest server to form our dangerous URL
	// server.URL is like http://127.0.0.1:12345
	// We want http://127.0.0.1.nip.io:12345
	parts := strings.Split(server.URL, ":")
	port := parts[len(parts)-1]
	dangerousURL := "http://" + dangerousDomain + ":" + port

	// Setup registry with network enabled but localhost disallowed
	ws, _, reg := setupNetworkToolRegistry(t, func(cfg *config.Config) {
		cfg.Network.Enabled = true
		cfg.Network.AllowLocalhosts = false
		// We explicitly allow the dangerous domain to simulate a bypass attempt
		// or a misconfiguration where the user trusts the domain but not the IP it points to.
		cfg.Network.AllowedDomains = []string{dangerousDomain}
	})

	// Try to access the dangerous URL using http.request tool
	res, err := reg.Execute(context.Background(), "agent", "http.request", ws, map[string]any{
		"url": dangerousURL,
	})

	if err == nil {
		t.Logf("Response: %+v", res)
		t.Fatalf("VULNERABILITY CONFIRMED: Successfully accessed localhost via %s when AllowLocalhosts=false", dangerousDomain)
	}

	// If it failed, check error message
	errStr := err.Error()
	if strings.Contains(errStr, "dial tcp") && strings.Contains(errStr, "no such host") {
		t.Skipf("Skipping SSRF test: DNS resolution for %s failed", dangerousDomain)
	}

	// We expect a specific error message once fixed
	if !strings.Contains(errStr, "resolves to loopback/private IP") && !strings.Contains(errStr, "localhost/loopback") {
		t.Logf("Got error: %v", err)
	}
}
