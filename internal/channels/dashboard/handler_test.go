package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	httpchannel "openclawssy/internal/channels/http"
	"openclawssy/internal/chatstore"
	"openclawssy/internal/config"
	"openclawssy/internal/instances"
	"openclawssy/internal/memory"
	memorystore "openclawssy/internal/memory/store"
	"openclawssy/internal/sandbox"
	"openclawssy/internal/scheduler"
	"openclawssy/internal/secrets"
)

type stubDashboardWorkspaceProvider struct {
	listDir  func(context.Context, string) ([]sandbox.FileInfo, error)
	readFile func(context.Context, string) ([]byte, error)
	lstat    func(context.Context, string) (sandbox.FileInfo, bool, error)
}

func (p *stubDashboardWorkspaceProvider) Start(context.Context) error { return nil }
func (p *stubDashboardWorkspaceProvider) Exec(sandbox.Command) (sandbox.Result, error) {
	return sandbox.Result{}, errors.New("not implemented")
}
func (p *stubDashboardWorkspaceProvider) Stop() error { return nil }
func (p *stubDashboardWorkspaceProvider) ReadFile(ctx context.Context, path string) ([]byte, error) {
	if p.readFile != nil {
		return p.readFile(ctx, path)
	}
	return nil, os.ErrNotExist
}
func (p *stubDashboardWorkspaceProvider) WriteFile(context.Context, string, []byte, os.FileMode) error {
	return errors.New("not implemented")
}
func (p *stubDashboardWorkspaceProvider) ListDir(ctx context.Context, path string) ([]sandbox.FileInfo, error) {
	if p.listDir != nil {
		return p.listDir(ctx, path)
	}
	return nil, os.ErrNotExist
}
func (p *stubDashboardWorkspaceProvider) MkdirAll(context.Context, string, os.FileMode) error {
	return errors.New("not implemented")
}
func (p *stubDashboardWorkspaceProvider) Remove(context.Context, string, bool) error {
	return errors.New("not implemented")
}
func (p *stubDashboardWorkspaceProvider) Rename(context.Context, string, string) error {
	return errors.New("not implemented")
}
func (p *stubDashboardWorkspaceProvider) Lstat(ctx context.Context, path string) (sandbox.FileInfo, bool, error) {
	if p.lstat != nil {
		return p.lstat(ctx, path)
	}
	return sandbox.FileInfo{}, false, nil
}
func (p *stubDashboardWorkspaceProvider) EvalSymlinks(context.Context, string) (string, error) {
	return "", errors.New("not implemented")
}

func extractDashboardAssetPath(indexHTML string, suffix string) string {
	marker := "/dashboard/static/assets/"
	searchFrom := 0
	for {
		start := strings.Index(indexHTML[searchFrom:], marker)
		if start < 0 {
			return ""
		}
		start += searchFrom
		end := strings.IndexAny(indexHTML[start:], "\"'")
		if end < 0 {
			return ""
		}
		candidate := indexHTML[start : start+end]
		if strings.Contains(candidate, suffix) {
			return candidate
		}
		searchFrom = start + len(marker)
	}
}

func TestDashboardRouteServesEmbeddedSPA(t *testing.T) {
	h := New(t.TempDir(), httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `<div id="root"></div>`) {
		t.Fatalf("expected react root in html body, got %q", body)
	}
	if strings.Contains(strings.ToLower(body), "open legacy dashboard") {
		t.Fatalf("expected no legacy dashboard link in html body, got %q", body)
	}
}

func TestDashboardRouteWithTrailingSlashServesEmbeddedSPA(t *testing.T) {
	h := New(t.TempDir(), httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `<div id="root"></div>`) {
		t.Fatalf("expected react root in html body, got %q", body)
	}
}

func TestDashboardLegacyRouteReturnsNotFound(t *testing.T) {
	h := New(t.TempDir(), httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/dashboard-legacy", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected %d, got %d", http.StatusNotFound, rr.Code)
	}
}

