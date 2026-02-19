package httpchannel

// SandboxAdminHandler handles admin API endpoints for Docker sandbox management.
// It is mounted under /api/admin/sandbox/docker/ and provides operators with
// visibility and control over the agent Docker sandbox lifecycle.
//
// All endpoints require a valid bearer token (enforced by the existing
// authMiddleware in server.go — no secrets are ever returned in responses).
//
// Supported routes (registered via Register):
//
//	GET  /api/admin/sandbox/docker/status
//	POST /api/admin/sandbox/docker/create
//	POST /api/admin/sandbox/docker/stop
//	POST /api/admin/sandbox/docker/reset
//	POST /api/admin/sandbox/docker/pull
//	GET  /api/admin/sandbox/docker/images
//	GET  /api/admin/sandbox/docker/volumes
//	DELETE /api/admin/sandbox/docker/volume

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// ── DockerAdminService interface ──────────────────────────────────────────────

// DockerAdminService is the interface that SandboxAdminHandler depends on.
// It is satisfied by DockerAdminManager (production) and can be mocked in tests.
type DockerAdminService interface {
	// Status returns the current state of the named agent container.
	Status(ctx context.Context, agentID string) (DockerStatusResponse, error)

	// CreateContainer ensures the named volume and container exist and starts
	// the container.
	CreateContainer(ctx context.Context, agentID string) error

	// StopContainer stops (but does not remove) the container.
	StopContainer(ctx context.Context, agentID string) error

	// ResetContainer stops + removes the container (volume is kept).
	ResetContainer(ctx context.Context, agentID string) error

	// PullImage pulls a Docker image by its reference (e.g. "ubuntu:24.04").
	PullImage(ctx context.Context, image string) error

	// ListImages returns the locally available Docker images.
	ListImages(ctx context.Context) ([]DockerImageInfo, error)

	// ListVolumes returns the Docker volumes visible to the daemon.
	ListVolumes(ctx context.Context) ([]DockerVolumeInfo, error)

	// DeleteVolume removes the named Docker volume.
	DeleteVolume(ctx context.Context, name string) error
}

// ── Response types ────────────────────────────────────────────────────────────

// DockerStatusResponse describes the runtime state of an agent container.
// No secrets or host paths appear in this struct.
type DockerStatusResponse struct {
	AgentID       string `json:"agent_id"`
	ContainerName string `json:"container_name"`
	Image         string `json:"image"`
	Running       bool   `json:"running"`
	ContainerID   string `json:"container_id,omitempty"` // short id, never a secret
	VolumeName    string `json:"volume_name"`
	WorkspacePath string `json:"workspace_path"` // container-side path only
	Status        string `json:"status"`         // "running", "exited", "not_found", …
}

// DockerImageInfo describes a locally available Docker image.
type DockerImageInfo struct {
	ID         string    `json:"id"` // short digest prefix
	Repository string    `json:"repository"`
	Tag        string    `json:"tag"`
	Size       string    `json:"size"`
	CreatedAt  time.Time `json:"created_at"`
}

// DockerVolumeInfo describes a Docker volume.
type DockerVolumeInfo struct {
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	Mountpoint string `json:"mountpoint,omitempty"` // host path — included here because operators need it; no secrets present
	CreatedAt  string `json:"created_at,omitempty"`
}

// ── SandboxAdminHandler ───────────────────────────────────────────────────────

// SandboxAdminHandler wires the DockerAdminService to HTTP endpoints.
type SandboxAdminHandler struct {
	svc DockerAdminService
}

// NewSandboxAdminHandler creates a handler backed by the given service.
func NewSandboxAdminHandler(svc DockerAdminService) *SandboxAdminHandler {
	return &SandboxAdminHandler{svc: svc}
}

