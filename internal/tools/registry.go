package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

type Policy interface {
	CheckTool(agentID, tool string) error
	ResolveReadPath(workspace, target string) (string, error)
	ResolveWritePath(workspace, target string) (string, error)
}

type Auditor interface {
	LogEvent(ctx context.Context, eventType string, fields map[string]any) error
}

type ToolSpec struct {
	Name        string
	Description string
	Required    []string
	ArgTypes    map[string]ArgType
}

type ArgType string

const (
	ArgTypeString ArgType = "string"
	ArgTypeNumber ArgType = "number"
	ArgTypeBool   ArgType = "bool"
	ArgTypeObject ArgType = "object"
	ArgTypeArray  ArgType = "array"
)

type ShellExecutor interface {
	Exec(ctx context.Context, command string, args []string, workDir string) (stdout string, stderr string, exitCode int, err error)
}

type Handler func(ctx context.Context, req Request) (map[string]any, error)

type Request struct {
	AgentID              string
	Tool                 string
	Workspace            string
	Args                 map[string]any
	Policy               Policy
	Shell                ShellExecutor
	ShellAllowedCommands []string
	SandboxProvider      SandboxFileOps // nil = use host OS directly
}

type registryItem struct {
	spec    ToolSpec
	handler Handler
}

type Registry struct {
	policy               Policy
	audit                Auditor
	shell                ShellExecutor
	shellAllowedCommands []string
	sandboxProvider      SandboxFileOps
	mu                   sync.RWMutex
	tools                map[string]registryItem
}

func NewRegistry(policy Policy, audit Auditor) *Registry {
	return &Registry{
		policy: policy,
		audit:  audit,
		tools:  make(map[string]registryItem),
	}
}

func (r *Registry) SetShellExecutor(shell ShellExecutor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.shell = shell
}

func (r *Registry) SetSandboxProvider(sp SandboxFileOps) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sandboxProvider = sp
}

func (r *Registry) SetShellAllowedCommands(prefixes []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.shellAllowedCommands = append([]string(nil), prefixes...)
}

func (r *Registry) Register(spec ToolSpec, handler Handler) error {
	if spec.Name == "" {
		return errors.New("tool name is required")
	}
	if handler == nil {
		return errors.New("tool handler is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[spec.Name]; exists {
		return fmt.Errorf("tool already registered: %s", spec.Name)
	}
	r.tools[spec.Name] = registryItem{spec: spec, handler: handler}
	return nil
}

func (r *Registry) List() []ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]ToolSpec, 0, len(r.tools))
	for _, item := range r.tools {
		out = append(out, item.spec)
	}
	return out
}

