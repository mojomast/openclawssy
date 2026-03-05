package agent

import (
	"context"
	"encoding/json"
	"time"
)

type SystemPromptExtender func(ctx context.Context, basePrompt string, messages []ChatMessage, message string, toolResults []ToolCallResult) string

// ArtifactDoc is a prompt source document.
type ArtifactDoc struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// RunInput is the input contract for a single runner invocation.
type RunInput struct {
	AgentID           string                     `json:"agent_id"`
	RunID             string                     `json:"run_id"`
	Message           string                     `json:"message"`
	Messages          []ChatMessage              `json:"messages,omitempty"`
	ArtifactDocs      []ArtifactDoc              `json:"artifact_docs"`
	PerFileByteLimit  int                        `json:"per_file_byte_limit"`
	MaxToolIterations int                        `json:"max_tool_iterations"`
	ToolTimeoutMS     int                        `json:"tool_timeout_ms,omitempty"`
	AllowedTools      []string                   `json:"allowed_tools,omitempty"`
	ToolSchemas       []ToolSchema               `json:"tool_schemas,omitempty"`
	AutoDelegate      bool                       `json:"auto_delegate,omitempty"`
	OnToolCall        func(ToolCallRecord) error `json:"-"`
	SystemPromptExt   SystemPromptExtender       `json:"-"`
	OnTextDelta       func(delta string) error   `json:"-"`
}

// RunOutput is the finalized output contract for a run.
type RunOutput struct {
	Prompt           string           `json:"prompt"`
	FinalText        string           `json:"final_text"`
	Thinking         string           `json:"thinking,omitempty"`
	ThinkingPresent  bool             `json:"thinking_present,omitempty"`
	ToolParseFailure bool             `json:"tool_parse_failure,omitempty"`
	ToolCalls        []ToolCallRecord `json:"tool_calls"`
	StartedAt        time.Time        `json:"started_at"`
	CompletedAt      time.Time        `json:"completed_at"`
}

// ModelRequest is sent to the model on each loop iteration.
type ModelRequest struct {
	AgentID       string                   `json:"agent_id,omitempty"`
	RunID         string                   `json:"run_id,omitempty"`
	SystemPrompt  string                   `json:"system_prompt,omitempty"`
	Messages      []ChatMessage            `json:"messages,omitempty"`
	AllowedTools  []string                 `json:"allowed_tools,omitempty"`
	ToolSchemas   []ToolSchema             `json:"tool_schemas,omitempty"`
	ToolTimeoutMS int                      `json:"tool_timeout_ms,omitempty"`
	Prompt        string                   `json:"prompt,omitempty"`
	Message       string                   `json:"message,omitempty"`
	ToolResults   []ToolCallResult         `json:"tool_results"`
	OnTextDelta   func(delta string) error `json:"-"`
}

// ToolSchema describes an LLM-callable tool and its JSON schema parameters.
type ToolSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// ChatMessage is a role-tagged conversational turn passed to the model.
type ChatMessage struct {
	Role       string    `json:"role"`
	Content    string    `json:"content"`
	Name       string    `json:"name,omitempty"`
	ToolCallID string    `json:"tool_call_id,omitempty"`
	TS         time.Time `json:"ts,omitempty"`
}

// ToolCallRequest is a model-requested tool invocation.
type ToolCallRequest struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ModelResponse returns final text and optional tool calls.
type ModelResponse struct {
	FinalText        string            `json:"final_text"`
	Thinking         string            `json:"thinking,omitempty"`
	ThinkingPresent  bool              `json:"thinking_present,omitempty"`
	ToolParseFailure bool              `json:"tool_parse_failure,omitempty"`
	ToolCalls        []ToolCallRequest `json:"tool_calls"`
	PromptTokens     int               `json:"prompt_tokens,omitempty"`
	TotalTokens      int               `json:"total_tokens,omitempty"`
}

// ToolCallResult is the result returned by a tool executor.
type ToolCallResult struct {
	ID     string `json:"id"`
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}

// ToolCallRecord stores request + result for audit/output.
type ToolCallRecord struct {
	Request     ToolCallRequest `json:"request"`
	Result      ToolCallResult  `json:"result"`
	CallbackErr string          `json:"callback_error,omitempty"`
	StartedAt   time.Time       `json:"started_at"`
	CompletedAt time.Time       `json:"completed_at"`
}

// Model is an injectable model backend.
type Model interface {
	Generate(ctx context.Context, req ModelRequest) (ModelResponse, error)
}

// ToolExecutor is an injectable tool backend.
type ToolExecutor interface {
	Execute(ctx context.Context, call ToolCallRequest) (ToolCallResult, error)
}

// SubAgentRunner is used for delegating tasks to other agents.
// It is compatible with tools.AgentRunner interface.
type SubAgentRunner interface {
	ExecuteSubAgent(ctx context.Context, input DecomposedTask) (SubAgentOutput, error)
}

// SubAgentOutput is returned from subagent execution.
type SubAgentOutput struct {
	RunID     string `json:"run_id"`
	FinalText string `json:"final_text"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
}
