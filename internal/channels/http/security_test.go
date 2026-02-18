package httpchannel

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurity_PathTraversalBypass(t *testing.T) {
	s := NewServer(Config{BearerToken: "secret", Store: NewInMemoryRunStore()})

	// Create a request with a path that exploits the vulnerability
	req := httptest.NewRequest(http.MethodGet, "/dashboard/static/../v1/runs", nil)
	// Do NOT set Authorization header

	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)

	// If vulnerable, it returns 200 OK (empty list of runs) or 500
	// If fixed, it should return 401 Unauthorized because /v1/runs requires auth
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("Vulnerability confirmed: expected 401 Unauthorized, got %d. Response: %s", rr.Code, rr.Body.String())
	}
}