func TestDashboardStaticAssetRouteServesEmbeddedReactAssets(t *testing.T) {
	h := New(t.TempDir(), httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	indexReq := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	indexResp := httptest.NewRecorder()
	mux.ServeHTTP(indexResp, indexReq)
	if indexResp.Code != http.StatusOK {
		t.Fatalf("expected dashboard index status %d, got %d", http.StatusOK, indexResp.Code)
	}

	assetPath := extractDashboardAssetPath(indexResp.Body.String(), ".js")
	if assetPath == "" {
		t.Fatalf("expected javascript asset reference in dashboard index, got %q", indexResp.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, assetPath, nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "javascript") {
		t.Fatalf("expected javascript content type, got %q", got)
	}
	if len(rr.Body.Bytes()) == 0 {
		t.Fatal("expected non-empty javascript asset body")
	}
}

func TestDashboardStaticAssetRouteMissingAssetNotFound(t *testing.T) {
	h := New(t.TempDir(), httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/static/assets/not-a-real-asset.js", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected %d, got %d", http.StatusNotFound, rr.Code)
	}
}

func TestAdminWorkspaceEntriesListsDirectoriesAndFiles(t *testing.T) {
	root := t.TempDir()
	manifest, err := instances.BootstrapDefaultInstance(root)
	if err != nil {
		t.Fatalf("bootstrap default instance: %v", err)
	}
	workspaceRoot := manifest.Workspace.Root
	if !filepath.IsAbs(workspaceRoot) {
		workspaceRoot = filepath.Join(root, workspaceRoot)
	}
	if err := os.MkdirAll(filepath.Join(workspaceRoot, "project", "nested"), 0o755); err != nil {
		t.Fatalf("mkdir workspace tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceRoot, "project", "notes.txt"), []byte("hello workspace\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/workspace/entries?path=project", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, rr.Code, rr.Body.String())
	}
	var payload struct {
		Path       string                  `json:"path"`
		ParentPath string                  `json:"parent_path"`
		Entries    []workspaceEntryPayload `json:"entries"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Path != "project" {
		t.Fatalf("expected path project, got %#v", payload.Path)
	}
	if payload.ParentPath != "" {
		t.Fatalf("expected empty parent path for top-level child, got %#v", payload.ParentPath)
	}
	if len(payload.Entries) != 2 {
		t.Fatalf("expected two entries, got %#v", payload.Entries)
	}
	if payload.Entries[0].Kind != "dir" || payload.Entries[0].Name != "nested" {
		t.Fatalf("expected nested dir first, got %#v", payload.Entries[0])
	}
	if payload.Entries[1].Kind != "file" || payload.Entries[1].Path != "project/notes.txt" {
		t.Fatalf("unexpected file entry %#v", payload.Entries[1])
	}
	if payload.Entries[1].SizeBytes == 0 {
		t.Fatalf("expected non-zero file size, got %#v", payload.Entries[1])
	}
}

func TestAdminWorkspaceFileReadsTextAndRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	manifest, err := instances.BootstrapDefaultInstance(root)
	if err != nil {
		t.Fatalf("bootstrap default instance: %v", err)
	}
	workspaceRoot := manifest.Workspace.Root
	if !filepath.IsAbs(workspaceRoot) {
		workspaceRoot = filepath.Join(root, workspaceRoot)
	}
	if err := os.MkdirAll(filepath.Join(workspaceRoot, "project"), 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceRoot, "project", "notes.txt"), []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	readReq := httptest.NewRequest(http.MethodGet, "/api/admin/workspace/file?path=project/notes.txt", nil)
	readResp := httptest.NewRecorder()
	mux.ServeHTTP(readResp, readReq)
	if readResp.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, readResp.Code, readResp.Body.String())
	}
	var payload struct {
		Path      string `json:"path"`
		IsText    bool   `json:"is_text"`
		Content   string `json:"content"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal(readResp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode file payload: %v", err)
	}
	if payload.Path != "project/notes.txt" || !payload.IsText {
		t.Fatalf("unexpected file payload %#v", payload)
	}
	if !strings.Contains(payload.Content, "line two") || payload.Truncated {
		t.Fatalf("expected full text preview, got %#v", payload)
	}

	denyReq := httptest.NewRequest(http.MethodGet, "/api/admin/workspace/file?path=../outside.txt", nil)
	denyResp := httptest.NewRecorder()
	mux.ServeHTTP(denyResp, denyReq)
	if denyResp.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, denyResp.Code)
	}
}

func TestAdminWorkspaceUsesActiveInstanceWorkspaceInsteadOfHostFallback(t *testing.T) {
	root := t.TempDir()
	hostWorkspace := filepath.Join(root, "workspace", "project")
	if err := os.MkdirAll(hostWorkspace, 0o755); err != nil {
		t.Fatalf("mkdir host workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hostWorkspace, "host-only.txt"), []byte("host"), 0o644); err != nil {
		t.Fatalf("write host file: %v", err)
	}
	if _, err := instances.BootstrapDefaultInstance(root); err != nil {
		t.Fatalf("bootstrap default instance: %v", err)
	}
	lab := instances.DefaultInstanceManifest("lab")
	lab.Workspace.Root = filepath.Join(root, "workspace", "instances", "lab")
	if err := instances.SaveInstanceManifest(root, lab); err != nil {
		t.Fatalf("save lab instance: %v", err)
	}
	if err := instances.SaveAgentManifest(root, "lab", instances.DefaultAgentManifest("default")); err != nil {
		t.Fatalf("save lab default agent: %v", err)
	}
	if err := instances.SaveAgentManifest(root, "lab", instances.DefaultAgentManifest("builder")); err != nil {
		t.Fatalf("save lab agent: %v", err)
	}
	if err := instances.SaveActiveInstanceID(root, "lab"); err != nil {
		t.Fatalf("save active instance: %v", err)
	}
	instanceWorkspace := filepath.Join(lab.Workspace.Root, "project")
	if err := os.MkdirAll(instanceWorkspace, 0o755); err != nil {
		t.Fatalf("mkdir instance workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(instanceWorkspace, "instance-only.txt"), []byte("instance"), 0o644); err != nil {
		t.Fatalf("write instance file: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/workspace/entries?path=project", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, rr.Code, rr.Body.String())
	}
	var payload struct {
		WorkspaceRoot string                  `json:"workspace_root"`
		WorkspaceMode string                  `json:"workspace_mode"`
		InstanceID    string                  `json:"instance_id"`
		AgentID       string                  `json:"agent_id"`
		Entries       []workspaceEntryPayload `json:"entries"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.InstanceID != "lab" || payload.AgentID != "default" {
		t.Fatalf("expected lab/default identity, got %#v", payload)
	}
	if payload.WorkspaceMode != "none" {
		t.Fatalf("expected none workspace mode, got %#v", payload.WorkspaceMode)
	}
	if payload.WorkspaceRoot != filepath.ToSlash(filepath.Clean(lab.Workspace.Root)) {
		t.Fatalf("expected lab workspace root %q, got %q", filepath.ToSlash(filepath.Clean(lab.Workspace.Root)), payload.WorkspaceRoot)
	}
	if len(payload.Entries) != 1 || payload.Entries[0].Name != "instance-only.txt" {
		t.Fatalf("expected instance workspace entries only, got %#v", payload.Entries)
	}
}

func TestAdminWorkspaceUsesDockerProviderForSelectedAgentWorkspace(t *testing.T) {
	root := t.TempDir()
	if _, err := instances.BootstrapDefaultInstance(root); err != nil {
		t.Fatalf("bootstrap default instance: %v", err)
	}
	lab := instances.DefaultInstanceManifest("lab")
	lab.Workspace.Root = filepath.Join(root, "workspace", "instances", "lab")
	if err := instances.SaveInstanceManifest(root, lab); err != nil {
		t.Fatalf("save lab instance: %v", err)
	}
	if err := instances.SaveAgentManifest(root, "lab", instances.DefaultAgentManifest("builder")); err != nil {
		t.Fatalf("save lab agent: %v", err)
	}
	if err := instances.SaveActiveInstanceID(root, "lab"); err != nil {
		t.Fatalf("save active instance: %v", err)
	}

	effective := config.Default()
	effective.Sandbox.Active = true
	effective.Sandbox.Provider = "docker"

	originalFactory := newDashboardSandboxProviderForAgent
	t.Cleanup(func() { newDashboardSandboxProviderForAgent = originalFactory })
	newDashboardSandboxProviderForAgent = func(name, workspace, agentID string, dockerCfg config.DockerSandboxConfig) (sandbox.Provider, error) {
		if name != "docker" {
			return nil, fmt.Errorf("unexpected provider %q", name)
		}
		if filepath.Clean(workspace) != filepath.Clean(lab.Workspace.Root) {
			return nil, fmt.Errorf("unexpected workspace %q", workspace)
		}
		if agentID != "builder" {
			return nil, fmt.Errorf("unexpected agent %q", agentID)
		}
		return &stubDashboardWorkspaceProvider{
			listDir: func(_ context.Context, path string) ([]sandbox.FileInfo, error) {
				if path != "/workspace/project" {
					return nil, fmt.Errorf("unexpected list path %q", path)
				}
				return []sandbox.FileInfo{{Name: "test2", Size: 5}, {Name: "skills", IsDir: true}}, nil
			},
			readFile: func(_ context.Context, path string) ([]byte, error) {
				if path != "/workspace/project/test2" {
					return nil, fmt.Errorf("unexpected read path %q", path)
				}
				return []byte("hello"), nil
			},
			lstat: func(_ context.Context, path string) (sandbox.FileInfo, bool, error) {
				if path != "/workspace/project/test2" {
					return sandbox.FileInfo{}, false, fmt.Errorf("unexpected stat path %q", path)
				}
				return sandbox.FileInfo{Name: "test2", Size: 5}, true, nil
			},
		}, nil
	}

	h := NewWithOptions(root, httpchannel.NewInMemoryRunStore(), Options{EffectiveConfig: &effective})
	mux := http.NewServeMux()
	h.Register(mux)

	entriesReq := httptest.NewRequest(http.MethodGet, "/api/admin/workspace/entries?instance_id=lab&agent_id=builder&path=project", nil)
	entriesResp := httptest.NewRecorder()
	mux.ServeHTTP(entriesResp, entriesReq)
	if entriesResp.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, entriesResp.Code, entriesResp.Body.String())
	}
	var entriesPayload struct {
		WorkspaceRoot string                  `json:"workspace_root"`
		WorkspaceMode string                  `json:"workspace_mode"`
		InstanceID    string                  `json:"instance_id"`
		AgentID       string                  `json:"agent_id"`
		Entries       []workspaceEntryPayload `json:"entries"`
	}
	if err := json.Unmarshal(entriesResp.Body.Bytes(), &entriesPayload); err != nil {
		t.Fatalf("decode entries payload: %v", err)
	}
	if entriesPayload.WorkspaceRoot != "/workspace" || entriesPayload.WorkspaceMode != "docker" {
		t.Fatalf("expected docker workspace metadata, got %#v", entriesPayload)
	}
	if entriesPayload.InstanceID != "lab" || entriesPayload.AgentID != "builder" {
		t.Fatalf("expected lab/builder identity, got %#v", entriesPayload)
	}
	if len(entriesPayload.Entries) != 2 || entriesPayload.Entries[0].Name != "skills" || entriesPayload.Entries[1].Name != "test2" {
		t.Fatalf("unexpected docker entries %#v", entriesPayload.Entries)
	}

	fileReq := httptest.NewRequest(http.MethodGet, "/api/admin/workspace/file?instance_id=lab&agent_id=builder&path=project/test2", nil)
	fileResp := httptest.NewRecorder()
	mux.ServeHTTP(fileResp, fileReq)
	if fileResp.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, fileResp.Code, fileResp.Body.String())
	}
	var filePayload struct {
		WorkspaceRoot string `json:"workspace_root"`
		WorkspaceMode string `json:"workspace_mode"`
		InstanceID    string `json:"instance_id"`
		AgentID       string `json:"agent_id"`
		Path          string `json:"path"`
		Content       string `json:"content"`
	}
	if err := json.Unmarshal(fileResp.Body.Bytes(), &filePayload); err != nil {
		t.Fatalf("decode file payload: %v", err)
	}
	if filePayload.WorkspaceRoot != "/workspace" || filePayload.Path != "project/test2" || filePayload.Content != "hello" {
		t.Fatalf("unexpected docker file payload %#v", filePayload)
	}
}

func TestDashboardWorkspaceRootFallsBackToContainerWorkspaceWhenConfiguredAbsolutePathMissing(t *testing.T) {
	root := t.TempDir()
	defaultWorkspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(defaultWorkspace, 0o755); err != nil {
		t.Fatalf("mkdir default workspace: %v", err)
	}
	cfg := config.Default()
	cfg.Workspace.Root = "/definitely/missing/host/workspace"
	if err := config.Save(filepath.Join(root, ".openclawssy", "config.json"), cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	if got := h.dashboardWorkspaceRoot(); got != defaultWorkspace {
		t.Fatalf("expected fallback workspace %q, got %q", defaultWorkspace, got)
	}
}

func TestDashboardStaticAssetRouteUnknownPathReturnsNotFound(t *testing.T) {
	h := New(t.TempDir(), httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/static/unknown/missing-file.json", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected %d, got %d", http.StatusNotFound, rr.Code)
	}
}

func TestDebugRunTraceEndpoint(t *testing.T) {
	store := httpchannel.NewInMemoryRunStore()
	_, err := store.Create(context.Background(), httpchannel.Run{
		ID:        "run_1",
		AgentID:   "default",
		Message:   "hello",
		Status:    "completed",
		Trace:     map[string]any{"run_id": "run_1", "prompt_length": float64(42)},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	h := New(".", store)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/debug/runs/run_1/trace", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	trace, ok := payload["trace"].(map[string]any)
	if !ok {
		t.Fatalf("expected trace map, got %#v", payload["trace"])
	}
	if trace["run_id"] != "run_1" {
		t.Fatalf("unexpected run_id in trace: %#v", trace["run_id"])
	}
}

func TestAdminStatusEndpoint(t *testing.T) {
	store := httpchannel.NewInMemoryRunStore()
	_, err := store.Create(context.Background(), httpchannel.Run{ID: "run_a", AgentID: "default", Message: "hello", Status: "completed", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	h := New(t.TempDir(), store)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/status", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["run_count"] != float64(1) {
		t.Fatalf("expected run_count=1, got %#v", payload["run_count"])
	}
}

func TestAdminStatusEndpointIncludesConfiguredModelStamp(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Model.Provider = "openai"
	cfg.Model.Name = "gpt-4.1-mini"
	if err := config.Save(filepath.Join(root, ".openclawssy", "config.json"), cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/status", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	model, ok := payload["model"].(map[string]any)
	if !ok {
		t.Fatalf("expected model map in payload, got %#v", payload["model"])
	}
	if model["provider"] != "openai" || model["name"] != "gpt-4.1-mini" {
		t.Fatalf("unexpected model stamp: %#v", model)
	}
}

func TestAdminMemoryEndpointReturnsHealthAndItems(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, ".openclawssy", "agents", "default", "memory", "memory.db")
	store, err := memorystore.OpenSQLite(dbPath, "default")
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	_, err = store.Upsert(context.Background(), memory.MemoryItem{
		Kind:       "preference",
		Title:      "Notifications",
		Content:    "User prefers proactive notifications.",
		Importance: 4,
		Confidence: 0.9,
	})
	if err != nil {
		t.Fatalf("upsert memory item: %v", err)
	}
	_ = store.Close()

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/memory/default", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["agent_id"] != "default" {
		t.Fatalf("expected default agent id, got %#v", payload["agent_id"])
	}
	if _, ok := payload["health"].(map[string]any); !ok {
		t.Fatalf("expected health payload, got %#v", payload["health"])
	}
	if _, ok := payload["active_items"].([]any); !ok {
		t.Fatalf("expected active_items array, got %#v", payload["active_items"])
	}
	embeddingStats, ok := payload["embedding_stats"].(map[string]any)
	if !ok {
		t.Fatalf("expected embedding_stats object, got %#v", payload["embedding_stats"])
	}
	if _, ok := embeddingStats["vector_count"].(float64); !ok {
		t.Fatalf("expected numeric vector_count, got %#v", embeddingStats["vector_count"])
	}
	if _, ok := embeddingStats["semantic_search_available"].(bool); !ok {
		t.Fatalf("expected semantic_search_available bool, got %#v", embeddingStats["semantic_search_available"])
	}
}

func TestAdminMemoryEndpointRejectsInvalidAgentID(t *testing.T) {
	h := New(t.TempDir(), httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/memory/default/extra", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestAdminConfigEndpointRedactsSecrets(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg := config.Default()
	cfg.Providers.OpenAI.APIKey = "super-secret"
	cfg.Providers.OpenAICompat.APIKey = "generic-secret"
	cfg.Discord.Token = "discord-secret"
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/config", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	var out config.Config
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode config response: %v", err)
	}
	if out.Providers.OpenAI.APIKey != "" || out.Providers.OpenAICompat.APIKey != "" || out.Discord.Token != "" {
		t.Fatalf("expected sensitive values redacted, got %+v", out)
	}
}

func TestAdminStatusIncludesEffectiveRuntimeConfig(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg := config.Default()
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	effective := cfg
	effective.Server.BindAddress = "0.0.0.0"
	effective.Server.Port = 8082
	effective.Workspace.Root = "/app/workspace"
	effective.Sandbox.Active = true
	effective.Sandbox.Provider = "docker"
	effective.Shell.EnableExec = true
	effective.Output.ThinkingMode = config.ThinkingModeNever
	effective.Engine.MaxConcurrentRuns = 64

	h := NewWithOptions(root, httpchannel.NewInMemoryRunStore(), Options{EffectiveConfig: &effective})
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/status", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	runtimePayload, ok := out["runtime"].(map[string]any)
	if !ok {
		t.Fatalf("expected runtime payload, got %#v", out)
	}
	serverPayload, ok := runtimePayload["server"].(map[string]any)
	if !ok || serverPayload["bind_address"] != "0.0.0.0" {
		t.Fatalf("expected runtime server bind_address override, got %#v", runtimePayload)
	}
	if gotPort, ok := serverPayload["port"].(float64); !ok || int(gotPort) != 8082 {
		t.Fatalf("expected runtime server port 8082, got %#v", serverPayload["port"])
	}
	sandboxPayload, ok := runtimePayload["sandbox"].(map[string]any)
	if !ok || sandboxPayload["provider"] != "docker" || sandboxPayload["active"] != true {
		t.Fatalf("expected runtime sandbox payload, got %#v", runtimePayload["sandbox"])
	}
	shellPayload, ok := runtimePayload["shell"].(map[string]any)
	if !ok || shellPayload["enable_exec"] != true {
		t.Fatalf("expected runtime shell payload, got %#v", runtimePayload["shell"])
	}
}

func TestAdminServerControlRestart(t *testing.T) {
	root := t.TempDir()
	restarted := make(chan struct{}, 1)
	h := NewWithOptions(root, httpchannel.NewInMemoryRunStore(), Options{RestartFunc: func() { restarted <- struct{}{} }})
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/server/control", bytes.NewBufferString(`{"action":"restart"}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, rr.Code, rr.Body.String())
	}
	select {
	case <-restarted:
	case <-time.After(2 * time.Second):
		t.Fatal("expected restart func to be invoked")
	}
}

func TestAdminConfigPatchMergesAndValidateReturnsFieldErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".openclawssy"), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	path := filepath.Join(root, ".openclawssy", "config.json")
	cfg := config.Default()
	cfg.Chat.DefaultAgentID = "alpha"
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	patchReq := httptest.NewRequest(http.MethodPatch, "/api/admin/config", bytes.NewBufferString(`{"model":{"provider":"zai","name":"glm-4.7"}}`))
	patchReq.Header.Set("Content-Type", "application/json")
	patchResp := httptest.NewRecorder()
	mux.ServeHTTP(patchResp, patchReq)
	if patchResp.Code != http.StatusOK {
		t.Fatalf("expected patch status 200, got %d (%s)", patchResp.Code, patchResp.Body.String())
	}
	updated, err := config.Load(path)
	if err != nil {
		t.Fatalf("load updated config: %v", err)
	}
	if updated.Chat.DefaultAgentID != "alpha" {
		t.Fatalf("expected unrelated field preserved, got %q", updated.Chat.DefaultAgentID)
	}
	if updated.Model.Provider != "zai" || updated.Model.Name != "glm-4.7" {
		t.Fatalf("expected model patch applied, got %+v", updated.Model)
	}

	validateReq := httptest.NewRequest(http.MethodPost, "/api/admin/config/validate", bytes.NewBufferString(`{"model":{"provider":"bad-provider","name":""}}`))
	validateReq.Header.Set("Content-Type", "application/json")
	validateResp := httptest.NewRecorder()
	mux.ServeHTTP(validateResp, validateReq)
	if validateResp.Code != http.StatusOK {
		t.Fatalf("expected validate status 200, got %d (%s)", validateResp.Code, validateResp.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(validateResp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode validate response: %v", err)
	}
	if ok, _ := payload["ok"].(bool); ok {
		t.Fatalf("expected ok=false for invalid validate payload, got %#v", payload)
	}
	fieldErrors, _ := payload["field_errors"].(map[string]any)
	if _, ok := fieldErrors["model.provider"]; !ok {
		t.Fatalf("expected model.provider field error, got %#v", fieldErrors)
	}
	if _, ok := fieldErrors["model.name"]; !ok {
		t.Fatalf("expected model.name field error, got %#v", fieldErrors)
	}
}

func TestAdminProviderModelsUsesStoredHatzSecret(t *testing.T) {
	root := t.TempDir()
	masterPath := filepath.Join(root, ".openclawssy", "master.key")
	if _, err := secrets.GenerateAndWriteMasterKey(masterPath); err != nil {
		t.Fatalf("generate master key: %v", err)
	}

	hatzServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer hatz-secret" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "hatz-coder"}, {"id": "hatz-reasoner"}},
		})
	}))
	defer hatzServer.Close()

	configPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg := config.Default()
	cfg.Secrets.MasterKeyFile = masterPath
	cfg.Secrets.StoreFile = filepath.Join(root, ".openclawssy", "secrets.enc")
	cfg.Providers.Hatz.BaseURL = hatzServer.URL
	cfg.Providers.Hatz.APIKey = ""
	cfg.Providers.Hatz.APIKeyEnv = ""
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	store, err := secrets.NewStore(cfg)
	if err != nil {
		t.Fatalf("new secret store: %v", err)
	}
	if err := store.Set("provider/hatz/api_key", "hatz-secret"); err != nil {
		t.Fatalf("store hatz secret: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/providers/models?provider=hatz", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (%s)", rr.Code, rr.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode provider models response: %v", err)
	}
	if payload["provider"] != "hatz" {
		t.Fatalf("unexpected provider payload: %#v", payload["provider"])
	}
	models, ok := payload["models"].([]any)
	if !ok || len(models) != 2 || models[0] != "hatz-coder" || models[1] != "hatz-reasoner" {
		t.Fatalf("unexpected models payload: %#v", payload["models"])
	}
}

