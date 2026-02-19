package httpchannel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── Mock service ──────────────────────────────────────────────────────────────

// mockDockerAdminService is a test double for DockerAdminService.
// Each method's behaviour can be overridden per-test via the corresponding
// field; leaving a field nil makes the method return a zero value + nil error.
type mockDockerAdminService struct {
	statusFn          func(ctx context.Context, agentID string) (DockerStatusResponse, error)
	createContainerFn func(ctx context.Context, agentID string) error
	stopContainerFn   func(ctx context.Context, agentID string) error
	resetContainerFn  func(ctx context.Context, agentID string) error
	pullImageFn       func(ctx context.Context, image string) error
	listImagesFn      func(ctx context.Context) ([]DockerImageInfo, error)
	listVolumesFn     func(ctx context.Context) ([]DockerVolumeInfo, error)
	deleteVolumeFn    func(ctx context.Context, name string) error
}

func (m *mockDockerAdminService) Status(ctx context.Context, agentID string) (DockerStatusResponse, error) {
	if m.statusFn != nil {
		return m.statusFn(ctx, agentID)
	}
	return DockerStatusResponse{AgentID: agentID, Status: "running", Running: true}, nil
}

func (m *mockDockerAdminService) CreateContainer(ctx context.Context, agentID string) error {
	if m.createContainerFn != nil {
		return m.createContainerFn(ctx, agentID)
	}
	return nil
}

func (m *mockDockerAdminService) StopContainer(ctx context.Context, agentID string) error {
	if m.stopContainerFn != nil {
		return m.stopContainerFn(ctx, agentID)
	}
	return nil
}

func (m *mockDockerAdminService) ResetContainer(ctx context.Context, agentID string) error {
	if m.resetContainerFn != nil {
		return m.resetContainerFn(ctx, agentID)
	}
	return nil
}

func (m *mockDockerAdminService) PullImage(ctx context.Context, image string) error {
	if m.pullImageFn != nil {
		return m.pullImageFn(ctx, image)
	}
	return nil
}

func (m *mockDockerAdminService) ListImages(ctx context.Context) ([]DockerImageInfo, error) {
	if m.listImagesFn != nil {
		return m.listImagesFn(ctx)
	}
	return []DockerImageInfo{{ID: "abc123", Repository: "ubuntu", Tag: "24.04", Size: "80MB"}}, nil
}

func (m *mockDockerAdminService) ListVolumes(ctx context.Context) ([]DockerVolumeInfo, error) {
	if m.listVolumesFn != nil {
		return m.listVolumesFn(ctx)
	}
	return []DockerVolumeInfo{{Name: "openclawssy_ws_default", Driver: "local"}}, nil
}

func (m *mockDockerAdminService) DeleteVolume(ctx context.Context, name string) error {
	if m.deleteVolumeFn != nil {
		return m.deleteVolumeFn(ctx, name)
	}
	return nil
}

// ── Helper ────────────────────────────────────────────────────────────────────

// newTestServerWithSandboxAdmin builds a full server (with auth middleware) that
// has the SandboxAdminHandler registered.  token is the bearer token required.
func newTestServerWithSandboxAdmin(t *testing.T, svc DockerAdminService, token string) *Server {
	t.Helper()
	handler := NewSandboxAdminHandler(svc)
	return NewServer(Config{
		BearerToken: token,
		Store:       NewInMemoryRunStore(),
		RegisterMux: func(mux *http.ServeMux) {
			handler.Register(mux)
		},
	})
}

// do executes a request against the server handler and returns the recorder.
func doRequest(t *testing.T, srv *Server, method, path, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var bodyReader *bytes.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	} else {
		bodyReader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	return rr
}

// decodeBody unmarshals the response body into a map for flexible assertions.
func decodeBody(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("failed to decode response body %q: %v", rr.Body.String(), err)
	}
	return out
}

// ── 401 auth tests ────────────────────────────────────────────────────────────

