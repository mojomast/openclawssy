/*
DockerProvider threat model:

MITIGATED:
  - Path traversal: validateContainerPath() enforces /workspace prefix on all
    file operations before any data reaches the container. dockerResolvePath()
    in engine.go provides a second enforcement layer at the tool/policy level.
  - Host filesystem access: CopyTo/CopyFrom use in-memory tar streams; no
    workspace bind mount to host paths is required.
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
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/docker/docker/pkg/stdcopy"

	"openclawssy/internal/config"
)

// DockerProvider implements Provider by executing all operations inside a
// persistent Docker container backed by a named Docker volume.
//
// The container is created once (in Start) and persists across Stop/Start
// cycles so that the volume contents survive agent reruns. The container is
// NOT removed on Stop — call Reset to tear it down explicitly.
//
// All file-system methods route I/O through the container, so no workspace
// data ever touches the host filesystem.
type DockerProvider struct {
	agentID       string
	containerID   string // actual full ID once created
	image         string
	dockerHost    string
	volumeName    string
	containerName string
	networkMode   string  // "none" by default
	cpuLimit      float64 // 0 = no limit
	memoryMB      int     // 0 = no limit
	hardened      bool
	pidsLimit     int
	extraEnv      []string
	pullPolicy    string // "always", "if-not-present", "never"

	mu      sync.RWMutex
	started bool
	runCtx  context.Context //nolint:containedctx
	cancel  context.CancelFunc
	cli     *client.Client
}

// NewDockerProvider creates a DockerProvider for the given agentID.
// The agentID is used to derive deterministic container and volume names.
func NewDockerProvider(agentID string, cfg config.DockerSandboxConfig) (*DockerProvider, error) {
	if agentID == "" {
		return nil, errors.New("sandbox: agentID required for docker provider")
	}
	sanitized := sanitizeDockerName(agentID)

	imageName := cfg.Image
	if imageName == "" {
		imageName = "ubuntu:24.04"
	}
	if !imageAllowed(imageName, cfg.AllowedImages) {
		return nil, fmt.Errorf("sandbox: docker: image %q is not in sandbox.docker.allowed_images", imageName)
	}

	dockerHost := strings.TrimSpace(cfg.Host)
	if cfg.RequireDedicatedDaemon {
		if dockerHost == "" {
			return nil, errors.New("sandbox: docker: dedicated daemon required but sandbox.docker.host is empty")
		}
		if dockerHost == "unix:///var/run/docker.sock" {
			return nil, errors.New("sandbox: docker: dedicated daemon required but sandbox.docker.host points to default host socket")
		}
	}

	pullPolicy := cfg.PullPolicy
	if pullPolicy == "" {
		pullPolicy = "if-not-present"
	}

	networkMode := "none"
	if cfg.NetworkEnabled {
		networkMode = "bridge"
	}
	pidsLimit := cfg.PidsLimit
	if cfg.Hardened && pidsLimit <= 0 {
		pidsLimit = 256
	}

	// Defense-in-depth: warn if Docker socket is pointed somewhere unusual.
	warnDockerSocketExposure(dockerHost)

	return &DockerProvider{
		agentID:       agentID,
		image:         imageName,
		dockerHost:    dockerHost,
		volumeName:    "openclawssy_ws_" + sanitized,
		containerName: "openclawssy_agent_" + sanitized,
		networkMode:   networkMode,
		cpuLimit:      cfg.CPULimit,
		memoryMB:      cfg.MemoryLimitMB,
		hardened:      cfg.Hardened,
		pidsLimit:     pidsLimit,
		// extraEnv are non-secret environment variables from config.
		// Secrets MUST NOT be placed here — they are managed by the secrets store
		// and injected only at the API call layer, never into the container.
		extraEnv:   cfg.ExtraEnv,
		pullPolicy: pullPolicy,
	}, nil
}

// warnDockerSocketExposure logs a warning if DOCKER_HOST is set to a
// non-standard value. This is a defense-in-depth signal only — it does not
// prevent operation, but alerts operators to unexpected configurations.
func warnDockerSocketExposure(configuredHost string) {
	host := strings.TrimSpace(configuredHost)
	if host == "" {
		host = os.Getenv("DOCKER_HOST")
	}
	if host != "" && host != "unix:///var/run/docker.sock" {
		slog.Warn("DOCKER_HOST is set to a non-standard value — ensure this is intentional", "docker_host", host)
	}
}

func imageAllowed(image string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, item := range allowed {
		if strings.TrimSpace(item) == image {
			return true
		}
	}
	return false
}

func (p *DockerProvider) dockerClient() (*client.Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cli != nil {
		return p.cli, nil
	}
	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}
	if strings.TrimSpace(p.dockerHost) != "" {
		opts = append(opts, client.WithHost(strings.TrimSpace(p.dockerHost)))
	}
	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("sandbox: docker client init: %w", err)
	}
	p.cli = cli
	return p.cli, nil
}

// validateContainerPath ensures a path passed to container file operations is
// absolute, within /workspace, and contains no traversal sequences or null
// bytes. All DockerProvider file-operation methods call this before executing.
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
	p.runCtx, p.cancel = context.WithCancel(runCtx)
	ctx := p.runCtx
	p.mu.Unlock()

	cli, err := p.dockerClient()
	if err != nil {
		return err
	}

	if err := p.pullImage(cli, ctx); err != nil {
		return fmt.Errorf("sandbox: docker pull image: %w", err)
	}

	if err := p.ensureVolume(cli, ctx); err != nil {
		return fmt.Errorf("sandbox: docker ensure volume: %w", err)
	}

	containerID, err := p.ensureContainer(cli, ctx)
	if err != nil {
		return fmt.Errorf("sandbox: docker ensure container: %w", err)
	}

	if err := p.startContainerIfNeeded(cli, ctx, containerID); err != nil {
		return fmt.Errorf("sandbox: docker start container: %w", err)
	}

	if err := p.runAsRoot(ctx, "mkdir", "-p", "/workspace"); err != nil {
		return fmt.Errorf("sandbox: docker mkdir workspace: %w", err)
	}
	if err := p.runAsRoot(ctx, "chmod", "777", "/workspace"); err != nil {
		return fmt.Errorf("sandbox: docker chmod workspace: %w", err)
	}

	p.mu.Lock()
	p.containerID = containerID
	p.started = true
	p.mu.Unlock()

	return nil
}

// Stop cancels the context for this run but does NOT stop or remove the container.
// The container and volume persist for reuse across runs. Call Reset to destroy.
func (p *DockerProvider) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancel != nil {
		p.cancel()
	}
	p.runCtx = nil
	p.cancel = nil
	p.started = false
	if p.cli != nil {
		_ = p.cli.Close()
		p.cli = nil
	}
	return nil
}

// Reset stops and removes the container but keeps the volume.
// The next Start() call will recreate the container fresh.
func (p *DockerProvider) Reset(ctx context.Context) error {
	cli, err := p.dockerClient()
	if err != nil {
		return err
	}

	p.mu.RLock()
	containerName := p.containerName
	p.mu.RUnlock()

	timeout := 5
	_ = cli.ContainerStop(ctx, containerName, container.StopOptions{Timeout: &timeout})
	err = cli.ContainerRemove(ctx, containerName, container.RemoveOptions{Force: true})
	if err != nil && !errdefs.IsNotFound(err) {
		return fmt.Errorf("sandbox: docker rm failed: %w", err)
	}
	return nil
}

// ContainerStatus returns "running", "exited", or "not_found".
func (p *DockerProvider) ContainerStatus(ctx context.Context) string {
	cli, err := p.dockerClient()
	if err != nil {
		return "not_found"
	}
	inspected, err := cli.ContainerInspect(ctx, p.containerName)
	if err != nil {
		return "not_found"
	}
	return strings.TrimSpace(inspected.State.Status)
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

	stdout, stderr, exitCode, err := p.execInContainer(runCtx, containerName, "", cmd.WorkDir, append([]string{cmd.Name}, cmd.Args...))
	result := Result{Stdout: stdout, Stderr: stderr, ExitCode: exitCode}
	if err != nil {
		return result, err
	}
	return result, nil
}

// ---- File operations --------------------------------------------------------

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

	cli, err := p.dockerClient()
	if err != nil {
		return nil, err
	}

	stat, err := cli.ContainerStatPath(ctx, containerName, path)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, fmt.Errorf("sandbox: path does not exist: %s", path)
		}
		return nil, fmt.Errorf("sandbox: docker stat %s: %w", path, err)
	}
	if os.FileMode(stat.Mode).IsDir() {
		return nil, fmt.Errorf("sandbox: path is a directory: %s", path)
	}

	r, _, err := cli.CopyFromContainer(ctx, containerName, path)
	if err != nil {
		return nil, fmt.Errorf("sandbox: docker cp from %s: %w", path, err)
	}
	defer r.Close()

	tr := tar.NewReader(r)
	for {
		hdr, nextErr := tr.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return nil, fmt.Errorf("sandbox: read docker archive for %s: %w", path, nextErr)
		}
		if hdr == nil {
			continue
		}
		if hdr.FileInfo().IsDir() {
			continue
		}
		data, readErr := io.ReadAll(tr)
		if readErr != nil {
			return nil, fmt.Errorf("sandbox: read docker file data for %s: %w", path, readErr)
		}
		return data, nil
	}

	return nil, fmt.Errorf("sandbox: docker file not found in archive: %s", path)
}

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

	cli, err := p.dockerClient()
	if err != nil {
		return err
	}

	if dir := filepath.Dir(path); dir != "" && dir != "." && dir != "/" {
		if err := p.runAsRoot(ctx, "mkdir", "-p", dir); err != nil {
			return fmt.Errorf("sandbox: create parent dir for %s: %w", path, err)
		}
	}

	tf, err := tarSingleFile(filepath.Base(path), data, perm)
	if err != nil {
		return err
	}

	err = cli.CopyToContainer(ctx, containerName, filepath.Dir(path), tf, container.CopyToContainerOptions{AllowOverwriteDirWithFile: true})
	if err != nil {
		return fmt.Errorf("sandbox: docker cp to %s: %w", path, err)
	}

	if perm != 0 {
		if err := p.runAsRoot(ctx, "chmod", fmt.Sprintf("%o", perm), path); err != nil {
			return fmt.Errorf("sandbox: chmod %s: %w", path, err)
		}
	}

	return nil
}

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

	cli, err := p.dockerClient()
	if err != nil {
		return nil, err
	}

	// Verify the path exists and is a directory using the Docker API.
	stat, err := cli.ContainerStatPath(ctx, containerName, path)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, fmt.Errorf("sandbox: directory does not exist: %s", path)
		}
		return nil, fmt.Errorf("sandbox: docker stat %s: %w", path, err)
	}
	if !os.FileMode(stat.Mode).IsDir() {
		return nil, fmt.Errorf("sandbox: path is not a directory: %s", path)
	}

	// Use CopyFromContainer to read the directory listing via the Docker
	// daemon, matching the I/O path used by ReadFile and WriteFile.  This
	// avoids consistency issues between Docker Copy API writes and exec-
	// based reads that could return stale results.
	r, _, err := cli.CopyFromContainer(ctx, containerName, path)
	if err != nil {
		return nil, fmt.Errorf("sandbox: docker cp from %s: %w", path, err)
	}
	defer r.Close()

	// The Docker Copy API returns a tar archive of the directory.
	// Top-level entries in the archive whose parent matches the requested
	// directory name are immediate children.
	dirBase := filepath.Base(path) + "/"
	var entries []FileInfo
	tr := tar.NewReader(r)
	for {
		hdr, nextErr := tr.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return nil, fmt.Errorf("sandbox: read docker archive for %s: %w", path, nextErr)
		}
		if hdr == nil {
			continue
		}
		// The archive contains the directory itself as the first entry
		// (e.g. "ussydub/"), then immediate children as "ussydub/file.txt",
		// and nested entries as "ussydub/sub/deep.txt".  We only want
		// depth-1 children.
		name := filepath.ToSlash(hdr.Name)
		if name == dirBase || name == "." || name == "./" {
			continue
		}
		// Strip the directory prefix.
		if len(name) > len(dirBase) && name[:len(dirBase)] == dirBase {
			name = name[len(dirBase):]
		}
		// Skip nested entries (depth > 1).
		trimmed := strings.TrimSuffix(name, "/")
		if strings.Contains(trimmed, "/") {
			continue
		}
		if trimmed == "" {
			continue
		}
		entries = append(entries, FileInfo{
			Name:  trimmed,
			IsDir: hdr.Typeflag == tar.TypeDir,
			Size:  hdr.Size,
		})
	}
	return entries, nil
}

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

	cli, err := p.dockerClient()
	if err != nil {
		return FileInfo{}, false, err
	}

	stat, err := cli.ContainerStatPath(ctx, containerName, path)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return FileInfo{}, false, nil
		}
		return FileInfo{}, false, fmt.Errorf("sandbox: docker Lstat %s: %w", path, err)
	}

	fm := os.FileMode(stat.Mode)
	return FileInfo{
		Name:  filepath.Base(path),
		IsDir: fm.IsDir(),
		Size:  stat.Size,
	}, true, nil
}

func (p *DockerProvider) EvalSymlinks(ctx context.Context, path string) (string, error) {
	if err := validateContainerPath(path); err != nil {
		return "", err
	}
	p.mu.RLock()
	started := p.started
	p.mu.RUnlock()
	if !started {
		return "", ErrNotStarted
	}

	stdout, stderr, _, err := p.runInContainerWithResult(ctx, "sh", "-c", fmt.Sprintf("readlink -f %s 2>/dev/null || echo NOTEXIST", shellescape(path)))
	if err != nil {
		return "", fmt.Errorf("sandbox: docker EvalSymlinks %s: %s: %w", path, strings.TrimSpace(stderr), err)
	}

	result := strings.TrimSpace(stdout)
	if result == "NOTEXIST" || result == "" {
		return "", fmt.Errorf("sandbox: path does not exist: %s", path)
	}
	return result, nil
}

// ---- Internal helpers -------------------------------------------------------

func (p *DockerProvider) pullImage(cli *client.Client, ctx context.Context) error {
	switch p.pullPolicy {
	case "never":
		return nil
	case "if-not-present":
		_, _, err := cli.ImageInspectWithRaw(ctx, p.image)
		if err == nil {
			return nil
		}
		if !errdefs.IsNotFound(err) {
			return fmt.Errorf("docker image inspect %s failed: %w", p.image, err)
		}
		fallthrough
	case "always":
		reader, err := cli.ImagePull(ctx, p.image, image.PullOptions{})
		if err != nil {
			return fmt.Errorf("docker pull %s failed: %w", p.image, err)
		}
		defer reader.Close()
		_, _ = io.Copy(io.Discard, reader)
		return nil
	default:
		return nil
	}
}

func (p *DockerProvider) ensureVolume(cli *client.Client, ctx context.Context) error {
	_, err := cli.VolumeInspect(ctx, p.volumeName)
	if err == nil {
		return nil
	}
	if !errdefs.IsNotFound(err) {
		return fmt.Errorf("docker volume inspect failed: %w", err)
	}
	_, err = cli.VolumeCreate(ctx, volume.CreateOptions{Name: p.volumeName})
	if err != nil {
		return fmt.Errorf("docker volume create failed: %w", err)
	}
	return nil
}

func (p *DockerProvider) ensureContainer(cli *client.Client, ctx context.Context) (string, error) {
	inspected, err := cli.ContainerInspect(ctx, p.containerName)
	if err == nil && strings.TrimSpace(inspected.ID) != "" {
		return strings.TrimSpace(inspected.ID), nil
	}
	if err != nil && !errdefs.IsNotFound(err) {
		return "", fmt.Errorf("docker inspect %s failed: %w", p.containerName, err)
	}

	hostConfig := &container.HostConfig{
		Mounts: []mount.Mount{{
			Type:   mount.TypeVolume,
			Source: p.volumeName,
			Target: "/workspace",
		}},
		NetworkMode: container.NetworkMode(p.networkMode),
		RestartPolicy: container.RestartPolicy{
			Name: "no",
		},
	}

	if p.cpuLimit > 0 {
		hostConfig.Resources.NanoCPUs = int64(p.cpuLimit * 1_000_000_000)
	}
	if p.memoryMB > 0 {
		hostConfig.Resources.Memory = int64(p.memoryMB) * 1024 * 1024
	}
	if p.pidsLimit > 0 {
		pids := int64(p.pidsLimit)
		hostConfig.Resources.PidsLimit = &pids
	}
	if p.hardened {
		hostConfig.SecurityOpt = append(hostConfig.SecurityOpt, "no-new-privileges:true")
		hostConfig.CapDrop = append(hostConfig.CapDrop, "ALL")
	}

	resp, err := cli.ContainerCreate(ctx,
		&container.Config{
			Image:      p.image,
			Entrypoint: []string{},
			WorkingDir: "/workspace",
			Env:        p.extraEnv,
			Labels: map[string]string{
				"openclawssy": "true",
				"agent_id":    p.agentID,
			},
			Cmd: []string{"sleep", "infinity"},
		},
		hostConfig,
		nil,
		nil,
		p.containerName,
	)
	if err != nil {
		return "", fmt.Errorf("docker create failed: %w", err)
	}
	return strings.TrimSpace(resp.ID), nil
}

func (p *DockerProvider) startContainerIfNeeded(cli *client.Client, ctx context.Context, containerID string) error {
	inspected, err := cli.ContainerInspect(ctx, containerID)
	if err == nil && inspected.State != nil && inspected.State.Running {
		return nil
	}
	if err != nil && !errdefs.IsNotFound(err) {
		return fmt.Errorf("docker inspect state failed: %w", err)
	}
	if err := cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("docker start failed: %w", err)
	}
	return nil
}

func (p *DockerProvider) runInContainer(ctx context.Context, cmd string, args ...string) error {
	_, stderr, _, err := p.runInContainerWithResult(ctx, append([]string{cmd}, args...)...)
	if err != nil {
		return fmt.Errorf("sandbox: docker exec %s: %s: %w", cmd, strings.TrimSpace(stderr), err)
	}
	return nil
}

func (p *DockerProvider) runInContainerWithResult(ctx context.Context, argv ...string) (string, string, int, error) {
	p.mu.RLock()
	containerName := p.containerName
	p.mu.RUnlock()
	return p.execInContainer(ctx, containerName, "", "", argv)
}

func (p *DockerProvider) runAsRoot(ctx context.Context, cmd string, args ...string) error {
	_, stderr, _, err := p.execAsRoot(ctx, append([]string{cmd}, args...)...)
	if err != nil {
		return fmt.Errorf("sandbox: docker exec (root) %s: %s: %w", cmd, strings.TrimSpace(stderr), err)
	}
	return nil
}

func (p *DockerProvider) execAsRoot(ctx context.Context, argv ...string) (string, string, int, error) {
	p.mu.RLock()
	containerName := p.containerName
	p.mu.RUnlock()
	return p.execInContainer(ctx, containerName, "0", "", argv)
}

func (p *DockerProvider) execInContainer(ctx context.Context, containerName, user, workDir string, argv []string) (string, string, int, error) {
	cli, err := p.dockerClient()
	if err != nil {
		return "", "", -1, err
	}

	createResp, err := cli.ContainerExecCreate(ctx, containerName, container.ExecOptions{
		User:         user,
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          argv,
		WorkingDir:   workDir,
	})
	if err != nil {
		return "", "", -1, err
	}

	attachResp, err := cli.ContainerExecAttach(ctx, createResp.ID, container.ExecAttachOptions{})
	if err != nil {
		return "", "", -1, err
	}
	defer attachResp.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, attachResp.Reader); err != nil {
		return "", "", -1, err
	}

	inspectResp, err := cli.ContainerExecInspect(ctx, createResp.ID)
	if err != nil {
		return stdout.String(), stderr.String(), -1, err
	}

	exitCode := inspectResp.ExitCode
	if exitCode != 0 {
		return stdout.String(), stderr.String(), exitCode, fmt.Errorf("exit status %d", exitCode)
	}

	return stdout.String(), stderr.String(), exitCode, nil
}

func tarSingleFile(name string, data []byte, perm os.FileMode) (io.Reader, error) {
	if perm == 0 {
		perm = 0o644
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{
		Name: filepath.Base(name),
		Mode: int64(perm.Perm()),
		Size: int64(len(data)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return nil, fmt.Errorf("sandbox: write tar header: %w", err)
	}
	if _, err := tw.Write(data); err != nil {
		return nil, fmt.Errorf("sandbox: write tar file content: %w", err)
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("sandbox: finalize tar stream: %w", err)
	}
	return bytes.NewReader(buf.Bytes()), nil
}

// shellescape wraps s in single quotes, escaping any embedded single quotes,
// so the result is safe to embed in a `sh -c '...'` string.
func shellescape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