func TestDashboardLayoutsEndpointRejectsOversizedLayout(t *testing.T) {
	root := t.TempDir()
	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/dashboards", bytes.NewBufferString(`{"name":"Ops"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createResp := httptest.NewRecorder()
	mux.ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected create status 200, got %d (%s)", createResp.Code, createResp.Body.String())
	}
	var createPayload struct {
		Dashboard dashboardLayoutRecord `json:"dashboard"`
	}
	if err := json.Unmarshal(createResp.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	layout := make([]map[string]any, 0, maxDashboardWidgets+1)
	for i := 0; i < maxDashboardWidgets+1; i++ {
		layout = append(layout, map[string]any{"widget_key": "runtime.status", "widget_instance_id": fmt.Sprintf("w%d", i), "x": 0, "y": i, "w": 3, "h": 2})
	}
	body, err := json.Marshal(map[string]any{"name": "Ops", "layout": layout})
	if err != nil {
		t.Fatalf("marshal oversized layout: %v", err)
	}
	updateReq := httptest.NewRequest(http.MethodPut, "/api/admin/dashboards/"+createPayload.Dashboard.ID, bytes.NewReader(body))
	updateReq.Header.Set("Content-Type", "application/json")
	updateResp := httptest.NewRecorder()
	mux.ServeHTTP(updateResp, updateReq)
	if updateResp.Code != http.StatusBadRequest {
		t.Fatalf("expected update status 400, got %d (%s)", updateResp.Code, updateResp.Body.String())
	}
}

func TestAdminSecretsEndpointSetAndList(t *testing.T) {
	root := t.TempDir()
	masterPath := filepath.Join(root, ".openclawssy", "master.key")
	if _, err := secrets.GenerateAndWriteMasterKey(masterPath); err != nil {
		t.Fatalf("generate master key: %v", err)
	}

	configPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg := config.Default()
	cfg.Secrets.MasterKeyFile = masterPath
	cfg.Secrets.StoreFile = filepath.Join(root, ".openclawssy", "secrets.enc")
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	setReq := httptest.NewRequest(http.MethodPost, "/api/admin/secrets", bytes.NewBufferString(`{"name":"discord/token","value":"abc"}`))
	setReq.Header.Set("Content-Type", "application/json")
	setResp := httptest.NewRecorder()
	mux.ServeHTTP(setResp, setReq)
	if setResp.Code != http.StatusOK {
		t.Fatalf("expected set secret status %d, got %d (%s)", http.StatusOK, setResp.Code, setResp.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/secrets", nil)
	listResp := httptest.NewRecorder()
	mux.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected list secrets status %d, got %d (%s)", http.StatusOK, listResp.Code, listResp.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(listResp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode secrets response: %v", err)
	}
	keys, ok := payload["keys"].([]any)
	if !ok || len(keys) != 1 {
		t.Fatalf("expected one stored secret key, got %#v", payload["keys"])
	}
	if keys[0] != "discord/token" {
		t.Fatalf("unexpected key entry: %#v", keys[0])
	}
}

