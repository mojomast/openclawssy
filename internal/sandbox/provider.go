package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"openclawssy/internal/config"
)

var (
	ErrExecDenied      = errors.New("sandbox: exec denied")
	ErrNotStarted      = errors.New("sandbox: provider not started")
	ErrUnknownProvider = errors.New("sandbox: unknown provider")
	ErrDockerNotYet    = errors.New("sandbox: docker provider not yet available")

	errFileOpsNotAvailable = errors.New("sandbox: file operations not available in none provider")
)

// FileInfo describes a filesystem entry returned by sandbox file operations.
type FileInfo struct {
	Name  string
	IsDir bool
	Size  int64
}

// ExecOptions carries optional parameters for command execution.
type ExecOptions struct {
	WorkDir string
	Env     []string
}

type Command struct {
	Name string
	Args []string
}

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Provider is the sandbox abstraction.  Local and docker implementations must
// satisfy all methods — including the file-operation methods so that Docker
// containers can intercept every fs.* tool call.
type Provider interface {
	Start(runCtx context.Context) error
	Exec(cmd Command) (Result, error)
	Stop() error

	// File operations — local provider uses host fs, docker uses container.
	ReadFile(ctx context.Context, path string) ([]byte, error)
	WriteFile(ctx context.Context, path string, data []byte, perm os.FileMode) error
	ListDir(ctx context.Context, path string) ([]FileInfo, error)
	MkdirAll(ctx context.Context, path string, perm os.FileMode) error
	Remove(ctx context.Context, path string, recursive bool) error
	Rename(ctx context.Context, src, dst string) error
	// Lstat returns (info, exists, err).  exists is false (with nil err) when
	// the path simply does not exist.
	Lstat(ctx context.Context, path string) (FileInfo, bool, error)
	EvalSymlinks(ctx context.Context, path string) (string, error)
}

// ProviderFS is a helper type so tools can use a Provider as a filesystem.
// It satisfies the FS interface required by tools.
type ProviderFS struct {
	P Provider
}

type providerState interface {
	providerName() string
	isStarted() bool
}

func NewProvider(name string, workspace string) (Provider, error) {
	switch name {
	case "none":
		return &NoneProvider{}, nil
	case "local":
		return NewLocalProvider(workspace)
	case "docker":
		// Docker provider is planned for Phase 2.  The config layer allows
		// "docker" as a valid provider name so that configs can be written
		// ahead of time; runtime creation will fail gracefully until Phase 2
		// ships.
		return nil, ErrDockerNotYet
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnknownProvider, name)
	}
}

// NewProviderForAgent creates a Provider for a specific agent run.
// For the docker provider, agentID is used as the container/volume identifier
// and the full DockerSandboxConfig is applied.
// For all other providers the workspace path is used as before.
func NewProviderForAgent(name, workspace, agentID string, dockerCfg config.DockerSandboxConfig) (Provider, error) {
	switch name {
	case "none":
		return &NoneProvider{}, nil
	case "local":
		return NewLocalProvider(workspace)
	case "docker":
		if agentID == "" {
			agentID = "default"
		}
		return NewDockerProvider(agentID, dockerCfg)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnknownProvider, name)
	}
}

func ShellExecAllowed(active Provider) bool {
	if active == nil {
		return false
	}
	s, ok := active.(providerState)
	if !ok {
		return false
	}
	return s.providerName() != "none" && s.isStarted()
}

type NoneProvider struct {
	mu      sync.RWMutex
	started bool
}

func (p *NoneProvider) Start(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.started = true
	return nil
}

func (p *NoneProvider) Exec(Command) (Result, error) {
	return Result{}, ErrExecDenied
}

func (p *NoneProvider) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.started = false
	return nil
}

func (p *NoneProvider) providerName() string { return "none" }

func (p *NoneProvider) isStarted() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.started
}

