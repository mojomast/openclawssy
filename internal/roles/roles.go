package roles

import (
	"fmt"
	"strings"
)

type RoleTemplate struct {
	Name                  string         `json:"name"`
	Description           string         `json:"description"`
	AllowedTools          []string       `json:"allowed_tools"`
	DeniedTools           []string       `json:"denied_tools"`
	MaxIterations         int            `json:"max_iterations"`
	TimeoutMS             int            `json:"timeout_ms"`
	MemoryAccessScope     string         `json:"memory_access_scope"`
	WritablePaths         []string       `json:"writable_paths"`
	PromptContract        string         `json:"prompt_contract"`
	OutputSchema          map[string]any `json:"output_schema"`
	HandoffSchema         map[string]any `json:"handoff_schema"`
	EscalationRules       []string       `json:"escalation_rules"`
	DelegationPermissions []string       `json:"delegation_permissions"`
	IsBuiltin             bool           `json:"is_builtin"`
}

func (r RoleTemplate) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("role name cannot be empty")
	}
	if len(normalizeStringSlice(r.AllowedTools)) == 0 {
		return fmt.Errorf("allowed_tools cannot be empty")
	}
	if r.TimeoutMS < 0 {
		return fmt.Errorf("timeout_ms must be >= 0, got %d", r.TimeoutMS)
	}
	return nil
}

func BuiltInTemplates() []RoleTemplate {
	builtIns := []RoleTemplate{
		{
			Name:              "scout",
			Description:       "Discovery specialist for read-only workspace and web reconnaissance.",
			AllowedTools:      []string{"fs.list", "fs.read", "fs.search", "web.get"},
			MaxIterations:     20,
			TimeoutMS:         120000,
			MemoryAccessScope: "read_only",
			WritablePaths:     []string{},
			PromptContract:    "Collect evidence and summarize findings without mutating files.",
			OutputSchema: map[string]any{
				"type": "object",
			},
			HandoffSchema: map[string]any{
				"type": "object",
			},
			EscalationRules:       []string{"Escalate when required sources are unavailable or conflicting."},
			DelegationPermissions: []string{},
			IsBuiltin:             true,
		},
		{
			Name:                  "planner",
			Description:           "Decomposition specialist for planning and sequencing delegated tasks.",
			AllowedTools:          []string{"fs.list", "fs.read", "fs.search", "web.get", "task.decompose", "task.plan"},
			MaxIterations:         30,
			TimeoutMS:             120000,
			MemoryAccessScope:     "summary",
			PromptContract:        "Break work into clear task nodes with dependencies and success criteria.",
			OutputSchema:          map[string]any{"type": "object"},
			HandoffSchema:         map[string]any{"type": "object"},
			EscalationRules:       []string{"Escalate when task goals are ambiguous or contradictory."},
			DelegationPermissions: []string{"scout", "implementer", "verifier", "reviewer", "operator"},
			IsBuiltin:             true,
		},
		{
			Name:                  "implementer",
			Description:           "Execution specialist for patching code and running shell workflows.",
			AllowedTools:          []string{"fs.list", "fs.read", "fs.search", "fs.write", "fs.append", "fs.delete", "fs.move", "fs.edit", "fs.mkdir", "shell.exec"},
			MaxIterations:         100,
			TimeoutMS:             120000,
			MemoryAccessScope:     "workspace",
			WritablePaths:         []string{"workspace/**"},
			PromptContract:        "Apply minimal safe changes and report precise diffs.",
			OutputSchema:          map[string]any{"type": "object"},
			HandoffSchema:         map[string]any{"type": "object"},
			EscalationRules:       []string{"Escalate if required write scope exceeds writable paths."},
			DelegationPermissions: []string{"scout", "verifier", "reviewer"},
			IsBuiltin:             true,
		},
		{
			Name:                  "verifier",
			Description:           "Validation specialist for running checks, tests, and evidence gathering.",
			AllowedTools:          []string{"fs.list", "fs.read", "fs.search", "web.get", "test.run", "check.run", "check.lint"},
			MaxIterations:         50,
			TimeoutMS:             120000,
			MemoryAccessScope:     "read_only",
			PromptContract:        "Verify expected behavior with reproducible checks and clear pass/fail evidence.",
			OutputSchema:          map[string]any{"type": "object"},
			HandoffSchema:         map[string]any{"type": "object"},
			EscalationRules:       []string{"Escalate when validation prerequisites are unavailable."},
			DelegationPermissions: []string{},
			IsBuiltin:             true,
		},
		{
			Name:                  "reviewer",
			Description:           "Risk and quality specialist for critical review and audit feedback.",
			AllowedTools:          []string{"fs.list", "fs.read", "fs.search", "web.get", "decision.log"},
			MaxIterations:         30,
			TimeoutMS:             120000,
			MemoryAccessScope:     "summary",
			PromptContract:        "Assess correctness, safety, and maintainability; provide actionable findings.",
			OutputSchema:          map[string]any{"type": "object"},
			HandoffSchema:         map[string]any{"type": "object"},
			EscalationRules:       []string{"Escalate when findings indicate blocking production risk."},
			DelegationPermissions: []string{},
			IsBuiltin:             true,
		},
		{
			Name:                  "operator",
			Description:           "Control-plane specialist for guarded configuration and policy operations.",
			AllowedTools:          []string{"config.get", "config.set"},
			MaxIterations:         10,
			TimeoutMS:             120000,
			MemoryAccessScope:     "none",
			PromptContract:        "Operate only on approved configuration surfaces and preserve safety defaults.",
			OutputSchema:          map[string]any{"type": "object"},
			HandoffSchema:         map[string]any{"type": "object"},
			EscalationRules:       []string{"Escalate before making high-impact policy changes."},
			DelegationPermissions: []string{},
			IsBuiltin:             true,
		},
	}

	out := make([]RoleTemplate, 0, len(builtIns))
	for _, template := range builtIns {
		normalized, err := normalizeTemplate(template, true)
		if err != nil {
			panic(fmt.Sprintf("roles: invalid built-in template %q: %v", template.Name, err))
		}
		out = append(out, normalized)
	}
	return out
}