func TestAdminSecretsEndpointValidatesInputAndDeletesKeys(t *testing.T) {
	root := t.TempDir()
	masterPath := filepath.Join(root, ".openclawssy", "master.key")
	if _, err := secrets.GenerateAndWriteMasterKey(masterPath); err != nil {
		t.Fatalf("generate master key: %v", err)
	}

	configPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg := config.Default()
	cfg.Secrets.MasterKeyFile = masterPath
	cfg.Secrets.StoreFile = filepath.Join(root, ".openclawssy", "secrets.enc")
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	tooLongName := strings.Repeat("a", maxAdminSecretKeyLen+1)
	tooLongValue := strings.Repeat("b", maxAdminSecretValueLen+1)
	tests := []struct {
		name string
		body string
	}{
		{name: "blank name", body: `{"name":"   ","value":"abc"}`},
		{name: "control chars", body: "{\"name\":\"discord\\nkey\",\"value\":\"abc\"}"},
		{name: "name too long", body: `{"name":"` + tooLongName + `","value":"abc"}`},
		{name: "value too long", body: `{"name":"discord/bot_token","value":"` + tooLongValue + `"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/admin/secrets", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()
			mux.ServeHTTP(resp, req)
			if resp.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", resp.Code, resp.Body.String())
			}
		})
	}

	setReq := httptest.NewRequest(http.MethodPost, "/api/admin/secrets", bytes.NewBufferString(`{"name":" discord/bot_token ","value":"abc"}`))
	setReq.Header.Set("Content-Type", "application/json")
	setResp := httptest.NewRecorder()
	mux.ServeHTTP(setResp, setReq)
	if setResp.Code != http.StatusOK {
		t.Fatalf("expected set status 200, got %d (%s)", setResp.Code, setResp.Body.String())
	}
	setOtherReq := httptest.NewRequest(http.MethodPost, "/api/admin/secrets", bytes.NewBufferString(`{"name":"OPENAI_API_KEY","value":"xyz"}`))
	setOtherReq.Header.Set("Content-Type", "application/json")
	setOtherResp := httptest.NewRecorder()
	mux.ServeHTTP(setOtherResp, setOtherReq)
	if setOtherResp.Code != http.StatusOK {
		t.Fatalf("expected second set status 200, got %d (%s)", setOtherResp.Code, setOtherResp.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/secrets/discord%2Fbot_token", nil)
	deleteResp := httptest.NewRecorder()
	mux.ServeHTTP(deleteResp, deleteReq)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("expected delete status 200, got %d (%s)", deleteResp.Code, deleteResp.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/secrets", nil)
	listResp := httptest.NewRecorder()
	mux.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected list status 200, got %d (%s)", listResp.Code, listResp.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(listResp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode secrets response: %v", err)
	}
	keys, ok := payload["keys"].([]any)
	if !ok {
		t.Fatalf("expected keys array, got %#v", payload["keys"])
	}
	if len(keys) != 1 || keys[0] != "OPENAI_API_KEY" {
		t.Fatalf("expected delete to preserve other keys, got %#v", keys)
	}

	deleteMissingReq := httptest.NewRequest(http.MethodDelete, "/api/admin/secrets/discord%2Fbot_token", nil)
	deleteMissingResp := httptest.NewRecorder()
	mux.ServeHTTP(deleteMissingResp, deleteMissingReq)
	if deleteMissingResp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing key, got %d (%s)", deleteMissingResp.Code, deleteMissingResp.Body.String())
	}
}

func TestDashboardLayoutsEndpointsPersistRecords(t *testing.T) {
	root := t.TempDir()
	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/dashboards", bytes.NewBufferString(`{"name":"Ops"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createResp := httptest.NewRecorder()
	mux.ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected create status 200, got %d (%s)", createResp.Code, createResp.Body.String())
	}
	var createPayload struct {
		Dashboard dashboardLayoutRecord `json:"dashboard"`
	}
	if err := json.Unmarshal(createResp.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if createPayload.Dashboard.ID == "" {
		t.Fatal("expected created dashboard id")
	}

	updateReq := httptest.NewRequest(http.MethodPut, "/api/admin/dashboards/"+createPayload.Dashboard.ID, bytes.NewBufferString(`{"name":"Ops Board","layout":[{"widget_key":"runtime.status","widget_instance_id":"w1","x":0,"y":0,"w":4,"h":2}]}`))
	updateReq.Header.Set("Content-Type", "application/json")
	updateResp := httptest.NewRecorder()
	mux.ServeHTTP(updateResp, updateReq)
	if updateResp.Code != http.StatusOK {
		t.Fatalf("expected update status 200, got %d (%s)", updateResp.Code, updateResp.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/dashboards", nil)
	listResp := httptest.NewRecorder()
	mux.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected list status 200, got %d (%s)", listResp.Code, listResp.Body.String())
	}
	var listPayload struct {
		Dashboards []dashboardLayoutRecord `json:"dashboards"`
	}
	if err := json.Unmarshal(listResp.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listPayload.Dashboards) != 1 || listPayload.Dashboards[0].Name != "Ops Board" {
		t.Fatalf("unexpected dashboards payload: %#v", listPayload.Dashboards)
	}
	if len(listPayload.Dashboards[0].Layout) != 1 || listPayload.Dashboards[0].Layout[0].WidgetKey != "runtime.status" {
		t.Fatalf("unexpected layout payload: %#v", listPayload.Dashboards[0].Layout)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/dashboards/"+createPayload.Dashboard.ID, nil)
	deleteResp := httptest.NewRecorder()
	mux.ServeHTTP(deleteResp, deleteReq)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("expected delete status 200, got %d (%s)", deleteResp.Code, deleteResp.Body.String())
	}
}

func TestAdminAgentDocsEndpointListAndSave(t *testing.T) {
	root := t.TempDir()
	agentDir := filepath.Join(root, ".openclawssy", "agents", "default")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "SOUL.md"), []byte("# SOUL\nold"), 0o600); err != nil {
		t.Fatalf("write soul doc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "HANDOFF.md"), []byte("# HANDOFF\nold"), 0o600); err != nil {
		t.Fatalf("write handoff doc: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/agent/docs?agent_id=default", nil)
	listResp := httptest.NewRecorder()
	mux.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected list status %d, got %d (%s)", http.StatusOK, listResp.Code, listResp.Body.String())
	}
	var listPayload struct {
		AgentID   string            `json:"agent_id"`
		Documents []agentDocPayload `json:"documents"`
	}
	if err := json.Unmarshal(listResp.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode docs list: %v", err)
	}
	if listPayload.AgentID != "default" {
		t.Fatalf("unexpected agent id: %q", listPayload.AgentID)
	}
	if len(listPayload.Documents) < 7 {
		t.Fatalf("expected editable docs payload, got %d docs", len(listPayload.Documents))
	}

	var heartbeatDoc *agentDocPayload
	for i := range listPayload.Documents {
		doc := &listPayload.Documents[i]
		if doc.Name == "HEARTBEAT.md" {
			heartbeatDoc = doc
			break
		}
	}
	if heartbeatDoc == nil {
		t.Fatal("expected HEARTBEAT.md entry in documents")
	}
	if heartbeatDoc.AliasFor != "HANDOFF.md" {
		t.Fatalf("expected heartbeat alias to handoff, got %q", heartbeatDoc.AliasFor)
	}

	setHeartbeatReq := httptest.NewRequest(http.MethodPost, "/api/admin/agent/docs", bytes.NewBufferString(`{"agent_id":"default","name":"HEARTBEAT.md","content":"# HEARTBEAT\nupdated"}`))
	setHeartbeatReq.Header.Set("Content-Type", "application/json")
	setHeartbeatResp := httptest.NewRecorder()
	mux.ServeHTTP(setHeartbeatResp, setHeartbeatReq)
	if setHeartbeatResp.Code != http.StatusOK {
		t.Fatalf("expected set heartbeat status %d, got %d (%s)", http.StatusOK, setHeartbeatResp.Code, setHeartbeatResp.Body.String())
	}

	rawHandoff, err := os.ReadFile(filepath.Join(agentDir, "HANDOFF.md"))
	if err != nil {
		t.Fatalf("read handoff after heartbeat update: %v", err)
	}
	if string(rawHandoff) != "# HEARTBEAT\nupdated" {
		t.Fatalf("expected heartbeat write to update HANDOFF.md, got %q", string(rawHandoff))
	}

	setSoulReq := httptest.NewRequest(http.MethodPost, "/api/admin/agent/docs", bytes.NewBufferString(`{"agent_id":"default","name":"SOUL.md","content":"# SOUL\nnew"}`))
	setSoulReq.Header.Set("Content-Type", "application/json")
	setSoulResp := httptest.NewRecorder()
	mux.ServeHTTP(setSoulResp, setSoulReq)
	if setSoulResp.Code != http.StatusOK {
		t.Fatalf("expected set soul status %d, got %d (%s)", http.StatusOK, setSoulResp.Code, setSoulResp.Body.String())
	}
	rawSoul, err := os.ReadFile(filepath.Join(agentDir, "SOUL.md"))
	if err != nil {
		t.Fatalf("read soul after update: %v", err)
	}
	if string(rawSoul) != "# SOUL\nnew" {
		t.Fatalf("unexpected SOUL.md content: %q", string(rawSoul))
	}
}

func TestAdminAgentDocsEndpointRejectsInvalidInput(t *testing.T) {
	h := New(t.TempDir(), httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	invalidDocReq := httptest.NewRequest(http.MethodPost, "/api/admin/agent/docs", bytes.NewBufferString(`{"agent_id":"default","name":"README.md","content":"x"}`))
	invalidDocReq.Header.Set("Content-Type", "application/json")
	invalidDocResp := httptest.NewRecorder()
	mux.ServeHTTP(invalidDocResp, invalidDocReq)
	if invalidDocResp.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid doc status %d, got %d", http.StatusBadRequest, invalidDocResp.Code)
	}

	invalidAgentReq := httptest.NewRequest(http.MethodGet, "/api/admin/agent/docs?agent_id=../../etc", nil)
	invalidAgentResp := httptest.NewRecorder()
	mux.ServeHTTP(invalidAgentResp, invalidAgentReq)
	if invalidAgentResp.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid agent status %d, got %d", http.StatusBadRequest, invalidAgentResp.Code)
	}
}

func TestAdminSkillsInstallAndActivation(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "workspace"), 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := config.Save(filepath.Join(root, ".openclawssy", "config.json"), config.Default()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/skills?agent_id=default", nil)
	listResp := httptest.NewRecorder()
	mux.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected list status %d, got %d (%s)", http.StatusOK, listResp.Code, listResp.Body.String())
	}
	var listPayload map[string]any
	if err := json.Unmarshal(listResp.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode list payload: %v", err)
	}
	installable, ok := listPayload["installable"].([]any)
	if !ok {
		t.Fatalf("expected installable skills list, got %#v", listPayload["installable"])
	}
	foundClawDefuckifier := false
	for _, item := range installable {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if entry["name"] == "clawdefuckifier" {
			foundClawDefuckifier = true
			break
		}
	}
	if !foundClawDefuckifier {
		t.Fatalf("expected clawdefuckifier in installable skills, got %#v", listPayload["installable"])
	}

	installReq := httptest.NewRequest(http.MethodPost, "/api/admin/skills", bytes.NewBufferString(`{"action":"install","name":"playwrite","agent_id":"default"}`))
	installReq.Header.Set("Content-Type", "application/json")
	installResp := httptest.NewRecorder()
	mux.ServeHTTP(installResp, installReq)
	if installResp.Code != http.StatusOK {
		t.Fatalf("expected install status %d, got %d (%s)", http.StatusOK, installResp.Code, installResp.Body.String())
	}

	if _, err := os.Stat(filepath.Join(root, "workspace", "skills", "playwrite.md")); err != nil {
		t.Fatalf("expected installed playwrite skill file: %v", err)
	}

	activateReq := httptest.NewRequest(http.MethodPost, "/api/admin/skills", bytes.NewBufferString(`{"action":"activate","name":"playwrite","agent_id":"default"}`))
	activateReq.Header.Set("Content-Type", "application/json")
	activateResp := httptest.NewRecorder()
	mux.ServeHTTP(activateResp, activateReq)
	if activateResp.Code != http.StatusOK {
		t.Fatalf("expected activate status %d, got %d (%s)", http.StatusOK, activateResp.Code, activateResp.Body.String())
	}

	rawTools, err := os.ReadFile(filepath.Join(root, ".openclawssy", "agents", "default", "TOOLS.md"))
	if err != nil {
		t.Fatalf("read TOOLS.md: %v", err)
	}
	toolsText := string(rawTools)
	if !strings.Contains(toolsText, "OPENCLAWSSY_ACTIVATED_SKILLS_START") || !strings.Contains(toolsText, "- playwrite") {
		t.Fatalf("expected TOOLS.md activated skills block, got %q", toolsText)
	}

	verifyReq := httptest.NewRequest(http.MethodGet, "/api/admin/skills?agent_id=default", nil)
	verifyResp := httptest.NewRecorder()
	mux.ServeHTTP(verifyResp, verifyReq)
	if verifyResp.Code != http.StatusOK {
		t.Fatalf("expected verify status %d, got %d (%s)", http.StatusOK, verifyResp.Code, verifyResp.Body.String())
	}
	var verifyPayload map[string]any
	if err := json.Unmarshal(verifyResp.Body.Bytes(), &verifyPayload); err != nil {
		t.Fatalf("decode verify payload: %v", err)
	}
	activatedAny, ok := verifyPayload["activated_skills"].([]any)
	if !ok || len(activatedAny) != 1 || activatedAny[0] != "playwrite" {
		t.Fatalf("expected activated playwrite skill, got %#v", verifyPayload["activated_skills"])
	}

	deactivateReq := httptest.NewRequest(http.MethodPost, "/api/admin/skills", bytes.NewBufferString(`{"action":"deactivate","name":"playwrite","agent_id":"default"}`))
	deactivateReq.Header.Set("Content-Type", "application/json")
	deactivateResp := httptest.NewRecorder()
	mux.ServeHTTP(deactivateResp, deactivateReq)
	if deactivateResp.Code != http.StatusOK {
		t.Fatalf("expected deactivate status %d, got %d (%s)", http.StatusOK, deactivateResp.Code, deactivateResp.Body.String())
	}

	rawTools, err = os.ReadFile(filepath.Join(root, ".openclawssy", "agents", "default", "TOOLS.md"))
	if err != nil {
		t.Fatalf("read TOOLS.md after deactivate: %v", err)
	}
	if strings.Contains(string(rawTools), "- playwrite") {
		t.Fatalf("expected playwrite to be removed after deactivate, got %q", string(rawTools))
	}
}

func TestDocsAndSkillsRoutesRequireInstanceAgentsFeature(t *testing.T) {
	root := t.TempDir()
	h := New(root, httpchannel.NewInMemoryRunStore())
	store := defaultControlPlaneStore()
	store.Features.InstanceAgents = false
	if err := h.saveControlPlaneStore(store); err != nil {
		t.Fatalf("save control plane store: %v", err)
	}
	mux := http.NewServeMux()
	h.Register(mux)

	docsListReq := httptest.NewRequest(http.MethodGet, "/api/admin/agent/docs?agent_id=default", nil)
	docsListRR := httptest.NewRecorder()
	mux.ServeHTTP(docsListRR, docsListReq)
	if docsListRR.Code != http.StatusForbidden {
		t.Fatalf("expected docs list status %d, got %d (%s)", http.StatusForbidden, docsListRR.Code, docsListRR.Body.String())
	}
	assertDashboardErrorCode(t, docsListRR.Body.Bytes(), "feature.instance_agents_disabled")

	docsSetReq := httptest.NewRequest(http.MethodPost, "/api/admin/agent/docs", bytes.NewBufferString(`{"agent_id":"default","name":"SOUL.md","content":"x"}`))
	docsSetReq.Header.Set("Content-Type", "application/json")
	docsSetRR := httptest.NewRecorder()
	mux.ServeHTTP(docsSetRR, docsSetReq)
	if docsSetRR.Code != http.StatusForbidden {
		t.Fatalf("expected docs set status %d, got %d (%s)", http.StatusForbidden, docsSetRR.Code, docsSetRR.Body.String())
	}
	assertDashboardErrorCode(t, docsSetRR.Body.Bytes(), "feature.instance_agents_disabled")

	skillsListReq := httptest.NewRequest(http.MethodGet, "/api/admin/skills?agent_id=default", nil)
	skillsListRR := httptest.NewRecorder()
	mux.ServeHTTP(skillsListRR, skillsListReq)
	if skillsListRR.Code != http.StatusForbidden {
		t.Fatalf("expected skills list status %d, got %d (%s)", http.StatusForbidden, skillsListRR.Code, skillsListRR.Body.String())
	}
	assertDashboardErrorCode(t, skillsListRR.Body.Bytes(), "feature.instance_agents_disabled")

	skillsPostReq := httptest.NewRequest(http.MethodPost, "/api/admin/skills", bytes.NewBufferString(`{"action":"install","name":"playwrite","agent_id":"default"}`))
	skillsPostReq.Header.Set("Content-Type", "application/json")
	skillsPostRR := httptest.NewRecorder()
	mux.ServeHTTP(skillsPostRR, skillsPostReq)
	if skillsPostRR.Code != http.StatusForbidden {
		t.Fatalf("expected skills post status %d, got %d (%s)", http.StatusForbidden, skillsPostRR.Code, skillsPostRR.Body.String())
	}
	assertDashboardErrorCode(t, skillsPostRR.Body.Bytes(), "feature.instance_agents_disabled")
}

func TestDebugRunTraceEndpointReturnsNotFoundWithoutTrace(t *testing.T) {
	store := httpchannel.NewInMemoryRunStore()
	_, err := store.Create(context.Background(), httpchannel.Run{ID: "run_2", AgentID: "default", Message: "hello", Status: "completed", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	h := New(".", store)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/debug/runs/run_2/trace", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected %d, got %d", http.StatusNotFound, rr.Code)
	}
}

func TestListChatSessionsEndpoint(t *testing.T) {
	root := t.TempDir()
	store, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	_, err = store.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "dashboard_user", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/chat/sessions?agent_id=default&user_id=dashboard_user&room_id=dashboard&channel=dashboard", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	sessions, ok := payload["sessions"].([]any)
	if !ok || len(sessions) != 1 {
		t.Fatalf("expected one session, got %#v", payload["sessions"])
	}
}

func TestListChatSessionsEndpointPagination(t *testing.T) {
	root := t.TempDir()
	store, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := store.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "dashboard_user", RoomID: "dashboard"}); err != nil {
			t.Fatalf("create session %d: %v", i, err)
		}
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/chat/sessions?agent_id=default&user_id=dashboard_user&room_id=dashboard&channel=dashboard&limit=1&offset=1", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	var payload struct {
		Sessions []any `json:"sessions"`
		Total    int   `json:"total"`
		Limit    int   `json:"limit"`
		Offset   int   `json:"offset"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Total != 3 || payload.Limit != 1 || payload.Offset != 1 {
		t.Fatalf("unexpected pagination metadata: %+v", payload)
	}
	if len(payload.Sessions) != 1 {
		t.Fatalf("expected one paged session, got %d", len(payload.Sessions))
	}
}

func TestAdminAgentsEndpointListAndSetActive(t *testing.T) {
	root := t.TempDir()
	enabled := true
	cfg := config.Default()
	cfg.Agents.AllowInterAgentMessaging = true
	cfg.Agents.AllowAgentModelOverrides = true
	cfg.Agents.SelfImprovementEnabled = true
	cfg.Agents.Profiles = map[string]config.AgentProfile{
		"alpha": {
			Enabled:         &enabled,
			SelfImprovement: true,
			Model: config.ModelConfig{
				Provider:  "openai",
				Name:      "gpt-4.1-mini",
				MaxTokens: 1024,
				TimeoutMS: 180000,
			},
		},
		"reviewer": {},
	}
	cfg.Agents.EnabledAgentIDs = []string{"planner"}
	cfg.Chat.DefaultAgentID = "default"
	cfg.Discord.DefaultAgentID = "discord-bot"
	if err := config.Save(filepath.Join(root, ".openclawssy", "config.json"), cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	store, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	if _, err := store.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "dashboard_user", RoomID: "dashboard"}); err != nil {
		t.Fatalf("create default session: %v", err)
	}
	if _, err := store.CreateSession(chatstore.CreateSessionInput{AgentID: "alpha", Channel: "dashboard", UserID: "dashboard_user", RoomID: "dashboard"}); err != nil {
		t.Fatalf("create alpha session: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/agents?channel=dashboard&user_id=dashboard_user&room_id=dashboard", nil)
	listResp := httptest.NewRecorder()
	mux.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected list agents status 200, got %d (%s)", listResp.Code, listResp.Body.String())
	}
	var listed map[string]any
	if err := json.Unmarshal(listResp.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list payload: %v", err)
	}
	if listed["selected_agent"] != "default" {
		t.Fatalf("expected selected_agent default on first list, got %#v", listed["selected_agent"])
	}
	agentSummaries, ok := listed["agent_summaries"].(map[string]any)
	if !ok {
		t.Fatalf("expected agent_summaries object, got %#v", listed["agent_summaries"])
	}
	agents, ok := listed["agents"].([]any)
	if !ok {
		t.Fatalf("expected agents array, got %#v", listed["agents"])
	}
	seen := map[string]bool{}
	for _, item := range agents {
		seen[strings.TrimSpace(fmt.Sprint(item))] = true
	}
	for _, want := range []string{"default", "alpha", "reviewer", "planner", "discord-bot"} {
		if !seen[want] {
			t.Fatalf("expected agent %q in unified list, got %#v", want, listed["agents"])
		}
	}
	alphaSummary, ok := agentSummaries["alpha"].(map[string]any)
	if !ok {
		t.Fatalf("expected alpha summary object, got %#v", agentSummaries["alpha"])
	}
	if alphaSummary["self_improvement_ready"] != true {
		t.Fatalf("expected alpha self_improvement_ready=true, got %#v", alphaSummary)
	}
	activatedSkills, ok := alphaSummary["activated_skills"].([]any)
	if !ok {
		activatedSkills = []any{}
	}
	if len(activatedSkills) != 0 {
		t.Fatalf("expected no activated skills for alpha in fixture, got %#v", activatedSkills)
	}

	setReq := httptest.NewRequest(http.MethodPost, "/api/admin/agents", bytes.NewBufferString(`{"channel":"dashboard","user_id":"dashboard_user","room_id":"dashboard","agent_id":"alpha"}`))
	setReq.Header.Set("Content-Type", "application/json")
	setResp := httptest.NewRecorder()
	mux.ServeHTTP(setResp, setReq)
	if setResp.Code != http.StatusOK {
		t.Fatalf("expected set active agent status 200, got %d (%s)", setResp.Code, setResp.Body.String())
	}

	verifyReq := httptest.NewRequest(http.MethodGet, "/api/admin/agents?channel=dashboard&user_id=dashboard_user&room_id=dashboard", nil)
	verifyResp := httptest.NewRecorder()
	mux.ServeHTTP(verifyResp, verifyReq)
	if verifyResp.Code != http.StatusOK {
		t.Fatalf("expected verify status 200, got %d", verifyResp.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(verifyResp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode verify payload: %v", err)
	}
	if payload["active_agent"] != "alpha" {
		t.Fatalf("expected active_agent alpha, got %#v", payload["active_agent"])
	}
	if payload["selected_agent"] != "default" {
		t.Fatalf("expected selected_agent default while active pointer is alpha, got %#v", payload["selected_agent"])
	}
	profileContext, ok := payload["profile_context"].(map[string]any)
	if !ok {
		t.Fatalf("expected profile_context object, got %#v", payload["profile_context"])
	}
	if profileContext["agent_id"] != "default" || profileContext["exists"] != false {
		t.Fatalf("unexpected profile context header: %#v", profileContext)
	}
	if profileContext["model_provider"] != "" || profileContext["model_name"] != "" || profileContext["model_timeout_ms"] != float64(0) {
		t.Fatalf("expected no profile model override fields for default fallback, got %#v", profileContext)
	}
	agentsConfig, ok := payload["agents_config"].(map[string]any)
	if !ok {
		t.Fatalf("expected agents_config object, got %#v", payload["agents_config"])
	}
	if agentsConfig["allow_agent_model_overrides"] != true || agentsConfig["self_improvement_enabled"] != true {
		t.Fatalf("unexpected agents_config payload: %#v", agentsConfig)
	}
}

func TestAdminAgentsEndpointUsesActiveInstanceConfigAndInstanceScopedPointers(t *testing.T) {
	root := t.TempDir()
	if _, err := instances.BootstrapDefaultInstance(root); err != nil {
		t.Fatalf("bootstrap default instance: %v", err)
	}
	lab := instances.DefaultInstanceManifest("lab")
	lab.Workspace.Root = filepath.Join(root, "workspace")
	if err := instances.SaveInstanceManifest(root, lab); err != nil {
		t.Fatalf("save lab instance: %v", err)
	}
	if err := instances.SaveAgentManifest(root, "lab", instances.DefaultAgentManifest("builder")); err != nil {
		t.Fatalf("save lab builder agent: %v", err)
	}
	if err := instances.SaveAgentManifest(root, "lab", instances.DefaultAgentManifest("reviewer")); err != nil {
		t.Fatalf("save lab reviewer agent: %v", err)
	}
	if err := instances.SaveActiveInstanceID(root, "lab"); err != nil {
		t.Fatalf("save active instance id: %v", err)
	}

	store, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	if err := store.SetActiveAgentPointer("dashboard", "dashboard_user", "default:dashboard", "default"); err != nil {
		t.Fatalf("seed default pointer: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/agents?channel=dashboard&user_id=dashboard_user&room_id=dashboard", nil)
	listResp := httptest.NewRecorder()
	mux.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected list agents status 200, got %d (%s)", listResp.Code, listResp.Body.String())
	}
	var listed map[string]any
	if err := json.Unmarshal(listResp.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list payload: %v", err)
	}
	if listed["instance_id"] != "lab" {
		t.Fatalf("expected instance_id lab, got %#v", listed["instance_id"])
	}
	if listed["selected_agent"] != "builder" {
		t.Fatalf("expected selected_agent builder fallback, got %#v", listed["selected_agent"])
	}
	agents, ok := listed["agents"].([]any)
	if !ok {
		t.Fatalf("expected agents array, got %#v", listed["agents"])
	}
	seen := map[string]bool{}
	for _, item := range agents {
		seen[strings.TrimSpace(fmt.Sprint(item))] = true
	}
	if !seen["builder"] || !seen["reviewer"] {
		t.Fatalf("expected lab agents in list, got %#v", listed["agents"])
	}
	if seen["discord-bot"] {
		t.Fatalf("did not expect legacy root-config agent in lab instance list, got %#v", listed["agents"])
	}

	setReq := httptest.NewRequest(http.MethodPost, "/api/admin/agents", bytes.NewBufferString(`{"channel":"dashboard","user_id":"dashboard_user","room_id":"dashboard","agent_id":"builder"}`))
	setReq.Header.Set("Content-Type", "application/json")
	setResp := httptest.NewRecorder()
	mux.ServeHTTP(setResp, setReq)
	if setResp.Code != http.StatusOK {
		t.Fatalf("expected set active agent status 200, got %d (%s)", setResp.Code, setResp.Body.String())
	}

	verifyReq := httptest.NewRequest(http.MethodGet, "/api/admin/agents?channel=dashboard&user_id=dashboard_user&room_id=dashboard", nil)
	verifyResp := httptest.NewRecorder()
	mux.ServeHTTP(verifyResp, verifyReq)
	if verifyResp.Code != http.StatusOK {
		t.Fatalf("expected verify status 200, got %d (%s)", verifyResp.Code, verifyResp.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(verifyResp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode verify payload: %v", err)
	}
	if payload["active_agent"] != "builder" || payload["selected_agent"] != "builder" {
		t.Fatalf("expected builder active/selected, got %#v", payload)
	}
	pointer, err := store.GetActiveAgentPointer("dashboard", "dashboard_user", "lab:dashboard")
	if err != nil {
		t.Fatalf("get lab active pointer: %v", err)
	}
	if pointer != "builder" {
		t.Fatalf("expected lab pointer builder, got %q", pointer)
	}
	defaultPointer, err := store.GetActiveAgentPointer("dashboard", "dashboard_user", "default:dashboard")
	if err != nil {
		t.Fatalf("get default active pointer: %v", err)
	}
	if defaultPointer != "default" {
		t.Fatalf("expected default pointer to remain default, got %q", defaultPointer)
	}
}

func TestAdminAgentsEndpointFallsBackToInstanceDefaultAgentWhenDefaultManifestMissing(t *testing.T) {
	root := t.TempDir()
	if _, err := instances.BootstrapDefaultInstance(root); err != nil {
		t.Fatalf("bootstrap default instance: %v", err)
	}
	instance := instances.DefaultInstanceManifest("ussyone")
	instance.Runtime.DefaultAgentID = "ussy1"
	instance.Runtime.EnabledAgentIDs = []string{"ussy1"}
	instance.Channels = map[string]instances.ChannelRoute{"dashboard": {DefaultAgentID: "ussy1"}}
	if err := instances.SaveInstanceManifest(root, instance); err != nil {
		t.Fatalf("save instance manifest: %v", err)
	}
	if err := instances.SaveInstanceChannels(root, "ussyone", instance.Channels); err != nil {
		t.Fatalf("save instance channels: %v", err)
	}
	if err := instances.SaveAgentManifest(root, "ussyone", instances.DefaultAgentManifest("ussy1")); err != nil {
		t.Fatalf("save instance agent: %v", err)
	}
	if err := instances.SaveActiveInstanceID(root, "ussyone"); err != nil {
		t.Fatalf("save active instance id: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/agents?channel=dashboard&user_id=dashboard_user&room_id=dashboard", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected list agents status 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode list payload: %v", err)
	}
	if payload["instance_id"] != "ussyone" {
		t.Fatalf("expected instance_id ussyone, got %#v", payload["instance_id"])
	}
	if payload["selected_agent"] != "ussy1" || payload["active_agent"] != "ussy1" {
		t.Fatalf("expected ussy1 selected/active fallback, got %#v", payload)
	}
	profileContext, ok := payload["profile_context"].(map[string]any)
	if !ok {
		t.Fatalf("expected profile_context object, got %#v", payload["profile_context"])
	}
	if profileContext["agent_id"] != "ussy1" || profileContext["exists"] != true {
		t.Fatalf("expected ussy1 profile context, got %#v", profileContext)
	}
	agents, ok := payload["agents"].([]any)
	if !ok || len(agents) != 1 || fmt.Sprint(agents[0]) != "ussy1" {
		t.Fatalf("expected single ussy1 agent entry, got %#v", payload["agents"])
	}
}

func TestAdminAgentsEndpointRequiresInstanceAgentsFeature(t *testing.T) {
	root := t.TempDir()
	h := New(root, httpchannel.NewInMemoryRunStore())
	store := defaultControlPlaneStore()
	store.Features.InstanceAgents = false
	if err := h.saveControlPlaneStore(store); err != nil {
		t.Fatalf("save control plane store: %v", err)
	}
	mux := http.NewServeMux()
	h.Register(mux)

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/agents?channel=dashboard&user_id=dashboard_user&room_id=dashboard", nil)
	listRR := httptest.NewRecorder()
	mux.ServeHTTP(listRR, listReq)
	if listRR.Code != http.StatusForbidden {
		t.Fatalf("expected agents list status %d, got %d (%s)", http.StatusForbidden, listRR.Code, listRR.Body.String())
	}
	assertDashboardErrorCode(t, listRR.Body.Bytes(), "feature.instance_agents_disabled")

	setReq := httptest.NewRequest(http.MethodPost, "/api/admin/agents", bytes.NewBufferString(`{"channel":"dashboard","user_id":"dashboard_user","room_id":"dashboard","agent_id":"default"}`))
	setReq.Header.Set("Content-Type", "application/json")
	setRR := httptest.NewRecorder()
	mux.ServeHTTP(setRR, setReq)
	if setRR.Code != http.StatusForbidden {
		t.Fatalf("expected agents set status %d, got %d (%s)", http.StatusForbidden, setRR.Code, setRR.Body.String())
	}
	assertDashboardErrorCode(t, setRR.Body.Bytes(), "feature.instance_agents_disabled")
}

type stubDashboardRunCanceller struct {
	tracked  map[string]bool
	called   []string
	onCancel func(runID string)
}

func (s *stubDashboardRunCanceller) Cancel(runID string) error {
	s.called = append(s.called, runID)
	if s.onCancel != nil {
		s.onCancel(runID)
	}
	if s.tracked[runID] {
		return nil
	}
	return errors.New("not tracked")
}

func (s *stubDashboardRunCanceller) IsTracked(runID string) bool {
	return s.tracked[runID]
}

func (s *stubDashboardRunCanceller) CancelComposite(instanceID, agentID, runID string) error {
	return s.Cancel(instanceID + ":" + agentID + ":" + runID)
}

func (s *stubDashboardRunCanceller) IsTrackedComposite(instanceID, agentID, runID string) bool {
	return s.IsTracked(instanceID + ":" + agentID + ":" + runID)
}

func TestMonitorRunControlReconcilesStaleRunningEntryAfterSuccessfulCancel(t *testing.T) {
	root := t.TempDir()
	auditDir := filepath.Join(root, ".openclawssy", "agents", "alpha", "audit")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatalf("mkdir audit dir: %v", err)
	}
	auditBody := `{"ts":"2026-03-14T12:00:00Z","type":"run.start","run_id":"run-stale","agent_id":"alpha","payload":{"source":"chat","message":"cancel me"}}` + "\n"
	if err := os.WriteFile(filepath.Join(auditDir, "events.jsonl"), []byte(auditBody), 0o600); err != nil {
		t.Fatalf("write audit log: %v", err)
	}

	canceller := &stubDashboardRunCanceller{tracked: map[string]bool{"run-stale": true}}
	h := NewWithOptions(root, httpchannel.NewInMemoryRunStore(), Options{RunCanceller: canceller})
	mux := http.NewServeMux()
	h.Register(mux)

	cancelReq := httptest.NewRequest(http.MethodPost, "/api/admin/monitor/runs/control", bytes.NewBufferString(`{"action":"cancel","run_id":"run-stale"}`))
	cancelReq.Header.Set("Content-Type", "application/json")
	cancelResp := httptest.NewRecorder()
	mux.ServeHTTP(cancelResp, cancelReq)
	if cancelResp.Code != http.StatusOK {
		t.Fatalf("expected cancel status 200, got %d (%s)", cancelResp.Code, cancelResp.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/monitor/runs", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var payload struct {
		Runs []monitorRunRecord `json:"runs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Runs) != 1 {
		t.Fatalf("expected one monitor run, got %+v", payload.Runs)
	}
	if payload.Runs[0].RunID != "run-stale" {
		t.Fatalf("expected run-stale record, got %+v", payload.Runs[0])
	}
	if payload.Runs[0].InstanceID != instances.DefaultInstanceID {
		t.Fatalf("expected default instance id, got %+v", payload.Runs[0])
	}
	if payload.Runs[0].Status != "canceled" {
		t.Fatalf("expected canceled status after successful cancel reconciliation, got %+v", payload.Runs[0])
	}
	if payload.Runs[0].CompletedAt == "" {
		t.Fatalf("expected completed_at to be populated after reconciliation, got %+v", payload.Runs[0])
	}
}

