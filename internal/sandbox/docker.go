/*
DockerProvider threat model:

MITIGATED:
  - Path traversal: validateContainerPath() enforces /workspace prefix on all
    file operations before any data reaches the container. dockerResolvePath()
    in engine.go provides a second enforcement layer at the tool/policy level.
  - Host filesystem access: docker cp uses host-only temp files in os.TempDir();
    the container only sees the /workspace volume — no host paths are mounted.
  - Container escape via path: dockerResolvePath() in engine.go enforces
    /workspace on the tool call layer; validateContainerPath() re-enforces at
    the Provider method layer so that a rogue caller cannot bypass either guard.
  - Secret leakage: secrets are NEVER passed to the container environment, only
    to the model API call layer (HTTP headers / request body). extraEnv contains
    only non-secret configuration values from the operator config file.
  - Privilege escalation: administrative ops (mkdir/chmod) run as root, but only
    inside the container; file I/O exec runs as the image default user.
  - Network abuse: network=none by default; configurable but disabled by default.
  - Null-byte injection: null bytes are stripped before any path comparison.
  - Overly long paths: rejected before they reach container commands.

ACCEPTED RISKS / OUT OF SCOPE:
  - Docker socket itself is trusted (host-level access required to run containers).
  - Container image supply chain (image must be trusted by the operator).
  - Resource exhaustion: CPU/memory limits are configurable; enforcement is at
    the OS/Docker level.
  - Shared kernel: Docker is process isolation, not VM isolation (no seccomp
    profile is applied beyond Docker defaults here; operators should add one).
*/

package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"openclawssy/internal/config"
)

// DockerProvider implements Provider by executing all operations inside a
// persistent Docker container backed by a named Docker volume.
//
// The container is created once (in Start) and persists across Stop/Start
// cycles so that the volume contents survive agent reruns.  The container is
// NOT removed on Stop — call Reset to tear it down explicitly.
//
// All file-system methods route I/O through the container, so no workspace
// data ever touches the host filesystem.
type DockerProvider struct {
	agentID       string
	containerID   string // actual full ID once created
	image         string
	volumeName    string
	containerName string
	networkMode   string  // "none" by default
	cpuLimit      float64 // 0 = no limit
	memoryMB      int     // 0 = no limit
	extraEnv      []string
	pullPolicy    string // "always", "if-not-present", "never"

	mu      sync.RWMutex
	started bool
	runCtx  context.Context //nolint:containedctx
	cancel  context.CancelFunc
}

// NewDockerProvider creates a DockerProvider for the given agentID.
// The agentID is used to derive deterministic container and volume names.
func NewDockerProvider(agentID string, cfg config.DockerSandboxConfig) (*DockerProvider, error) {
	if agentID == "" {
		return nil, errors.New("sandbox: agentID required for docker provider")
	}
	sanitized := sanitizeDockerName(agentID)

	image := cfg.Image
	if image == "" {
		image = "ubuntu:24.04"
	}

	pullPolicy := cfg.PullPolicy
	if pullPolicy == "" {
		pullPolicy = "if-not-present"
	}

	networkMode := "none"
	if cfg.NetworkEnabled {
		networkMode = "bridge"
	}

	// Defense-in-depth: warn if Docker socket is pointed somewhere unusual.
	warnDockerSocketExposure()

	return &DockerProvider{
		agentID:       agentID,
		image:         image,
		volumeName:    "openclawssy_ws_" + sanitized,
		containerName: "openclawssy_agent_" + sanitized,
		networkMode:   networkMode,
		cpuLimit:      cfg.CPULimit,
		memoryMB:      cfg.MemoryLimitMB,
		// extraEnv are non-secret environment variables from config.
		// Secrets MUST NOT be placed here — they are managed by the secrets store
		// and injected only at the API call layer, never into the container.
		extraEnv:   cfg.ExtraEnv,
		pullPolicy: pullPolicy,
	}, nil
}

// warnDockerSocketExposure logs a warning if DOCKER_HOST is set to a
// non-standard value.  This is a defense-in-depth signal only — it does not
// prevent operation, but alerts operators to unexpected configurations.
func warnDockerSocketExposure() {
	if host := os.Getenv("DOCKER_HOST"); host != "" && host != "unix:///var/run/docker.sock" {
		log.Printf("warning: DOCKER_HOST is set to %q — ensure this is intentional", host)
	}
}