func normalizeTemplate(template RoleTemplate, builtIn bool) (RoleTemplate, error) {
	normalized := template
	normalized.Name = normalizeRoleName(template.Name)
	normalized.Description = strings.TrimSpace(template.Description)
	normalized.AllowedTools = normalizeStringSlice(template.AllowedTools)
	normalized.DeniedTools = normalizeStringSlice(template.DeniedTools)
	normalized.MemoryAccessScope = strings.TrimSpace(template.MemoryAccessScope)
	normalized.WritablePaths = normalizeStringSlice(template.WritablePaths)
	normalized.PromptContract = strings.TrimSpace(template.PromptContract)
	normalized.EscalationRules = normalizeStringSlice(template.EscalationRules)
	normalized.DelegationPermissions = normalizeStringSlice(template.DelegationPermissions)
	normalized.IsBuiltin = builtIn

	if err := normalized.Validate(); err != nil {
		return RoleTemplate{}, err
	}

	return normalized, nil
}

func normalizeRoleName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func normalizeStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}

	if len(normalized) == 0 {
		return nil
	}

	return normalized
}

func cloneTemplate(template RoleTemplate) RoleTemplate {
	cloned := template
	cloned.AllowedTools = append([]string(nil), template.AllowedTools...)
	cloned.DeniedTools = append([]string(nil), template.DeniedTools...)
	cloned.WritablePaths = append([]string(nil), template.WritablePaths...)
	cloned.EscalationRules = append([]string(nil), template.EscalationRules...)
	cloned.DelegationPermissions = append([]string(nil), template.DelegationPermissions...)
	cloned.OutputSchema = cloneMap(template.OutputSchema)
	cloned.HandoffSchema = cloneMap(template.HandoffSchema)
	return cloned
}

func cloneMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}