func TestMonitorRunControlReconcilesUntrackedStaleRunAfterCancelFallback(t *testing.T) {
	root := t.TempDir()
	auditDir := filepath.Join(root, ".openclawssy", "agents", "alpha", "audit")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatalf("mkdir audit dir: %v", err)
	}
	auditBody := `{"ts":"2026-03-14T12:00:00Z","type":"run.start","run_id":"run-stale-untracked","agent_id":"alpha","payload":{"source":"scheduler/system","message":"stale maintenance"}}` + "\n"
	if err := os.WriteFile(filepath.Join(auditDir, "events.jsonl"), []byte(auditBody), 0o600); err != nil {
		t.Fatalf("write audit log: %v", err)
	}

	canceller := &stubDashboardRunCanceller{tracked: map[string]bool{}}
	h := NewWithOptions(root, httpchannel.NewInMemoryRunStore(), Options{RunCanceller: canceller})
	mux := http.NewServeMux()
	h.Register(mux)

	cancelReq := httptest.NewRequest(http.MethodPost, "/api/admin/monitor/runs/control", bytes.NewBufferString(`{"action":"cancel","run_id":"run-stale-untracked"}`))
	cancelReq.Header.Set("Content-Type", "application/json")
	cancelResp := httptest.NewRecorder()
	mux.ServeHTTP(cancelResp, cancelReq)
	if cancelResp.Code != http.StatusOK {
		t.Fatalf("expected cancel status 200, got %d (%s)", cancelResp.Code, cancelResp.Body.String())
	}
	var cancelPayload map[string]any
	if err := json.Unmarshal(cancelResp.Body.Bytes(), &cancelPayload); err != nil {
		t.Fatalf("decode cancel payload: %v", err)
	}
	if cancelPayload["cancelled"] != true {
		t.Fatalf("expected cancelled=true for untracked stale fallback, got %#v", cancelPayload)
	}
	if cancelPayload["tracked"] != false {
		t.Fatalf("expected tracked=false for untracked stale fallback, got %#v", cancelPayload)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/monitor/runs", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var payload struct {
		Runs []monitorRunRecord `json:"runs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Runs) != 1 {
		t.Fatalf("expected one monitor run, got %+v", payload.Runs)
	}
	if payload.Runs[0].RunID != "run-stale-untracked" || payload.Runs[0].Status != "canceled" {
		t.Fatalf("expected run-stale-untracked canceled after fallback reconciliation, got %+v", payload.Runs[0])
	}
	if payload.Runs[0].InstanceID != instances.DefaultInstanceID {
		t.Fatalf("expected default instance id, got %+v", payload.Runs[0])
	}
	if payload.Runs[0].CompletedAt == "" {
		t.Fatalf("expected completed_at to be populated after fallback reconciliation, got %+v", payload.Runs[0])
	}
}

func TestMonitorRunsEndpointRetiresStaleUntrackedAuditOnlyRunningRuns(t *testing.T) {
	root := t.TempDir()
	auditDir := filepath.Join(root, ".openclawssy", "agents", "default", "audit")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatalf("mkdir audit dir: %v", err)
	}

	now := time.Now().UTC()
	oldStartedAt := now.Add(-(monitorStaleUntrackedRunTTL + 2*time.Minute)).Format(time.RFC3339)
	recentStartedAt := now.Add(-(monitorStaleUntrackedRunTTL / 4)).Format(time.RFC3339)
	auditBody := strings.Join([]string{
		fmt.Sprintf(`{"ts":"%s","type":"run.start","run_id":"run-orphan-old","agent_id":"default","payload":{"source":"scheduler/system","message":"stale orphan"}}`, oldStartedAt),
		fmt.Sprintf(`{"ts":"%s","type":"run.start","run_id":"run-orphan-recent","agent_id":"default","payload":{"source":"scheduler/system","message":"still active"}}`, recentStartedAt),
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(auditDir, "events.jsonl"), []byte(auditBody), 0o600); err != nil {
		t.Fatalf("write audit log: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/monitor/runs", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var payload struct {
		Runs []monitorRunRecord `json:"runs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Runs) != 2 {
		t.Fatalf("expected 2 monitor runs, got %+v", payload.Runs)
	}

	byRunID := make(map[string]monitorRunRecord, len(payload.Runs))
	for _, record := range payload.Runs {
		byRunID[record.RunID] = record
	}

	oldRecord, ok := byRunID["run-orphan-old"]
	if !ok {
		t.Fatalf("expected stale orphan run in payload, got %+v", payload.Runs)
	}
	if oldRecord.Status != "failed" {
		t.Fatalf("expected stale orphan run to be retired to failed, got %+v", oldRecord)
	}
	if oldRecord.CompletedAt == "" {
		t.Fatalf("expected stale orphan run completed_at to be populated, got %+v", oldRecord)
	}
	if !strings.Contains(strings.ToLower(oldRecord.Error), "stale") {
		t.Fatalf("expected stale orphan retirement error detail, got %+v", oldRecord)
	}

	recentRecord, ok := byRunID["run-orphan-recent"]
	if !ok {
		t.Fatalf("expected recent orphan run in payload, got %+v", payload.Runs)
	}
	if recentRecord.Status != "running" {
		t.Fatalf("expected recent orphan run to remain running, got %+v", recentRecord)
	}
}