// validateContainerPath ensures a path passed to container file operations is
// absolute, within /workspace, and contains no traversal sequences or null
// bytes.  All DockerProvider file-operation methods call this before executing.
func validateContainerPath(path string) error {
	// Strip null bytes first — they can truncate C-string comparisons.
	path = strings.ReplaceAll(path, "\x00", "")
	path = strings.TrimSpace(path)

	if path == "" {
		return errors.New("sandbox: docker: empty container path")
	}
	if len(path) > 4096 {
		return errors.New("sandbox: docker: path too long")
	}
	// Path must be absolute so callers cannot smuggle in relative traversal.
	if !filepath.IsAbs(path) {
		return fmt.Errorf("sandbox: docker: container path must be absolute: %s", path)
	}
	// Clean and re-check — filepath.Clean resolves ".." components so that
	// "/workspace/../../etc" becomes "/etc", which is caught here.
	clean := filepath.Clean(path)
	if clean != "/workspace" && !strings.HasPrefix(clean, "/workspace/") {
		return fmt.Errorf("sandbox: docker: container path outside /workspace: %s", path)
	}
	return nil
}

// sanitizeDockerName converts an agentID to a string safe for use in Docker
// container/volume names (lowercase alphanumeric and hyphens only).
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

// Start pulls the image if needed, creates/ensures the volume and container,
// and starts the container if it is not already running.
func (p *DockerProvider) Start(runCtx context.Context) error {
	if runCtx == nil {
		runCtx = context.Background()
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	p.runCtx, p.cancel = context.WithCancel(runCtx)

	// 1. Pull image if needed.
	if err := p.pullImage(); err != nil {
		return fmt.Errorf("sandbox: docker pull image: %w", err)
	}

	// 2. Ensure the named volume exists.
	if err := p.ensureVolume(); err != nil {
		return fmt.Errorf("sandbox: docker ensure volume: %w", err)
	}

	// 3. Create or reuse the container.
	containerID, err := p.ensureContainer()
	if err != nil {
		return fmt.Errorf("sandbox: docker ensure container: %w", err)
	}
	p.containerID = containerID

	// 4. Start the container if it is not already running.
	if err := p.startContainerIfNeeded(containerID); err != nil {
		return fmt.Errorf("sandbox: docker start container: %w", err)
	}

	// 5. Make sure /workspace exists with open permissions.
	// Use root for mkdir/chmod because the container runs "sleep infinity" as
	// whatever the image default user is; we override the exec user explicitly.
	if err := p.runAsRoot(p.runCtx, "mkdir", "-p", "/workspace"); err != nil {
		return fmt.Errorf("sandbox: docker mkdir workspace: %w", err)
	}
	if err := p.runAsRoot(p.runCtx, "chmod", "777", "/workspace"); err != nil {
		return fmt.Errorf("sandbox: docker chmod workspace: %w", err)
	}

	p.started = true
	return nil
}

// Stop cancels the context for this run but does NOT stop or remove the container.
// The container and volume persist for reuse across runs.  Call Reset to destroy.
func (p *DockerProvider) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancel != nil {
		p.cancel()
	}
	p.runCtx = nil
	p.cancel = nil
	p.started = false
	return nil
}

// Reset stops and removes the container but keeps the volume.
// The next Start() call will recreate the container fresh.
func (p *DockerProvider) Reset(ctx context.Context) error {
	p.mu.Lock()
	containerName := p.containerName
	p.mu.Unlock()

	// Stop (ignore errors if not running).
	_ = exec.CommandContext(ctx, "docker", "stop", "-t", "5", containerName).Run()

	// Remove.
	out, err := exec.CommandContext(ctx, "docker", "rm", "-f", containerName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("sandbox: docker rm failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// ContainerStatus returns "running", "exited", or "not_found".
func (p *DockerProvider) ContainerStatus(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, "docker", "inspect",
		"--format", "{{.State.Status}}", p.containerName).CombinedOutput()
	if err != nil {
		return "not_found"
	}
	return strings.TrimSpace(string(out))
}

// ContainerName returns the container name used by this provider.
func (p *DockerProvider) ContainerName() string { return p.containerName }

// VolumeName returns the Docker volume name used by this provider.
func (p *DockerProvider) VolumeName() string { return p.volumeName }

// ImageName returns the container image name.
func (p *DockerProvider) ImageName() string { return p.image }

// providerState implementation.
func (p *DockerProvider) providerName() string { return "docker" }

func (p *DockerProvider) isStarted() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.started
}

// ---- Exec -------------------------------------------------------------------

// Exec runs a command inside the container and returns its output.
func (p *DockerProvider) Exec(cmd Command) (Result, error) {
	p.mu.RLock()
	started := p.started
	containerName := p.containerName
	runCtx := p.runCtx
	p.mu.RUnlock()

	if !started {
		return Result{}, ErrNotStarted
	}
	if cmd.Name == "" {
		return Result{}, errors.New("sandbox: command name is required")
	}

	args := append([]string{"exec", containerName, cmd.Name}, cmd.Args...)
	proc := exec.CommandContext(runCtx, "docker", args...)

	var stdout, stderr bytes.Buffer
	proc.Stdout = &stdout
	proc.Stderr = &stderr

	err := proc.Run()
	result := Result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: 0}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, err
	}
	result.ExitCode = -1
	return result, err
}