func TestSandboxAdmin_AuthRequired(t *testing.T) {
	type endpoint struct {
		method string
		path   string
		body   []byte
	}

	endpoints := []endpoint{
		{http.MethodGet, "/api/admin/sandbox/docker/status", nil},
		{http.MethodPost, "/api/admin/sandbox/docker/create", []byte(`{"agent_id":"x"}`)},
		{http.MethodPost, "/api/admin/sandbox/docker/stop", []byte(`{"agent_id":"x"}`)},
		{http.MethodPost, "/api/admin/sandbox/docker/reset", []byte(`{"agent_id":"x"}`)},
		{http.MethodPost, "/api/admin/sandbox/docker/pull", []byte(`{"image":"ubuntu:24.04"}`)},
		{http.MethodGet, "/api/admin/sandbox/docker/images", nil},
		{http.MethodGet, "/api/admin/sandbox/docker/volumes", nil},
		{http.MethodDelete, "/api/admin/sandbox/docker/volume", []byte(`{"name":"myvol"}`)},
	}

	svc := &mockDockerAdminService{}
	srv := newTestServerWithSandboxAdmin(t, svc, "secret-token")

	for _, ep := range endpoints {
		t.Run("no_token_"+ep.method+"_"+ep.path, func(t *testing.T) {
			rr := doRequest(t, srv, ep.method, ep.path, "", ep.body)
			if rr.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d (body=%s)", rr.Code, rr.Body.String())
			}
		})

		t.Run("wrong_token_"+ep.method+"_"+ep.path, func(t *testing.T) {
			rr := doRequest(t, srv, ep.method, ep.path, "wrong-token", ep.body)
			if rr.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d (body=%s)", rr.Code, rr.Body.String())
			}
		})
	}
}

// ── Success cases ─────────────────────────────────────────────────────────────