func TestMonitorRunsEndpointUsesStoreTerminalStatusForMatchingRunID(t *testing.T) {
	root := t.TempDir()
	auditDir := filepath.Join(root, ".openclawssy", "agents", "default", "audit")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatalf("mkdir audit dir: %v", err)
	}
	auditBody := `{"ts":"2026-03-14T12:05:00Z","type":"run.start","run_id":"run-store","agent_id":"default","payload":{"source":"dashboard","message":"started"}}` + "\n"
	if err := os.WriteFile(filepath.Join(auditDir, "events.jsonl"), []byte(auditBody), 0o600); err != nil {
		t.Fatalf("write audit log: %v", err)
	}

	store := httpchannel.NewInMemoryRunStore()
	now := time.Now().UTC()
	if _, err := store.Create(context.Background(), httpchannel.Run{
		ID:        "run-store",
		AgentID:   "default",
		Message:   "started",
		Source:    "dashboard",
		Status:    "canceled",
		Error:     "run canceled",
		CreatedAt: now.Add(-2 * time.Second),
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create run: %v", err)
	}

	h := New(root, store)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/monitor/runs", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var payload struct {
		Runs []monitorRunRecord `json:"runs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Runs) != 1 {
		t.Fatalf("expected one monitor run, got %+v", payload.Runs)
	}
	if payload.Runs[0].Status != "canceled" {
		t.Fatalf("expected monitor status to reconcile with store terminal status, got %+v", payload.Runs[0])
	}
	if payload.Runs[0].InstanceID != instances.DefaultInstanceID {
		t.Fatalf("expected default instance id, got %+v", payload.Runs[0])
	}
}

func TestMonitorRunsEndpointIncludesStoreRunAfterCancelWhenAuditMissing(t *testing.T) {
	root := t.TempDir()
	store := httpchannel.NewInMemoryRunStore()
	now := time.Now().UTC()
	if _, err := store.Create(context.Background(), httpchannel.Run{
		ID:        "run-chat-queued",
		AgentID:   "default",
		Message:   "chat task",
		Source:    "dashboard",
		Status:    "running",
		CreatedAt: now.Add(-3 * time.Second),
		UpdatedAt: now.Add(-3 * time.Second),
	}); err != nil {
		t.Fatalf("create queued run: %v", err)
	}
	canceller := &stubDashboardRunCanceller{
		tracked: map[string]bool{"run-chat-queued": true},
		onCancel: func(_ string) {
			run, err := store.Get(context.Background(), "run-chat-queued")
			if err != nil {
				return
			}
			run.Status = "canceled"
			run.Error = "run canceled"
			run.UpdatedAt = time.Now().UTC()
			_ = store.Update(context.Background(), run)
		},
	}

	h := NewWithOptions(root, store, Options{RunCanceller: canceller})
	mux := http.NewServeMux()
	h.Register(mux)

	cancelReq := httptest.NewRequest(http.MethodPost, "/api/admin/monitor/runs/control", bytes.NewBufferString(`{"action":"cancel","run_id":"run-chat-queued"}`))
	cancelReq.Header.Set("Content-Type", "application/json")
	cancelResp := httptest.NewRecorder()
	mux.ServeHTTP(cancelResp, cancelReq)
	if cancelResp.Code != http.StatusOK {
		t.Fatalf("expected cancel status 200, got %d (%s)", cancelResp.Code, cancelResp.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/monitor/runs", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var payload struct {
		Runs []monitorRunRecord `json:"runs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Runs) != 1 {
		t.Fatalf("expected one run from store reconciliation, got %+v", payload.Runs)
	}
	if payload.Runs[0].RunID != "run-chat-queued" || payload.Runs[0].Status != "canceled" {
		t.Fatalf("expected canceled store-backed monitor run, got %+v", payload.Runs[0])
	}
	if payload.Runs[0].InstanceID != instances.DefaultInstanceID {
		t.Fatalf("expected default instance id, got %+v", payload.Runs[0])
	}
}

func TestMonitorRunsEndpointListsMainAndSubagentAuditRuns(t *testing.T) {
	root := t.TempDir()
	auditDir := filepath.Join(root, ".openclawssy", "agents", "alpha", "audit")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatalf("mkdir audit dir: %v", err)
	}
	auditBody := strings.Join([]string{
		`{"ts":"2026-03-05T10:00:00Z","type":"run.start","run_id":"run-main","agent_id":"alpha","payload":{"source":"dashboard","message":"main task","task_id":"cdf-main-1","model_provider":"hatz","model_name":"hatz-coder"}}`,
		`{"ts":"2026-03-05T10:00:02Z","type":"run.end","run_id":"run-main","agent_id":"alpha","payload":{"artifact_path":"/tmp/run-main","checkpoint_path":"clawdefuckifier/alpha/runs/run-main.md"}}`,
		`{"ts":"2026-03-05T10:01:00Z","type":"run.start","run_id":"run-sub","agent_id":"alpha","payload":{"source":"subagent/delegation","message":"sub task","task_id":"cdf-diagnose-2","model_provider":"hatz","model_name":"hatz-coder"}}`,
		`{"ts":"2026-03-05T10:01:04Z","type":"run.end","run_id":"run-sub","agent_id":"alpha","payload":{"error":"timeout","checkpoint_path":"clawdefuckifier/alpha/runs/run-sub.md"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(auditDir, "events.jsonl"), []byte(auditBody), 0o600); err != nil {
		t.Fatalf("write audit log: %v", err)
	}

	h := NewWithOptions(root, httpchannel.NewInMemoryRunStore(), Options{RunCanceller: &stubDashboardRunCanceller{tracked: map[string]bool{"run-sub": true}}})
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/monitor/runs", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var payload struct {
		Runs []monitorRunRecord `json:"runs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Runs) != 2 {
		t.Fatalf("expected 2 monitor runs, got %+v", payload.Runs)
	}
	if payload.Runs[0].RunID != "run-sub" || payload.Runs[0].Role != "subagent" || payload.Runs[0].Status != "failed" {
		t.Fatalf("unexpected subagent run record: %+v", payload.Runs[0])
	}
	if payload.Runs[0].InstanceID != instances.DefaultInstanceID {
		t.Fatalf("expected default instance id, got %+v", payload.Runs[0])
	}
	if payload.Runs[0].TaskID != "cdf-diagnose-2" || payload.Runs[0].ModelProvider != "hatz" || payload.Runs[0].ModelName != "hatz-coder" {
		t.Fatalf("expected task/model metadata on subagent run, got %+v", payload.Runs[0])
	}
	if payload.Runs[0].CheckpointPath != "clawdefuckifier/alpha/runs/run-sub.md" {
		t.Fatalf("expected checkpoint path on subagent run, got %+v", payload.Runs[0])
	}
	if payload.Runs[1].RunID != "run-main" || payload.Runs[1].Role != "main" || payload.Runs[1].Status != "completed" {
		t.Fatalf("unexpected main run record: %+v", payload.Runs[1])
	}
	if payload.Runs[1].TaskID != "cdf-main-1" || payload.Runs[1].ModelProvider != "hatz" || payload.Runs[1].ModelName != "hatz-coder" {
		t.Fatalf("expected task/model metadata on main run, got %+v", payload.Runs[1])
	}
	if payload.Runs[1].CheckpointPath != "clawdefuckifier/alpha/runs/run-main.md" {
		t.Fatalf("expected checkpoint path on main run, got %+v", payload.Runs[1])
	}
}