func (r *Registry) Execute(ctx context.Context, agentID, name, workspace string, args map[string]any) (map[string]any, error) {
	if args == nil {
		args = map[string]any{}
	}

	_ = r.emit(ctx, "tool.call", map[string]any{
		"agent_id": agentID,
		"tool":     name,
		"args":     sanitizeAuditArgs(name, args),
	})

	r.mu.RLock()
	item, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		err := &ToolError{Code: ErrCodeNotFound, Tool: name, Message: "tool not registered"}
		_ = r.emit(ctx, "tool.result", map[string]any{"agent_id": agentID, "tool": name, "error": err.Error()})
		return nil, err
	}

	for _, required := range item.spec.Required {
		value, ok := args[required]
		if !ok {
			err := &ToolError{Code: ErrCodeInvalidInput, Tool: name, Message: "missing required field: " + required}
			_ = r.emit(ctx, "tool.result", map[string]any{"agent_id": agentID, "tool": name, "error": err.Error()})
			return nil, err
		}
		if expected, ok := item.spec.ArgTypes[required]; ok && !matchesArgType(value, expected) {
			err := &ToolError{Code: ErrCodeInvalidInput, Tool: name, Message: fmt.Sprintf("invalid type for field %s: expected %s", required, expected)}
			_ = r.emit(ctx, "tool.result", map[string]any{"agent_id": agentID, "tool": name, "error": err.Error()})
			return nil, err
		}
	}
	for field, expected := range item.spec.ArgTypes {
		value, ok := args[field]
		if !ok {
			continue
		}
		if !matchesArgType(value, expected) {
			err := &ToolError{Code: ErrCodeInvalidInput, Tool: name, Message: fmt.Sprintf("invalid type for field %s: expected %s", field, expected)}
			_ = r.emit(ctx, "tool.result", map[string]any{"agent_id": agentID, "tool": name, "error": err.Error()})
			return nil, err
		}
	}

	if r.policy != nil {
		if err := r.policy.CheckTool(agentID, name); err != nil {
			denied := wrapError(ErrCodePolicyDenied, name, err)
			_ = r.emit(ctx, "policy.denied", map[string]any{
				"agent_id": agentID,
				"tool":     name,
				"error":    denied.Error(),
			})
			_ = r.emit(ctx, "tool.result", map[string]any{"agent_id": agentID, "tool": name, "error": denied.Error()})
			return nil, denied
		}
	}

	res, err := item.handler(ctx, Request{
		AgentID:              agentID,
		Tool:                 name,
		Workspace:            workspace,
		Args:                 args,
		Policy:               r.policy,
		Shell:                r.shell,
		ShellAllowedCommands: append([]string(nil), r.shellAllowedCommands...),
		SandboxProvider:      r.sandboxProvider,
	})
	if err != nil {
		errCode := classifyToolErrorCode(err)
		execErr := wrapError(errCode, name, err)
		_ = r.emit(ctx, "tool.result", map[string]any{"agent_id": agentID, "tool": name, "error": execErr.Error()})
		return nil, execErr
	}

	_ = r.emit(ctx, "tool.result", map[string]any{
		"agent_id": agentID,
		"tool":     name,
		"result":   sanitizeAuditResult(name, res),
	})
	return res, nil
}

func sanitizeAuditArgs(tool string, args map[string]any) map[string]any {
	if !strings.HasPrefix(strings.TrimSpace(tool), "secrets.") {
		return args
	}
	if len(args) == 0 {
		return args
	}
	cloned := make(map[string]any, len(args))
	for k, v := range args {
		cloned[k] = v
	}
	if _, ok := cloned["value"]; ok {
		cloned["value"] = "[REDACTED]"
	}
	return cloned
}

func sanitizeAuditResult(tool string, result map[string]any) map[string]any {
	if !strings.HasPrefix(strings.TrimSpace(tool), "secrets.") {
		return result
	}
	if len(result) == 0 {
		return result
	}
	cloned := make(map[string]any, len(result))
	for k, v := range result {
		cloned[k] = v
	}
	if _, ok := cloned["value"]; ok {
		cloned["value"] = "[REDACTED]"
	}
	return cloned
}

func classifyToolErrorCode(err error) ErrorCode {
	if err == nil {
		return ErrCodeExecution
	}
	var toolErr *ToolError
	if errors.As(err, &toolErr) {
		if toolErr.Code != "" {
			return toolErr.Code
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrCodeTimeout
	}
	lower := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(lower, "invalid") || strings.Contains(lower, "must be") || strings.Contains(lower, "required") || strings.Contains(lower, "missing") {
		return ErrCodeInvalidInput
	}
	return ErrCodeExecution
}

func matchesArgType(value any, expected ArgType) bool {
	switch expected {
	case ArgTypeString:
		_, ok := value.(string)
		return ok
	case ArgTypeNumber:
		switch value.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			return true
		default:
			return false
		}
	case ArgTypeBool:
		_, ok := value.(bool)
		return ok
	case ArgTypeObject:
		_, ok := value.(map[string]any)
		return ok
	case ArgTypeArray:
		switch value.(type) {
		case []any, []string, []int, []float64, []bool:
			return true
		default:
			return false
		}
	default:
		return true
	}
}

func (r *Registry) emit(ctx context.Context, eventType string, fields map[string]any) error {
	if r.audit == nil {
		return nil
	}
	return r.audit.LogEvent(ctx, eventType, fields)
}