func TestSandboxAdmin_Status_Success(t *testing.T) {
	svc := &mockDockerAdminService{
		statusFn: func(_ context.Context, agentID string) (DockerStatusResponse, error) {
			return DockerStatusResponse{
				AgentID:       agentID,
				ContainerName: "openclawssy_agent_" + agentID,
				Status:        "running",
				Running:       true,
				VolumeName:    "openclawssy_ws_" + agentID,
				WorkspacePath: "/workspace",
			}, nil
		},
	}
	srv := newTestServerWithSandboxAdmin(t, svc, "tok")

	rr := doRequest(t, srv, http.MethodGet, "/api/admin/sandbox/docker/status?agent_id=myagent", "tok", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := decodeBody(t, rr)
	if body["agent_id"] != "myagent" {
		t.Errorf("expected agent_id=myagent, got %v", body["agent_id"])
	}
	if body["running"] != true {
		t.Errorf("expected running=true, got %v", body["running"])
	}
	if body["status"] != "running" {
		t.Errorf("expected status=running, got %v", body["status"])
	}
}

func TestSandboxAdmin_Status_DefaultAgentID(t *testing.T) {
	var capturedID string
	svc := &mockDockerAdminService{
		statusFn: func(_ context.Context, agentID string) (DockerStatusResponse, error) {
			capturedID = agentID
			return DockerStatusResponse{AgentID: agentID, Status: "not_found"}, nil
		},
	}
	srv := newTestServerWithSandboxAdmin(t, svc, "tok")

	rr := doRequest(t, srv, http.MethodGet, "/api/admin/sandbox/docker/status", "tok", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if capturedID != "default" {
		t.Errorf("expected capturedID=default, got %q", capturedID)
	}
}

func TestSandboxAdmin_Create_Success(t *testing.T) {
	var capturedID string
	svc := &mockDockerAdminService{
		createContainerFn: func(_ context.Context, agentID string) error {
			capturedID = agentID
			return nil
		},
	}
	srv := newTestServerWithSandboxAdmin(t, svc, "tok")

	body, _ := json.Marshal(map[string]string{"agent_id": "agent-1"})
	rr := doRequest(t, srv, http.MethodPost, "/api/admin/sandbox/docker/create", "tok", body)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	if capturedID != "agent-1" {
		t.Errorf("expected capturedID=agent-1, got %q", capturedID)
	}
	resp := decodeBody(t, rr)
	if resp["ok"] != true {
		t.Errorf("expected ok=true, got %v", resp["ok"])
	}
	if resp["action"] != "create" {
		t.Errorf("expected action=create, got %v", resp["action"])
	}
}

func TestSandboxAdmin_Stop_Success(t *testing.T) {
	svc := &mockDockerAdminService{}
	srv := newTestServerWithSandboxAdmin(t, svc, "tok")

	body, _ := json.Marshal(map[string]string{"agent_id": "agent-2"})
	rr := doRequest(t, srv, http.MethodPost, "/api/admin/sandbox/docker/stop", "tok", body)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	resp := decodeBody(t, rr)
	if resp["action"] != "stop" {
		t.Errorf("expected action=stop, got %v", resp["action"])
	}
}

func TestSandboxAdmin_Reset_Success(t *testing.T) {
	svc := &mockDockerAdminService{}
	srv := newTestServerWithSandboxAdmin(t, svc, "tok")

	body, _ := json.Marshal(map[string]string{"agent_id": "agent-3"})
	rr := doRequest(t, srv, http.MethodPost, "/api/admin/sandbox/docker/reset", "tok", body)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	resp := decodeBody(t, rr)
	if resp["action"] != "reset" {
		t.Errorf("expected action=reset, got %v", resp["action"])
	}
}

func TestSandboxAdmin_Pull_Success(t *testing.T) {
	var pulledImage string
	svc := &mockDockerAdminService{
		pullImageFn: func(_ context.Context, image string) error {
			pulledImage = image
			return nil
		},
	}
	srv := newTestServerWithSandboxAdmin(t, svc, "tok")

	body, _ := json.Marshal(map[string]string{"image": "ubuntu:24.04"})
	rr := doRequest(t, srv, http.MethodPost, "/api/admin/sandbox/docker/pull", "tok", body)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if pulledImage != "ubuntu:24.04" {
		t.Errorf("expected pulledImage=ubuntu:24.04, got %q", pulledImage)
	}
	resp := decodeBody(t, rr)
	if resp["action"] != "pull" {
		t.Errorf("expected action=pull, got %v", resp["action"])
	}
}

func TestSandboxAdmin_Images_Success(t *testing.T) {
	svc := &mockDockerAdminService{
		listImagesFn: func(_ context.Context) ([]DockerImageInfo, error) {
			return []DockerImageInfo{
				{ID: "abc", Repository: "ubuntu", Tag: "24.04", Size: "80MB"},
				{ID: "def", Repository: "alpine", Tag: "latest", Size: "10MB"},
			}, nil
		},
	}
	srv := newTestServerWithSandboxAdmin(t, svc, "tok")

	rr := doRequest(t, srv, http.MethodGet, "/api/admin/sandbox/docker/images", "tok", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	resp := decodeBody(t, rr)
	count, _ := resp["count"].(float64)
	if count != 2 {
		t.Errorf("expected count=2, got %v", resp["count"])
	}
	images, ok := resp["images"].([]any)
	if !ok || len(images) != 2 {
		t.Errorf("expected 2 images in response, got %v", resp["images"])
	}
}

func TestSandboxAdmin_Images_EmptyList(t *testing.T) {
	svc := &mockDockerAdminService{
		listImagesFn: func(_ context.Context) ([]DockerImageInfo, error) {
			return nil, nil
		},
	}
	srv := newTestServerWithSandboxAdmin(t, svc, "tok")

	rr := doRequest(t, srv, http.MethodGet, "/api/admin/sandbox/docker/images", "tok", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	resp := decodeBody(t, rr)
	images, ok := resp["images"].([]any)
	if !ok {
		t.Fatalf("expected images array in response, got %T: %v", resp["images"], resp["images"])
	}
	if len(images) != 0 {
		t.Errorf("expected 0 images, got %d", len(images))
	}
}

func TestSandboxAdmin_Volumes_Success(t *testing.T) {
	svc := &mockDockerAdminService{
		listVolumesFn: func(_ context.Context) ([]DockerVolumeInfo, error) {
			return []DockerVolumeInfo{
				{Name: "openclawssy_ws_agent1", Driver: "local"},
				{Name: "openclawssy_ws_agent2", Driver: "local"},
			}, nil
		},
	}
	srv := newTestServerWithSandboxAdmin(t, svc, "tok")

	rr := doRequest(t, srv, http.MethodGet, "/api/admin/sandbox/docker/volumes", "tok", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	resp := decodeBody(t, rr)
	count, _ := resp["count"].(float64)
	if count != 2 {
		t.Errorf("expected count=2, got %v", resp["count"])
	}
}

func TestSandboxAdmin_DeleteVolume_Success(t *testing.T) {
	var deletedName string
	svc := &mockDockerAdminService{
		deleteVolumeFn: func(_ context.Context, name string) error {
			deletedName = name
			return nil
		},
	}
	srv := newTestServerWithSandboxAdmin(t, svc, "tok")

	body, _ := json.Marshal(map[string]string{"name": "openclawssy_ws_old"})
	rr := doRequest(t, srv, http.MethodDelete, "/api/admin/sandbox/docker/volume", "tok", body)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	if deletedName != "openclawssy_ws_old" {
		t.Errorf("expected deletedName=openclawssy_ws_old, got %q", deletedName)
	}
	resp := decodeBody(t, rr)
	if resp["action"] != "delete_volume" {
		t.Errorf("expected action=delete_volume, got %v", resp["action"])
	}
}

// ── 400 bad request tests ─────────────────────────────────────────────────────

func TestSandboxAdmin_BadRequest(t *testing.T) {
	type tc struct {
		name   string
		method string
		path   string
		body   []byte
	}

	cases := []tc{
		{
			name:   "create_invalid_json",
			method: http.MethodPost,
			path:   "/api/admin/sandbox/docker/create",
			body:   []byte(`not-json`),
		},
		{
			name:   "stop_invalid_json",
			method: http.MethodPost,
			path:   "/api/admin/sandbox/docker/stop",
			body:   []byte(`not-json`),
		},
		{
			name:   "reset_invalid_json",
			method: http.MethodPost,
			path:   "/api/admin/sandbox/docker/reset",
			body:   []byte(`not-json`),
		},
		{
			name:   "pull_invalid_json",
			method: http.MethodPost,
			path:   "/api/admin/sandbox/docker/pull",
			body:   []byte(`not-json`),
		},
		{
			name:   "pull_empty_image",
			method: http.MethodPost,
			path:   "/api/admin/sandbox/docker/pull",
			body:   []byte(`{"image":""}`),
		},
		{
			name:   "delete_volume_invalid_json",
			method: http.MethodDelete,
			path:   "/api/admin/sandbox/docker/volume",
			body:   []byte(`not-json`),
		},
		{
			name:   "delete_volume_empty_name",
			method: http.MethodDelete,
			path:   "/api/admin/sandbox/docker/volume",
			body:   []byte(`{"name":""}`),
		},
	}

	svc := &mockDockerAdminService{}
	srv := newTestServerWithSandboxAdmin(t, svc, "tok")

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := doRequest(t, srv, tc.method, tc.path, "tok", tc.body)
			if rr.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d (body=%s)", rr.Code, rr.Body.String())
			}
			// Verify error envelope format.
			resp := decodeBody(t, rr)
			errObj, ok := resp["error"].(map[string]any)
			if !ok {
				t.Fatalf("expected error object in response, got %v", resp)
			}
			if errObj["code"] == "" || errObj["code"] == nil {
				t.Errorf("expected non-empty error code, got %v", errObj["code"])
			}
		})
	}
}

// ── Service error propagation tests ──────────────────────────────────────────

func TestSandboxAdmin_ServiceError_Returns500(t *testing.T) {
	serviceErr := errors.New("docker daemon unavailable")

	type tc struct {
		name   string
		method string
		path   string
		body   []byte
		svc    *mockDockerAdminService
	}

	cases := []tc{
		{
			name:   "status_error",
			method: http.MethodGet,
			path:   "/api/admin/sandbox/docker/status?agent_id=x",
			body:   nil,
			svc: &mockDockerAdminService{
				statusFn: func(_ context.Context, _ string) (DockerStatusResponse, error) {
					return DockerStatusResponse{}, serviceErr
				},
			},
		},
		{
			name:   "create_error",
			method: http.MethodPost,
			path:   "/api/admin/sandbox/docker/create",
			body:   []byte(`{"agent_id":"x"}`),
			svc: &mockDockerAdminService{
				createContainerFn: func(_ context.Context, _ string) error { return serviceErr },
			},
		},
		{
			name:   "stop_error",
			method: http.MethodPost,
			path:   "/api/admin/sandbox/docker/stop",
			body:   []byte(`{"agent_id":"x"}`),
			svc: &mockDockerAdminService{
				stopContainerFn: func(_ context.Context, _ string) error { return serviceErr },
			},
		},
		{
			name:   "reset_error",
			method: http.MethodPost,
			path:   "/api/admin/sandbox/docker/reset",
			body:   []byte(`{"agent_id":"x"}`),
			svc: &mockDockerAdminService{
				resetContainerFn: func(_ context.Context, _ string) error { return serviceErr },
			},
		},
		{
			name:   "pull_error",
			method: http.MethodPost,
			path:   "/api/admin/sandbox/docker/pull",
			body:   []byte(`{"image":"ubuntu:24.04"}`),
			svc: &mockDockerAdminService{
				pullImageFn: func(_ context.Context, _ string) error { return serviceErr },
			},
		},
		{
			name:   "images_error",
			method: http.MethodGet,
			path:   "/api/admin/sandbox/docker/images",
			body:   nil,
			svc: &mockDockerAdminService{
				listImagesFn: func(_ context.Context) ([]DockerImageInfo, error) { return nil, serviceErr },
			},
		},
		{
			name:   "volumes_error",
			method: http.MethodGet,
			path:   "/api/admin/sandbox/docker/volumes",
			body:   nil,
			svc: &mockDockerAdminService{
				listVolumesFn: func(_ context.Context) ([]DockerVolumeInfo, error) { return nil, serviceErr },
			},
		},
		{
			name:   "delete_volume_error",
			method: http.MethodDelete,
			path:   "/api/admin/sandbox/docker/volume",
			body:   []byte(`{"name":"somevol"}`),
			svc: &mockDockerAdminService{
				deleteVolumeFn: func(_ context.Context, _ string) error { return serviceErr },
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestServerWithSandboxAdmin(t, tc.svc, "tok")
			rr := doRequest(t, srv, tc.method, tc.path, "tok", tc.body)
			if rr.Code != http.StatusInternalServerError {
				t.Errorf("expected 500, got %d (body=%s)", rr.Code, rr.Body.String())
			}
			resp := decodeBody(t, rr)
			errObj, ok := resp["error"].(map[string]any)
			if !ok {
				t.Fatalf("expected error object in response, got %v", resp)
			}
			if errObj["code"] == "" || errObj["code"] == nil {
				t.Errorf("expected non-empty error code")
			}
			msg, _ := errObj["message"].(string)
			if !strings.Contains(msg, "docker daemon unavailable") {
				t.Errorf("expected error message to contain service error, got %q", msg)
			}
		})
	}
}

// ── sanitizeDockerName unit tests ─────────────────────────────────────────────

func TestSanitizeDockerName(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"myagent", "myagent"},
		{"MyAgent", "myagent"},
		{"agent-1", "agent-1"},
		{"agent 1", "agent_1"},
		{"agent@1", "agent_1"},
		{"", "default"},
		{"UPPER-CASE", "upper-case"},
		{"hello.world", "hello_world"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := sanitizeDockerName(tc.input)
			if got != tc.want {
				t.Errorf("sanitizeDockerName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