// Register mounts all sandbox admin routes on mux.
// The auth middleware wrapping the mux is provided by the server — no
// auth logic is needed here.
func (h *SandboxAdminHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/admin/sandbox/docker/status", h.handleStatus)
	mux.HandleFunc("/api/admin/sandbox/docker/create", h.handleCreate)
	mux.HandleFunc("/api/admin/sandbox/docker/stop", h.handleStop)
	mux.HandleFunc("/api/admin/sandbox/docker/reset", h.handleReset)
	mux.HandleFunc("/api/admin/sandbox/docker/pull", h.handlePull)
	mux.HandleFunc("/api/admin/sandbox/docker/images", h.handleImages)
	mux.HandleFunc("/api/admin/sandbox/docker/volumes", h.handleVolumes)
	mux.HandleFunc("/api/admin/sandbox/docker/volume", h.handleVolume)
}

// ── Individual handlers ───────────────────────────────────────────────────────

// GET /api/admin/sandbox/docker/status?agent_id=<id>
func (h *SandboxAdminHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeSandboxErrorJSON(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed")
		return
	}
	agentID := normalizeAgentIDParam(r.URL.Query().Get("agent_id"))

	status, err := h.svc.Status(r.Context(), agentID)
	if err != nil {
		writeSandboxErrorJSON(w, http.StatusInternalServerError, "sandbox.status_failed", err.Error())
		return
	}
	writeSandboxJSON(w, http.StatusOK, status)
}

// POST /api/admin/sandbox/docker/create  body: {"agent_id":"..."}
func (h *SandboxAdminHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeSandboxErrorJSON(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed")
		return
	}
	var req struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeSandboxErrorJSON(w, http.StatusBadRequest, "request.invalid_json", "invalid json body")
		return
	}
	agentID := normalizeAgentIDParam(req.AgentID)

	if err := h.svc.CreateContainer(r.Context(), agentID); err != nil {
		writeSandboxErrorJSON(w, http.StatusInternalServerError, "sandbox.create_failed", err.Error())
		return
	}
	writeSandboxJSON(w, http.StatusCreated, map[string]any{"ok": true, "agent_id": agentID, "action": "create"})
}

// POST /api/admin/sandbox/docker/stop  body: {"agent_id":"..."}
func (h *SandboxAdminHandler) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeSandboxErrorJSON(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed")
		return
	}
	var req struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeSandboxErrorJSON(w, http.StatusBadRequest, "request.invalid_json", "invalid json body")
		return
	}
	agentID := normalizeAgentIDParam(req.AgentID)

	if err := h.svc.StopContainer(r.Context(), agentID); err != nil {
		writeSandboxErrorJSON(w, http.StatusInternalServerError, "sandbox.stop_failed", err.Error())
		return
	}
	writeSandboxJSON(w, http.StatusOK, map[string]any{"ok": true, "agent_id": agentID, "action": "stop"})
}

// POST /api/admin/sandbox/docker/reset  body: {"agent_id":"..."}
func (h *SandboxAdminHandler) handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeSandboxErrorJSON(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed")
		return
	}
	var req struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeSandboxErrorJSON(w, http.StatusBadRequest, "request.invalid_json", "invalid json body")
		return
	}
	agentID := normalizeAgentIDParam(req.AgentID)

	if err := h.svc.ResetContainer(r.Context(), agentID); err != nil {
		writeSandboxErrorJSON(w, http.StatusInternalServerError, "sandbox.reset_failed", err.Error())
		return
	}
	writeSandboxJSON(w, http.StatusOK, map[string]any{"ok": true, "agent_id": agentID, "action": "reset"})
}