// ---- File operations --------------------------------------------------------

// ReadFile copies the file from the container to a temp file on the host,
// reads it, then deletes the temp file.
// The temp file path is never returned to callers — only file content is.
func (p *DockerProvider) ReadFile(ctx context.Context, path string) ([]byte, error) {
	if err := validateContainerPath(path); err != nil {
		return nil, err
	}
	p.mu.RLock()
	started := p.started
	containerName := p.containerName
	p.mu.RUnlock()
	if !started {
		return nil, ErrNotStarted
	}

	tmp, err := os.CreateTemp("", "openclawssy_docker_read_*")
	if err != nil {
		return nil, fmt.Errorf("sandbox: ReadFile tmp: %w", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()
	// Always remove the temp file, even on error — the path must never persist.
	defer os.Remove(tmpPath)

	out, err := exec.CommandContext(ctx, "docker", "cp",
		containerName+":"+path, tmpPath).CombinedOutput()
	if err != nil {
		// Do not include tmpPath in the error — only report the container-side path.
		return nil, fmt.Errorf("sandbox: docker cp from %s: %s: %w",
			path, strings.TrimSpace(string(out)), err)
	}

	return os.ReadFile(tmpPath)
}

// WriteFile writes data to a temp file on the host, then docker-cp's it into
// the container at the given path with the requested permissions.
// The temp file is created in os.TempDir() and always removed via defer.
func (p *DockerProvider) WriteFile(ctx context.Context, path string, data []byte, perm os.FileMode) error {
	if err := validateContainerPath(path); err != nil {
		return err
	}
	p.mu.RLock()
	started := p.started
	containerName := p.containerName
	p.mu.RUnlock()
	if !started {
		return ErrNotStarted
	}

	// Write to a temp file on the host (in os.TempDir(), never in workspace).
	tmp, err := os.CreateTemp("", "openclawssy_docker_write_*")
	if err != nil {
		return fmt.Errorf("sandbox: WriteFile tmp: %w", err)
	}
	// Always remove — ensures host temp files are not left behind on any error.
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("sandbox: WriteFile tmp write: %w", err)
	}
	tmp.Close()

	// Ensure parent directory exists inside the container.
	if dir := filepath.Dir(path); dir != "" && dir != "." && dir != "/" {
		_ = p.runAsRoot(ctx, "mkdir", "-p", dir)
	}

	// docker cp host-file container:container-path
	// The host temp path is not included in any error message returned to callers.
	out, err := exec.CommandContext(ctx, "docker", "cp",
		tmp.Name(), containerName+":"+path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("sandbox: docker cp to %s: %s: %w",
			path, strings.TrimSpace(string(out)), err)
	}

	// Apply requested permissions.
	if perm != 0 {
		_ = p.runAsRoot(ctx, "chmod", fmt.Sprintf("%o", perm), path)
	}

	return nil
}

// ListDir returns the entries (non-recursive) of a directory inside the container.
func (p *DockerProvider) ListDir(ctx context.Context, path string) ([]FileInfo, error) {
	if err := validateContainerPath(path); err != nil {
		return nil, err
	}
	p.mu.RLock()
	started := p.started
	containerName := p.containerName
	p.mu.RUnlock()
	if !started {
		return nil, ErrNotStarted
	}

	// Use `find -maxdepth 1 -mindepth 1 -printf` to get type, size, and name.
	// %y = file type char (d=dir, f=file, l=symlink, …), %s = size, %f = filename
	cmd := fmt.Sprintf(
		"find %s -maxdepth 1 -mindepth 1 -printf '%%y\\t%%s\\t%%f\\n' 2>/dev/null || true",
		shellescape(path),
	)
	out, err := exec.CommandContext(ctx, "docker", "exec", containerName,
		"sh", "-c", cmd).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("sandbox: docker ListDir %s: %s: %w",
			path, strings.TrimSpace(string(out)), err)
	}

	var entries []FileInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		typeChar := parts[0]
		sizeStr := parts[1]
		name := parts[2]
		if name == "" {
			continue
		}
		isDir := typeChar == "d"
		size, _ := strconv.ParseInt(sizeStr, 10, 64)
		entries = append(entries, FileInfo{Name: name, IsDir: isDir, Size: size})
	}
	return entries, nil
}