func TestMonitorRunsEndpointSkipsUnreadableAuditFiles(t *testing.T) {
	root := t.TempDir()

	alphaAuditDir := filepath.Join(root, ".openclawssy", "agents", "alpha", "audit")
	if err := os.MkdirAll(alphaAuditDir, 0o755); err != nil {
		t.Fatalf("mkdir alpha audit dir: %v", err)
	}
	alphaAudit := `{"ts":"2026-03-05T10:00:00Z","type":"run.start","run_id":"run-alpha","agent_id":"alpha","payload":{"source":"dashboard","message":"main task"}}` + "\n"
	if err := os.WriteFile(filepath.Join(alphaAuditDir, "events.jsonl"), []byte(alphaAudit), 0o600); err != nil {
		t.Fatalf("write alpha audit log: %v", err)
	}

	blockedAuditDir := filepath.Join(root, ".openclawssy", "agents", "blocked", "audit")
	if err := os.MkdirAll(blockedAuditDir, 0o755); err != nil {
		t.Fatalf("mkdir blocked audit dir: %v", err)
	}
	blockedPath := filepath.Join(blockedAuditDir, "events.jsonl")
	if err := os.WriteFile(blockedPath, []byte(alphaAudit), 0o600); err != nil {
		t.Fatalf("write blocked audit log: %v", err)
	}
	if err := os.Chmod(blockedPath, 0o000); err != nil {
		t.Fatalf("chmod blocked audit log: %v", err)
	}
	defer func() { _ = os.Chmod(blockedPath, 0o600) }()

	if probe, err := os.Open(blockedPath); err == nil {
		_ = probe.Close()
		t.Skip("unable to simulate permission-denied audit file on this platform/user")
	} else if !errors.Is(err, os.ErrPermission) {
		t.Skipf("expected permission error probe; got: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/monitor/runs", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var payload struct {
		Runs []monitorRunRecord `json:"runs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Runs) != 1 {
		t.Fatalf("expected one readable run, got %+v", payload.Runs)
	}
	if payload.Runs[0].RunID != "run-alpha" {
		t.Fatalf("expected readable alpha run, got %+v", payload.Runs[0])
	}
}

func TestMonitorRunsEndpointToleratesOversizedAuditLines(t *testing.T) {
	root := t.TempDir()
	auditDir := filepath.Join(root, ".openclawssy", "agents", "alpha", "audit")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatalf("mkdir audit dir: %v", err)
	}

	oversizedInvalidLine := strings.Repeat("x", 2*1024*1024)
	auditBody := oversizedInvalidLine + "\n" + strings.Join([]string{
		`{"ts":"2026-03-05T10:05:00Z","type":"run.start","run_id":"run-good","agent_id":"alpha","payload":{"source":"dashboard","message":"after oversized"}}`,
		`{"ts":"2026-03-05T10:05:04Z","type":"run.end","run_id":"run-good","agent_id":"alpha","payload":{}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(auditDir, "events.jsonl"), []byte(auditBody), 0o600); err != nil {
		t.Fatalf("write audit log: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/monitor/runs", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var payload struct {
		Runs []monitorRunRecord `json:"runs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Runs) != 1 {
		t.Fatalf("expected one parsed run after oversized line, got %+v", payload.Runs)
	}
	if payload.Runs[0].RunID != "run-good" || payload.Runs[0].Status != "completed" {
		t.Fatalf("expected completed run-good record, got %+v", payload.Runs[0])
	}
}

func TestMonitorRunsEndpointSeparatesSameRunIDAcrossInstances(t *testing.T) {
	root := t.TempDir()
	if _, err := instances.BootstrapDefaultInstance(root); err != nil {
		t.Fatalf("bootstrap default instance: %v", err)
	}
	lab := instances.DefaultInstanceManifest("lab")
	lab.Workspace.Root = filepath.Join(root, "workspace")
	if err := instances.SaveInstanceManifest(root, lab); err != nil {
		t.Fatalf("save lab instance: %v", err)
	}
	if err := instances.SaveAgentManifest(root, "lab", instances.DefaultAgentManifest("alpha")); err != nil {
		t.Fatalf("save lab agent: %v", err)
	}
	legacyAuditDir := filepath.Join(root, ".openclawssy", "agents", "alpha", "audit")
	if err := os.MkdirAll(legacyAuditDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy audit dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyAuditDir, "events.jsonl"), []byte(`{"ts":"2026-03-05T10:00:00Z","type":"run.start","run_id":"run-shared","agent_id":"alpha","payload":{"source":"dashboard","message":"default instance"}}`+"\n"), 0o600); err != nil {
		t.Fatalf("write legacy audit: %v", err)
	}
	labAuditDir := filepath.Join(root, ".openclawssy", "instances", "lab", "agents", "alpha", "audit")
	if err := os.MkdirAll(labAuditDir, 0o755); err != nil {
		t.Fatalf("mkdir lab audit dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(labAuditDir, "events.jsonl"), []byte(`{"ts":"2026-03-05T11:00:00Z","type":"run.start","run_id":"run-shared","agent_id":"alpha","payload":{"source":"dashboard","message":"lab instance"}}`+"\n"), 0o600); err != nil {
		t.Fatalf("write lab audit: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/monitor/runs", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var payload struct {
		Runs []monitorRunRecord `json:"runs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Runs) != 2 {
		t.Fatalf("expected two runs, got %+v", payload.Runs)
	}
	seen := map[string]bool{}
	for _, run := range payload.Runs {
		seen[run.InstanceID+":"+run.RunID] = true
	}
	if !seen[instances.DefaultInstanceID+":run-shared"] || !seen["lab:run-shared"] {
		t.Fatalf("expected separate run identities across instances, got %+v", payload.Runs)
	}
}

func TestMonitorRunControlUsesCompositeIdentityWhenProvided(t *testing.T) {
	root := t.TempDir()
	canceller := &stubDashboardRunCanceller{tracked: map[string]bool{"lab:alpha:run-composite": true}}
	h := NewWithOptions(root, httpchannel.NewInMemoryRunStore(), Options{RunCanceller: canceller})
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/monitor/runs/control", bytes.NewBufferString(`{"action":"cancel","instance_id":"lab","agent_id":"alpha","run_id":"run-composite"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	if len(canceller.called) == 0 || canceller.called[0] != "lab:alpha:run-composite" {
		t.Fatalf("expected composite cancel call, got %#v", canceller.called)
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode control payload: %v", err)
	}
	if payload["instance_id"] != "lab" || payload["agent_id"] != "alpha" {
		t.Fatalf("expected identity echoed in control response, got %#v", payload)
	}
}

func TestMonitorRoutesRequireInstanceAgentsFeature(t *testing.T) {
	root := t.TempDir()
	h := New(root, httpchannel.NewInMemoryRunStore())
	store := defaultControlPlaneStore()
	store.Features.InstanceAgents = false
	if err := h.saveControlPlaneStore(store); err != nil {
		t.Fatalf("save control plane store: %v", err)
	}
	mux := http.NewServeMux()
	h.Register(mux)

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/monitor/runs", nil)
	listRR := httptest.NewRecorder()
	mux.ServeHTTP(listRR, listReq)
	if listRR.Code != http.StatusForbidden {
		t.Fatalf("expected monitor list status %d, got %d (%s)", http.StatusForbidden, listRR.Code, listRR.Body.String())
	}
	assertDashboardErrorCode(t, listRR.Body.Bytes(), "feature.instance_agents_disabled")

	controlReq := httptest.NewRequest(http.MethodPost, "/api/admin/monitor/runs/control", bytes.NewBufferString(`{"action":"cancel","run_id":"run-1"}`))
	controlReq.Header.Set("Content-Type", "application/json")
	controlRR := httptest.NewRecorder()
	mux.ServeHTTP(controlRR, controlReq)
	if controlRR.Code != http.StatusForbidden {
		t.Fatalf("expected monitor control status %d, got %d (%s)", http.StatusForbidden, controlRR.Code, controlRR.Body.String())
	}
	assertDashboardErrorCode(t, controlRR.Body.Bytes(), "feature.instance_agents_disabled")
}

func TestSessionsRoutesRequireInstanceAgentsFeature(t *testing.T) {
	root := t.TempDir()
	h := New(root, httpchannel.NewInMemoryRunStore())
	store := defaultControlPlaneStore()
	store.Features.InstanceAgents = false
	if err := h.saveControlPlaneStore(store); err != nil {
		t.Fatalf("save control plane store: %v", err)
	}
	mux := http.NewServeMux()
	h.Register(mux)

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/chat/sessions", nil)
	listRR := httptest.NewRecorder()
	mux.ServeHTTP(listRR, listReq)
	if listRR.Code != http.StatusForbidden {
		t.Fatalf("expected sessions list status %d, got %d (%s)", http.StatusForbidden, listRR.Code, listRR.Body.String())
	}
	assertDashboardErrorCode(t, listRR.Body.Bytes(), "feature.instance_agents_disabled")

	messagesReq := httptest.NewRequest(http.MethodGet, "/api/admin/chat/sessions/session-1/messages?limit=10", nil)
	messagesRR := httptest.NewRecorder()
	mux.ServeHTTP(messagesRR, messagesReq)
	if messagesRR.Code != http.StatusForbidden {
		t.Fatalf("expected session messages status %d, got %d (%s)", http.StatusForbidden, messagesRR.Code, messagesRR.Body.String())
	}
	assertDashboardErrorCode(t, messagesRR.Body.Bytes(), "feature.instance_agents_disabled")
}

func TestListChatSessionsEndpointInvalidLimit(t *testing.T) {
	root := t.TempDir()
	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/chat/sessions?limit=0", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestChatSessionMessagesEndpoint(t *testing.T) {
	root := t.TempDir()
	store, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	session, err := store.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "dashboard_user", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.AppendMessage(session.SessionID, chatstore.Message{Role: "user", Content: "hello"}); err != nil {
		t.Fatalf("append message: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/chat/sessions/"+session.SessionID+"/messages?limit=10", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	msgs, ok := payload["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("expected one message, got %#v", payload["messages"])
	}
}

func TestChatSessionMessagesEndpointIncludesToolMetadata(t *testing.T) {
	root := t.TempDir()
	store, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	session, err := store.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "dashboard_user", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.AppendMessage(session.SessionID, chatstore.Message{
		Role:       "tool",
		Content:    `{"tool":"fs.list","id":"tool-json-1","output":"{\"entries\":[\"a.txt\"]}"}`,
		RunID:      "run_42",
		ToolCallID: "tool-json-1",
		ToolName:   "fs.list",
	}); err != nil {
		t.Fatalf("append tool message: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/chat/sessions/"+session.SessionID+"/messages?limit=10", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	msgs, ok := payload["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("expected one message, got %#v", payload["messages"])
	}
	msg, ok := msgs[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected message shape: %#v", msgs[0])
	}
	if msg["role"] != "tool" {
		t.Fatalf("expected role=tool, got %#v", msg["role"])
	}
	if msg["tool_name"] != "fs.list" || msg["tool_call_id"] != "tool-json-1" {
		t.Fatalf("expected tool metadata to round-trip, got %#v", msg)
	}
	if msg["run_id"] != "run_42" {
		t.Fatalf("expected run id to round-trip, got %#v", msg["run_id"])
	}
}

func TestChatSessionMessagesEndpointIncludesLifecycleMetadata(t *testing.T) {
	root := t.TempDir()
	store, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	session, err := store.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "dashboard_user", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.AppendMessage(session.SessionID, chatstore.Message{
		Role:            "system",
		Content:         `{"message":"queued for execution"}`,
		MessageID:       "msg_123",
		Status:          "acknowledged",
		InstanceID:      "lab",
		FromAgentID:     "planner",
		ToAgentID:       "implementer",
		TaskID:          "task_9",
		Subject:         "handoff",
		SourceSessionID: "source_session_1",
		RelatedRunID:    "run_314",
		Note:            "dashboard acknowledged",
		Error:           "",
	}); err != nil {
		t.Fatalf("append lifecycle message: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/chat/sessions/"+session.SessionID+"/messages?limit=10", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	msgs, ok := payload["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("expected one message, got %#v", payload["messages"])
	}
	msg, ok := msgs[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected message shape: %#v", msgs[0])
	}
	if msg["message_id"] != "msg_123" || msg["status"] != "acknowledged" {
		t.Fatalf("expected lifecycle id/status fields, got %#v", msg)
	}
	if msg["instance_id"] != "lab" || msg["from_agent_id"] != "planner" || msg["to_agent_id"] != "implementer" {
		t.Fatalf("expected lifecycle routing fields, got %#v", msg)
	}
	if msg["task_id"] != "task_9" || msg["subject"] != "handoff" || msg["source_session_id"] != "source_session_1" {
		t.Fatalf("expected lifecycle linkage fields, got %#v", msg)
	}
	if msg["related_run_id"] != "run_314" || msg["note"] != "dashboard acknowledged" {
		t.Fatalf("expected lifecycle note/run fields, got %#v", msg)
	}
}

func TestChatSessionMessagesEndpointPreservesMultiStepOrder(t *testing.T) {
	root := t.TempDir()
	store, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	session, err := store.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "dashboard_user", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sequence := []chatstore.Message{
		{Role: "user", Content: "list files"},
		{Role: "tool", Content: `{"tool":"fs.list","id":"tool-json-1","output":"{\"entries\":[\"a.txt\"]}"}`, ToolCallID: "tool-json-1", ToolName: "fs.list", RunID: "run_1"},
		{Role: "tool", Content: `{"tool":"fs.read","id":"tool-json-2","output":"hello"}`, ToolCallID: "tool-json-2", ToolName: "fs.read", RunID: "run_1"},
		{Role: "assistant", Content: "I found a.txt and read it."},
	}
	for _, msg := range sequence {
		if err := store.AppendMessage(session.SessionID, msg); err != nil {
			t.Fatalf("append message: %v", err)
		}
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/chat/sessions/"+session.SessionID+"/messages?limit=10", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	msgs, ok := payload["messages"].([]any)
	if !ok || len(msgs) != 4 {
		t.Fatalf("expected four messages, got %#v", payload["messages"])
	}

	roleAt := func(i int) string {
		item, _ := msgs[i].(map[string]any)
		if item == nil {
			return ""
		}
		v, _ := item["role"].(string)
		return v
	}
	if roleAt(0) != "user" || roleAt(1) != "tool" || roleAt(2) != "tool" || roleAt(3) != "assistant" {
		t.Fatalf("unexpected message ordering: %#v", msgs)
	}
	tool1, _ := msgs[1].(map[string]any)
	tool2, _ := msgs[2].(map[string]any)
	if tool1["tool_call_id"] != "tool-json-1" || tool2["tool_call_id"] != "tool-json-2" {
		t.Fatalf("expected distinct tool call ids in order, got %#v and %#v", tool1, tool2)
	}
}

func TestSchedulerAdminEndpointsCRUDAndPauseResume(t *testing.T) {
	root := t.TempDir()
	jobStore, err := scheduler.NewStore(filepath.Join(root, ".openclawssy", "scheduler", "jobs.json"))
	if err != nil {
		t.Fatalf("new scheduler store: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore(), jobStore)
	mux := http.NewServeMux()
	h.Register(mux)

	addReq := httptest.NewRequest(http.MethodPost, "/api/admin/scheduler/jobs", bytes.NewBufferString(`{"schedule":"@every 1m","message":"status ping"}`))
	addResp := httptest.NewRecorder()
	mux.ServeHTTP(addResp, addReq)
	if addResp.Code != http.StatusOK {
		t.Fatalf("expected add job 200, got %d (%s)", addResp.Code, addResp.Body.String())
	}
	var addPayload map[string]any
	if err := json.Unmarshal(addResp.Body.Bytes(), &addPayload); err != nil {
		t.Fatalf("decode add response: %v", err)
	}
	jobID, _ := addPayload["id"].(string)
	if jobID == "" {
		t.Fatalf("expected returned job id, got %#v", addPayload)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/scheduler/jobs", nil)
	listResp := httptest.NewRecorder()
	mux.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected list jobs 200, got %d", listResp.Code)
	}
	var listPayload map[string]any
	if err := json.Unmarshal(listResp.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	jobs, ok := listPayload["jobs"].([]any)
	if !ok || len(jobs) != 1 {
		t.Fatalf("expected one scheduler job, got %#v", listPayload["jobs"])
	}
	stored := jobStore.List()[0]
	if stored.Channel != "dashboard" || stored.UserID != "dashboard_user" || stored.RoomID != "dashboard" {
		t.Fatalf("expected dashboard default delivery metadata, got %+v", stored)
	}

	pauseReq := httptest.NewRequest(http.MethodPost, "/api/admin/scheduler/control", bytes.NewBufferString(`{"action":"pause"}`))
	pauseResp := httptest.NewRecorder()
	mux.ServeHTTP(pauseResp, pauseReq)
	if pauseResp.Code != http.StatusOK {
		t.Fatalf("expected global pause 200, got %d", pauseResp.Code)
	}
	if !jobStore.IsPaused() {
		t.Fatal("expected scheduler paused state after pause action")
	}

	jobPauseReq := httptest.NewRequest(http.MethodPost, "/api/admin/scheduler/control", bytes.NewBufferString(`{"action":"pause","job_id":"`+jobID+`"}`))
	jobPauseResp := httptest.NewRecorder()
	mux.ServeHTTP(jobPauseResp, jobPauseReq)
	if jobPauseResp.Code != http.StatusOK {
		t.Fatalf("expected per-job pause 200, got %d", jobPauseResp.Code)
	}
	if jobStore.List()[0].Enabled {
		t.Fatalf("expected paused job to be disabled: %+v", jobStore.List()[0])
	}

	jobResumeReq := httptest.NewRequest(http.MethodPost, "/api/admin/scheduler/control", bytes.NewBufferString(`{"action":"resume","job_id":"`+jobID+`"}`))
	jobResumeResp := httptest.NewRecorder()
	mux.ServeHTTP(jobResumeResp, jobResumeReq)
	if jobResumeResp.Code != http.StatusOK {
		t.Fatalf("expected per-job resume 200, got %d", jobResumeResp.Code)
	}
	if !jobStore.List()[0].Enabled {
		t.Fatalf("expected resumed job to be enabled: %+v", jobStore.List()[0])
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/scheduler/jobs/"+jobID, nil)
	deleteResp := httptest.NewRecorder()
	mux.ServeHTTP(deleteResp, deleteReq)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("expected delete job 200, got %d", deleteResp.Code)
	}
	if len(jobStore.List()) != 0 {
		t.Fatalf("expected empty scheduler after deletion, got %+v", jobStore.List())
	}
}

func newOAuthTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	masterPath := filepath.Join(root, ".openclawssy", "master.key")
	if _, err := secrets.GenerateAndWriteMasterKey(masterPath); err != nil {
		t.Fatalf("generate master key: %v", err)
	}
	cfg := config.Default()
	cfg.Secrets.MasterKeyFile = masterPath
	cfg.Secrets.StoreFile = filepath.Join(root, ".openclawssy", "secrets.enc")
	if err := config.Save(filepath.Join(root, ".openclawssy", "config.json"), cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	return root
}

func TestAdminOAuthStatusReturnsProviderStatus(t *testing.T) {
	root := newOAuthTestRoot(t)

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/oauth/status", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	providers, ok := payload["providers"].(map[string]any)
	if !ok {
		t.Fatalf("expected providers map, got %#v", payload["providers"])
	}
	for _, name := range []string{"openai_codex", "anthropic"} {
		entry, ok := providers[name].(map[string]any)
		if !ok {
			t.Fatalf("expected %s entry as map, got %#v", name, providers[name])
		}
		if entry["status"] != "not_configured" {
			t.Fatalf("expected %s status=not_configured, got %#v", name, entry["status"])
		}
	}
}

func TestAdminOAuthStatusMethodNotAllowed(t *testing.T) {
	root := newOAuthTestRoot(t)
	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/oauth/status", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, rr.Code)
	}
}

func TestAdminOAuthStartReturnsSessionAndAuthorizeURL(t *testing.T) {
	root := newOAuthTestRoot(t)
	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/oauth/openai_codex/start", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	sessionID, ok := payload["session_id"].(string)
	if !ok || sessionID == "" {
		t.Fatalf("expected non-empty session_id, got %#v", payload["session_id"])
	}
	authorizeURL, ok := payload["authorize_url"].(string)
	if !ok || authorizeURL == "" {
		t.Fatalf("expected non-empty authorize_url, got %#v", payload["authorize_url"])
	}
	if !strings.Contains(authorizeURL, "response_type=code") {
		t.Fatalf("expected authorize_url to contain response_type=code, got %q", authorizeURL)
	}
	if !strings.Contains(authorizeURL, "code_challenge_method=S256") {
		t.Fatalf("expected authorize_url to contain code_challenge_method=S256, got %q", authorizeURL)
	}
	expiresAt, ok := payload["expires_at"].(string)
	if !ok || expiresAt == "" {
		t.Fatalf("expected non-empty expires_at, got %#v", payload["expires_at"])
	}
}

func TestAdminOAuthStartAnthropicProvider(t *testing.T) {
	root := newOAuthTestRoot(t)
	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/oauth/anthropic/start", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := payload["session_id"].(string); !ok {
		t.Fatalf("expected session_id string, got %#v", payload["session_id"])
	}
}

func TestAdminOAuthDeleteRemovesCredentials(t *testing.T) {
	root := newOAuthTestRoot(t)

	// Pre-store a fake credential so delete has something to remove.
	cfg, _ := config.LoadOrDefault(filepath.Join(root, ".openclawssy", "config.json"))
	store, err := secrets.NewStore(cfg)
	if err != nil {
		t.Fatalf("new secret store: %v", err)
	}
	if err := store.Set("oauth/openai_codex/access_token", "fake-token"); err != nil {
		t.Fatalf("set fake token: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	// Verify status shows active before delete.
	statusReq := httptest.NewRequest(http.MethodGet, "/api/admin/oauth/status", nil)
	statusResp := httptest.NewRecorder()
	mux.ServeHTTP(statusResp, statusReq)
	if statusResp.Code != http.StatusOK {
		t.Fatalf("status expected 200, got %d", statusResp.Code)
	}
	var statusPayload map[string]any
	if err := json.Unmarshal(statusResp.Body.Bytes(), &statusPayload); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	providers := statusPayload["providers"].(map[string]any)
	if entry, ok := providers["openai_codex"].(map[string]any); !ok || entry["status"] == "not_configured" {
		t.Fatalf("expected openai_codex active before delete, got %#v", providers["openai_codex"])
	}

	// Delete credentials.
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/oauth/openai_codex", nil)
	deleteResp := httptest.NewRecorder()
	mux.ServeHTTP(deleteResp, deleteReq)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d (%s)", http.StatusOK, deleteResp.Code, deleteResp.Body.String())
	}
	var deletePayload map[string]any
	if err := json.Unmarshal(deleteResp.Body.Bytes(), &deletePayload); err != nil {
		t.Fatalf("decode delete response: %v", err)
	}
	if deletePayload["ok"] != true {
		t.Fatalf("expected ok=true, got %#v", deletePayload)
	}
	if deletePayload["provider"] != "openai_codex" {
		t.Fatalf("expected provider=openai_codex, got %#v", deletePayload["provider"])
	}

	// Status should now show not_configured.
	statusReq2 := httptest.NewRequest(http.MethodGet, "/api/admin/oauth/status", nil)
	statusResp2 := httptest.NewRecorder()
	mux.ServeHTTP(statusResp2, statusReq2)
	if statusResp2.Code != http.StatusOK {
		t.Fatalf("status2 expected 200, got %d", statusResp2.Code)
	}
	var statusPayload2 map[string]any
	if err := json.Unmarshal(statusResp2.Body.Bytes(), &statusPayload2); err != nil {
		t.Fatalf("decode status2: %v", err)
	}
	providers2 := statusPayload2["providers"].(map[string]any)
	entry2, ok := providers2["openai_codex"].(map[string]any)
	if !ok {
		t.Fatalf("expected openai_codex map, got %#v", providers2["openai_codex"])
	}
	if entry2["status"] != "not_configured" {
		t.Fatalf("expected not_configured after delete, got %#v", entry2["status"])
	}
}

func TestAdminOAuthInvalidProviderReturnsError(t *testing.T) {
	root := newOAuthTestRoot(t)
	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/oauth/invalid_provider/start", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d (%s)", http.StatusBadRequest, rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	errObj, ok := payload["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %#v", payload)
	}
	if errObj["code"] != "oauth.invalid_provider" {
		t.Fatalf("expected oauth.invalid_provider code, got %#v", errObj["code"])
	}
}

func TestAdminOAuthCompleteExpiredSessionReturnsError(t *testing.T) {
	root := newOAuthTestRoot(t)
	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	body := bytes.NewBufferString(`{"session_id":"nonexistent-session-id","input":"authcode123"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/oauth/openai_codex/complete", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d (%s)", http.StatusBadRequest, rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	errObj, ok := payload["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %#v", payload)
	}
	if errObj["code"] != "oauth.session_expired" {
		t.Fatalf("expected oauth.session_expired code, got %#v", errObj["code"])
	}
}