// POST /api/admin/sandbox/docker/pull  body: {"image":"ubuntu:24.04"}
func (h *SandboxAdminHandler) handlePull(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeSandboxErrorJSON(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed")
		return
	}
	var req struct {
		Image string `json:"image"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeSandboxErrorJSON(w, http.StatusBadRequest, "request.invalid_json", "invalid json body")
		return
	}
	image := strings.TrimSpace(req.Image)
	if image == "" {
		writeSandboxErrorJSON(w, http.StatusBadRequest, "request.invalid_input", "image is required")
		return
	}

	if err := h.svc.PullImage(r.Context(), image); err != nil {
		writeSandboxErrorJSON(w, http.StatusInternalServerError, "sandbox.pull_failed", err.Error())
		return
	}
	writeSandboxJSON(w, http.StatusOK, map[string]any{"ok": true, "image": image, "action": "pull"})
}

// GET /api/admin/sandbox/docker/images
func (h *SandboxAdminHandler) handleImages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeSandboxErrorJSON(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed")
		return
	}
	images, err := h.svc.ListImages(r.Context())
	if err != nil {
		writeSandboxErrorJSON(w, http.StatusInternalServerError, "sandbox.images_failed", err.Error())
		return
	}
	if images == nil {
		images = []DockerImageInfo{}
	}
	writeSandboxJSON(w, http.StatusOK, map[string]any{"images": images, "count": len(images)})
}

// GET /api/admin/sandbox/docker/volumes
func (h *SandboxAdminHandler) handleVolumes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeSandboxErrorJSON(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed")
		return
	}
	volumes, err := h.svc.ListVolumes(r.Context())
	if err != nil {
		writeSandboxErrorJSON(w, http.StatusInternalServerError, "sandbox.volumes_failed", err.Error())
		return
	}
	if volumes == nil {
		volumes = []DockerVolumeInfo{}
	}
	writeSandboxJSON(w, http.StatusOK, map[string]any{"volumes": volumes, "count": len(volumes)})
}

// DELETE /api/admin/sandbox/docker/volume  body: {"name":"volume-name"}
func (h *SandboxAdminHandler) handleVolume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeSandboxErrorJSON(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeSandboxErrorJSON(w, http.StatusBadRequest, "request.invalid_json", "invalid json body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeSandboxErrorJSON(w, http.StatusBadRequest, "request.invalid_input", "name is required")
		return
	}

	if err := h.svc.DeleteVolume(r.Context(), name); err != nil {
		writeSandboxErrorJSON(w, http.StatusInternalServerError, "sandbox.volume_delete_failed", err.Error())
		return
	}
	writeSandboxJSON(w, http.StatusOK, map[string]any{"ok": true, "name": name, "action": "delete_volume"})
}

// ── Helper writers ────────────────────────────────────────────────────────────

// writeSandboxJSON writes a JSON response with the given status code.
func writeSandboxJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeSandboxErrorJSON writes a JSON error response matching the standard
// error envelope: {"error":{"code":"...","message":"..."}}.
func writeSandboxErrorJSON(w http.ResponseWriter, status int, code, message string) {
	writeErrorJSON(w, status, code, message, 0)
}

// normalizeAgentIDParam trims and defaults an agent_id query/body parameter.
func normalizeAgentIDParam(raw string) string {
	id := strings.TrimSpace(raw)
	if id == "" {
		return "default"
	}
	return id
}

// ── DockerAdminManager (production implementation) ────────────────────────────

// DockerAdminManager implements DockerAdminService by shelling out to the
// docker CLI — consistent with how DockerProvider itself works in docker.go.
//
// Security note: no secrets, API keys, or host credentials are ever returned
// through this manager's methods or the API responses it populates.
type DockerAdminManager struct{}

// NewDockerAdminManager creates a production DockerAdminManager.
func NewDockerAdminManager() *DockerAdminManager {
	return &DockerAdminManager{}
}

// containerNameForAgent returns the deterministic container name for an agent.
// Must match the naming convention in DockerProvider.NewDockerProvider.
func containerNameForAgent(agentID string) string {
	return "openclawssy_agent_" + sanitizeDockerName(agentID)
}

// volumeNameForAgent returns the deterministic volume name for an agent.
func volumeNameForAgent(agentID string) string {
	return "openclawssy_ws_" + sanitizeDockerName(agentID)
}

// sanitizeDockerName replicates the logic from docker.go without importing the
// sandbox package (to keep channel/http package self-contained).
func sanitizeDockerName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32) // to lower
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	result := b.String()
	if result == "" {
		result = "default"
	}
	return result
}

// Status returns the runtime status of the container for agentID.
func (m *DockerAdminManager) Status(ctx context.Context, agentID string) (DockerStatusResponse, error) {
	containerName := containerNameForAgent(agentID)
	volumeName := volumeNameForAgent(agentID)

	// docker inspect --format '{{.State.Status}}\t{{.Id}}\t{{.Config.Image}}' <name>
	out, err := exec.CommandContext(ctx, "docker", "inspect",
		"--format", "{{.State.Status}}\t{{.Id}}\t{{.Config.Image}}",
		containerName).CombinedOutput()

	resp := DockerStatusResponse{
		AgentID:       agentID,
		ContainerName: containerName,
		VolumeName:    volumeName,
		WorkspacePath: "/workspace",
	}

	if err != nil {
		// Container does not exist or docker is not available.
		resp.Status = "not_found"
		resp.Running = false
		return resp, nil
	}

	parts := strings.SplitN(strings.TrimSpace(string(out)), "\t", 3)
	if len(parts) >= 1 {
		resp.Status = parts[0]
	}
	if len(parts) >= 2 {
		id := strings.TrimSpace(parts[1])
		if len(id) > 12 {
			id = id[:12] // short ID only — no security value in leaking the full hash
		}
		resp.ContainerID = id
	}
	if len(parts) >= 3 {
		resp.Image = parts[2]
	}
	resp.Running = resp.Status == "running"
	return resp, nil
}

// CreateContainer creates (or starts) the container for agentID.
// It mirrors the docker create + start flow in DockerProvider.Start.
func (m *DockerAdminManager) CreateContainer(ctx context.Context, agentID string) error {
	containerName := containerNameForAgent(agentID)
	volumeName := volumeNameForAgent(agentID)

	// Ensure volume exists.
	if out, err := exec.CommandContext(ctx, "docker", "volume", "inspect", volumeName).CombinedOutput(); err != nil || len(out) < 2 {
		if out2, err2 := exec.CommandContext(ctx, "docker", "volume", "create", volumeName).CombinedOutput(); err2 != nil {
			return fmt.Errorf("sandbox: docker volume create %s: %s: %w", volumeName, strings.TrimSpace(string(out2)), err2)
		}
	}

	// Check if container already exists.
	existsOut, existsErr := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.Id}}", containerName).CombinedOutput()
	containerID := strings.TrimSpace(string(existsOut))

	if existsErr != nil || containerID == "" {
		// Create the container.
		args := []string{
			"create",
			"--name", containerName,
			"--label", "openclawssy=true",
			"--label", "agent_id=" + agentID,
			"--volume", volumeName + ":/workspace",
			"--workdir", "/workspace",
			"--network", "none",
			"--restart", "no",
			"ubuntu:24.04",
			"sleep", "infinity",
		}
		createOut, createErr := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if createErr != nil {
			return fmt.Errorf("sandbox: docker create failed: %s: %w", strings.TrimSpace(string(createOut)), createErr)
		}
		containerID = strings.TrimSpace(string(createOut))
	}

	// Start the container if not already running.
	runningOut, _ := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Running}}", containerID).CombinedOutput()
	if strings.TrimSpace(string(runningOut)) != "true" {
		startOut, startErr := exec.CommandContext(ctx, "docker", "start", containerID).CombinedOutput()
		if startErr != nil {
			return fmt.Errorf("sandbox: docker start failed: %s: %w", strings.TrimSpace(string(startOut)), startErr)
		}
	}

	// Ensure /workspace exists with open permissions.
	_ = exec.CommandContext(ctx, "docker", "exec", "--user", "root", containerName, "mkdir", "-p", "/workspace").Run()
	_ = exec.CommandContext(ctx, "docker", "exec", "--user", "root", containerName, "chmod", "777", "/workspace").Run()

	return nil
}

// StopContainer stops the container for agentID (container is not removed).
func (m *DockerAdminManager) StopContainer(ctx context.Context, agentID string) error {
	containerName := containerNameForAgent(agentID)
	out, err := exec.CommandContext(ctx, "docker", "stop", "-t", "5", containerName).CombinedOutput()
	if err != nil {
		// Not running / not found is acceptable — we still return the error for
		// explicit stop calls so the operator knows.
		return fmt.Errorf("sandbox: docker stop %s: %s: %w", containerName, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// ResetContainer stops and removes the container for agentID.
// The volume is preserved so workspace data survives the reset.
func (m *DockerAdminManager) ResetContainer(ctx context.Context, agentID string) error {
	containerName := containerNameForAgent(agentID)
	// Ignore stop errors (container may already be stopped).
	_ = exec.CommandContext(ctx, "docker", "stop", "-t", "5", containerName).Run()

	out, err := exec.CommandContext(ctx, "docker", "rm", "-f", containerName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("sandbox: docker rm %s: %s: %w", containerName, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// PullImage pulls the given image reference from the registry.
func (m *DockerAdminManager) PullImage(ctx context.Context, image string) error {
	out, err := exec.CommandContext(ctx, "docker", "pull", image).CombinedOutput()
	if err != nil {
		return fmt.Errorf("sandbox: docker pull %s: %s: %w", image, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// ListImages returns the locally cached Docker images.
// Format: "{{.ID}}\t{{.Repository}}\t{{.Tag}}\t{{.Size}}\t{{.CreatedAt}}"
func (m *DockerAdminManager) ListImages(ctx context.Context) ([]DockerImageInfo, error) {
	out, err := exec.CommandContext(ctx, "docker", "images",
		"--format", "{{.ID}}\t{{.Repository}}\t{{.Tag}}\t{{.Size}}\t{{.CreatedAt}}").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("sandbox: docker images: %s: %w", strings.TrimSpace(string(out)), err)
	}

	var images []DockerImageInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 5)
		info := DockerImageInfo{}
		if len(parts) >= 1 {
			info.ID = parts[0]
		}
		if len(parts) >= 2 {
			info.Repository = parts[1]
		}
		if len(parts) >= 3 {
			info.Tag = parts[2]
		}
		if len(parts) >= 4 {
			info.Size = parts[3]
		}
		if len(parts) >= 5 {
			// Docker's default time format: "2024-01-15 10:30:00 +0000 UTC"
			// Try a few common formats and fall back to zero time gracefully.
			raw := strings.TrimSpace(parts[4])
			for _, layout := range []string{
				"2006-01-02 15:04:05 -0700 MST",
				"2006-01-02 15:04:05 +0000 UTC",
				time.RFC3339,
			} {
				if t, parseErr := time.Parse(layout, raw); parseErr == nil {
					info.CreatedAt = t
					break
				}
			}
		}
		images = append(images, info)
	}
	return images, nil
}

// ListVolumes returns all Docker volumes visible to the daemon.
// Format: "{{.Name}}\t{{.Driver}}\t{{.Mountpoint}}"
// Note: {{.CreatedAt}} is not available in all Docker CLI versions (< 27),
// so we omit it from the format string and fall back gracefully.
func (m *DockerAdminManager) ListVolumes(ctx context.Context) ([]DockerVolumeInfo, error) {
	out, err := exec.CommandContext(ctx, "docker", "volume", "ls",
		"--format", "{{.Name}}\t{{.Driver}}\t{{.Mountpoint}}").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("sandbox: docker volume ls: %s: %w", strings.TrimSpace(string(out)), err)
	}

	var volumes []DockerVolumeInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		vol := DockerVolumeInfo{}
		if len(parts) >= 1 {
			vol.Name = parts[0]
		}
		if len(parts) >= 2 {
			vol.Driver = parts[1]
		}
		if len(parts) >= 3 {
			vol.Mountpoint = parts[2]
		}
		volumes = append(volumes, vol)
	}
	return volumes, nil
}

// DeleteVolume removes the named Docker volume.
func (m *DockerAdminManager) DeleteVolume(ctx context.Context, name string) error {
	// Safety guard: reject obviously malicious names to prevent injection via
	// shell expansion.  The name is passed as a discrete argument to exec, so
	// actual shell injection is not possible, but we still sanitize for clarity.
	if strings.ContainsAny(name, " \t\n;|&`$") {
		return fmt.Errorf("sandbox: invalid volume name: %q", name)
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", "volume", "rm", name)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sandbox: docker volume rm %s: %s: %w", name, strings.TrimSpace(stderr.String()), err)
	}
	return nil
}