// File operations — NoneProvider disallows all file access.
func (p *NoneProvider) ReadFile(_ context.Context, _ string) ([]byte, error) {
	return nil, errFileOpsNotAvailable
}
func (p *NoneProvider) WriteFile(_ context.Context, _ string, _ []byte, _ os.FileMode) error {
	return errFileOpsNotAvailable
}
func (p *NoneProvider) ListDir(_ context.Context, _ string) ([]FileInfo, error) {
	return nil, errFileOpsNotAvailable
}
func (p *NoneProvider) MkdirAll(_ context.Context, _ string, _ os.FileMode) error {
	return errFileOpsNotAvailable
}
func (p *NoneProvider) Remove(_ context.Context, _ string, _ bool) error {
	return errFileOpsNotAvailable
}
func (p *NoneProvider) Rename(_ context.Context, _, _ string) error {
	return errFileOpsNotAvailable
}
func (p *NoneProvider) Lstat(_ context.Context, _ string) (FileInfo, bool, error) {
	return FileInfo{}, false, errFileOpsNotAvailable
}
func (p *NoneProvider) EvalSymlinks(_ context.Context, _ string) (string, error) {
	return "", errFileOpsNotAvailable
}

type LocalProvider struct {
	workspace string

	mu      sync.RWMutex
	started bool
	runCtx  context.Context
	cancel  context.CancelFunc
}

func NewLocalProvider(workspace string) (*LocalProvider, error) {
	if workspace == "" {
		return nil, errors.New("sandbox: workspace is required")
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("sandbox: resolve workspace: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("sandbox: stat workspace: %w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("sandbox: workspace must be a directory")
	}
	return &LocalProvider{workspace: abs}, nil
}

func (p *LocalProvider) Start(runCtx context.Context) error {
	if runCtx == nil {
		runCtx = context.Background()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.runCtx, p.cancel = context.WithCancel(runCtx)
	p.started = true
	return nil
}

func (p *LocalProvider) Exec(cmd Command) (Result, error) {
	p.mu.RLock()
	started := p.started
	runCtx := p.runCtx
	workspace := p.workspace
	p.mu.RUnlock()

	if !started {
		return Result{}, ErrNotStarted
	}
	if cmd.Name == "" {
		return Result{}, errors.New("sandbox: command name is required")
	}

	proc := exec.CommandContext(runCtx, cmd.Name, cmd.Args...)
	proc.Dir = workspace

	var stdout bytes.Buffer
	var stderr bytes.Buffer
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

func (p *LocalProvider) Stop() error {
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

func (p *LocalProvider) providerName() string { return "local" }

func (p *LocalProvider) isStarted() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.started
}

// File operations — LocalProvider delegates directly to the host OS.

func (p *LocalProvider) ReadFile(_ context.Context, path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (p *LocalProvider) WriteFile(_ context.Context, path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

func (p *LocalProvider) ListDir(_ context.Context, path string) ([]FileInfo, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]FileInfo, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		size := int64(0)
		if err == nil {
			size = info.Size()
		}
		out = append(out, FileInfo{
			Name:  e.Name(),
			IsDir: e.IsDir(),
			Size:  size,
		})
	}
	return out, nil
}

func (p *LocalProvider) MkdirAll(_ context.Context, path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (p *LocalProvider) Remove(_ context.Context, path string, recursive bool) error {
	if recursive {
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}

func (p *LocalProvider) Rename(_ context.Context, src, dst string) error {
	return os.Rename(src, dst)
}

func (p *LocalProvider) Lstat(_ context.Context, path string) (FileInfo, bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return FileInfo{}, false, nil
		}
		return FileInfo{}, false, err
	}
	return FileInfo{
		Name:  filepath.Base(path),
		IsDir: info.IsDir(),
		Size:  info.Size(),
	}, true, nil
}

func (p *LocalProvider) EvalSymlinks(_ context.Context, path string) (string, error) {
	return filepath.EvalSymlinks(path)
}