// MkdirAll creates the directory tree inside the container.
func (p *DockerProvider) MkdirAll(ctx context.Context, path string, _ os.FileMode) error {
	if err := validateContainerPath(path); err != nil {
		return err
	}
	p.mu.RLock()
	started := p.started
	p.mu.RUnlock()
	if !started {
		return ErrNotStarted
	}
	return p.runAsRoot(ctx, "mkdir", "-p", path)
}

// Remove removes a file or directory inside the container.
func (p *DockerProvider) Remove(ctx context.Context, path string, recursive bool) error {
	if err := validateContainerPath(path); err != nil {
		return err
	}
	p.mu.RLock()
	started := p.started
	p.mu.RUnlock()
	if !started {
		return ErrNotStarted
	}
	if recursive {
		return p.runInContainer(ctx, "rm", "-rf", path)
	}
	return p.runInContainer(ctx, "rm", "-f", path)
}

// Rename moves src to dst inside the container.
func (p *DockerProvider) Rename(ctx context.Context, src, dst string) error {
	if err := validateContainerPath(src); err != nil {
		return err
	}
	if err := validateContainerPath(dst); err != nil {
		return err
	}
	p.mu.RLock()
	started := p.started
	p.mu.RUnlock()
	if !started {
		return ErrNotStarted
	}
	return p.runInContainer(ctx, "mv", src, dst)
}

// Lstat returns file info for path inside the container.
// Returns (FileInfo{}, false, nil) when the path does not exist.
func (p *DockerProvider) Lstat(ctx context.Context, path string) (FileInfo, bool, error) {
	if err := validateContainerPath(path); err != nil {
		return FileInfo{}, false, err
	}
	p.mu.RLock()
	started := p.started
	containerName := p.containerName
	p.mu.RUnlock()
	if !started {
		return FileInfo{}, false, ErrNotStarted
	}

	// Use stat --printf so that \t is interpreted as a real tab character.
	// If the path does not exist, the command exits non-zero and we print NOTEXIST.
	cmd := fmt.Sprintf(
		"stat %s --printf '%%F\\t%%s\\t%%n\\n' 2>/dev/null || echo NOTEXIST",
		shellescape(path),
	)
	out, err := exec.CommandContext(ctx, "docker", "exec", containerName,
		"sh", "-c", cmd).CombinedOutput()
	if err != nil {
		return FileInfo{}, false, fmt.Errorf("sandbox: docker Lstat %s: %w", path, err)
	}

	result := strings.TrimSpace(string(out))
	if result == "NOTEXIST" || result == "" {
		return FileInfo{}, false, nil
	}

	// Split on the actual tab character that stat --printf produces.
	parts := strings.SplitN(result, "\t", 3)
	if len(parts) < 3 {
		return FileInfo{}, false, fmt.Errorf("sandbox: docker Lstat unexpected output: %q", result)
	}

	typeStr := parts[0]
	sizeStr := parts[1]
	name := filepath.Base(path)
	isDir := strings.Contains(typeStr, "directory")
	size, _ := strconv.ParseInt(sizeStr, 10, 64)

	return FileInfo{Name: name, IsDir: isDir, Size: size}, true, nil
}

// EvalSymlinks resolves symlinks inside the container.
func (p *DockerProvider) EvalSymlinks(ctx context.Context, path string) (string, error) {
	if err := validateContainerPath(path); err != nil {
		return "", err
	}
	p.mu.RLock()
	started := p.started
	containerName := p.containerName
	p.mu.RUnlock()
	if !started {
		return "", ErrNotStarted
	}

	cmd := fmt.Sprintf("readlink -f %s 2>/dev/null || echo NOTEXIST", shellescape(path))
	out, err := exec.CommandContext(ctx, "docker", "exec", containerName,
		"sh", "-c", cmd).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("sandbox: docker EvalSymlinks %s: %w", path, err)
	}

	result := strings.TrimSpace(string(out))
	if result == "NOTEXIST" || result == "" {
		return "", fmt.Errorf("sandbox: path does not exist: %s", path)
	}
	return result, nil
}

// ---- Internal helpers -------------------------------------------------------

// pullImage pulls the container image according to the pullPolicy.
func (p *DockerProvider) pullImage() error {
	switch p.pullPolicy {
	case "never":
		return nil
	case "if-not-present":
		// Check if image is already present locally.
		out, err := exec.Command("docker", "image", "inspect", p.image).CombinedOutput()
		if err == nil && len(out) > 2 {
			return nil // image exists
		}
		fallthrough
	case "always":
		out, err := exec.Command("docker", "pull", p.image).CombinedOutput()
		if err != nil {
			return fmt.Errorf("docker pull %s failed: %s: %w",
				p.image, strings.TrimSpace(string(out)), err)
		}
		return nil
	default:
		return nil
	}
}

// ensureVolume creates the Docker volume if it does not exist.
func (p *DockerProvider) ensureVolume() error {
	out, err := exec.Command("docker", "volume", "inspect", p.volumeName).CombinedOutput()
	if err == nil && len(out) > 2 {
		return nil // already exists
	}
	out, err = exec.Command("docker", "volume", "create", p.volumeName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker volume create failed: %s: %w",
			strings.TrimSpace(string(out)), err)
	}
	return nil
}

// ensureContainer creates the container if it does not already exist.
// Returns the container ID (full 64-char SHA or the container name).
func (p *DockerProvider) ensureContainer() (string, error) {
	// Check if container already exists.
	out, err := exec.Command("docker", "inspect", "--format", "{{.Id}}",
		p.containerName).CombinedOutput()
	if err == nil {
		id := strings.TrimSpace(string(out))
		if id != "" {
			return id, nil
		}
	}

	// Build docker create args.
	args := []string{
		"create",
		"--name", p.containerName,
		"--label", "openclawssy=true",
		"--label", "agent_id=" + p.agentID,
		"--volume", p.volumeName + ":/workspace",
		"--workdir", "/workspace",
		"--network", p.networkMode,
		"--restart", "no",
	}

	// Resource limits.
	if p.cpuLimit > 0 {
		args = append(args, "--cpus", fmt.Sprintf("%.2f", p.cpuLimit))
	}
	if p.memoryMB > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dm", p.memoryMB))
	}

	// Extra environment variables.
	for _, env := range p.extraEnv {
		args = append(args, "-e", env)
	}

	// Image + infinite-sleep keep-alive.
	args = append(args, p.image, "sleep", "infinity")

	out, err = exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker create failed: %s: %w",
			strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// startContainerIfNeeded starts the container if it is not already running.
func (p *DockerProvider) startContainerIfNeeded(containerID string) error {
	out, err := exec.Command("docker", "inspect", "--format",
		"{{.State.Running}}", containerID).CombinedOutput()
	if err == nil && strings.TrimSpace(string(out)) == "true" {
		return nil // already running
	}

	startOut, startErr := exec.Command("docker", "start", containerID).CombinedOutput()
	if startErr != nil {
		return fmt.Errorf("docker start failed: %s: %w",
			strings.TrimSpace(string(startOut)), startErr)
	}
	return nil
}

// runInContainer runs cmd with args inside the container as the default user.
func (p *DockerProvider) runInContainer(ctx context.Context, cmd string, args ...string) error {
	fullArgs := append([]string{"exec", p.containerName, cmd}, args...)
	out, err := exec.CommandContext(ctx, "docker", fullArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("sandbox: docker exec %s: %s: %w",
			cmd, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// runAsRoot runs cmd with args inside the container as root (uid 0).
// This is used for administrative operations like mkdir and chmod.
func (p *DockerProvider) runAsRoot(ctx context.Context, cmd string, args ...string) error {
	fullArgs := append([]string{"exec", "--user", "root", p.containerName, cmd}, args...)
	out, err := exec.CommandContext(ctx, "docker", fullArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("sandbox: docker exec (root) %s: %s: %w",
			cmd, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// shellescape wraps s in single quotes, escaping any embedded single quotes,
// so the result is safe to embed in a `sh -c '...'` string.
func shellescape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
