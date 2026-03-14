package contract

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func sampleContract() AgentContract {
	return AgentContract{
		Identity: Identity{
			AgentID:     "default",
			DisplayName: "Default Agent",
		},
		Mission: Mission{
			Description: "Handle coding tasks safely",
			Goals:       []string{"correctness", "safety"},
		},
		SystemPrompt: SystemPrompt{
			Content: "You are a helpful coding assistant.",
			Source:  "SOUL.md",
		},
		ToolPolicy: ToolPolicy{
			AllowedTools: []string{"fs.read", "code.search"},
			DeniedTools:  []string{"shell.exec"},
			DefaultDeny:  true,
		},
		DelegationPolicy: DelegationPolicy{
			Mode:         "tool_gated",
			Threshold:    2,
			Cooldown:     15,
			AutoDelegate: true,
			AgentID:      "default",
			MaxDepth:     3,
		},
		MemoryPolicy: MemoryPolicy{
			Enabled:         true,
			MaxWorkingItems: 200,
			MaxPromptTokens: 1200,
			AutoCheckpoint:  true,
			Proactive:       true,
			Embeddings:      true,
		},
		ModelPolicy: ModelPolicy{
			Provider:    "zai",
			Model:       "GLM-4.7",
			Temperature: 0.2,
			MaxTokens:   32000,
			TimeoutMS:   120000,
		},
		SandboxPolicy: SandboxPolicy{
			Active:   false,
			Provider: "none",
			Docker: SandboxDockerConfig{
				Image:          "ubuntu:24.04",
				NetworkEnabled: false,
				CPULimit:       1.0,
				MemoryLimitMB:  1024,
				PullPolicy:     "if-not-present",
			},
		},
		ObservabilityPolicy: ObservabilityPolicy{
			AuditEnabled: true,
			TraceEnabled: true,
			ThinkingMode: "never",
		},
		Inheritance: Inheritance{
			Source: map[string]string{
				"identity":             InheritanceSourceGlobal,
				"mission":              InheritanceSourceGlobal,
				"system_prompt":        InheritanceSourceAgentProfile,
				"tool_policy":          InheritanceSourceSubagentOverride,
				"delegation_policy":    InheritanceSourceGlobal,
				"memory_policy":        InheritanceSourceGlobal,
				"model_policy":         InheritanceSourceGlobal,
				"sandbox_policy":       InheritanceSourceGlobal,
				"observability_policy": InheritanceSourceGlobal,
			},
		},
	}
}

func TestAgentContractValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(c *AgentContract)
		wantErr string
	}{
		{
			name: "valid contract",
		},
		{
			name: "empty agent id",
			mutate: func(c *AgentContract) {
				c.Identity.AgentID = "   "
			},
			wantErr: "agent_id",
		},
		{
			name: "negative model timeout",
			mutate: func(c *AgentContract) {
				c.ModelPolicy.TimeoutMS = -1
			},
			wantErr: "timeout_ms",
		},
		{
			name: "unknown delegation mode",
			mutate: func(c *AgentContract) {
				c.DelegationPolicy.Mode = "mystery-mode"
			},
			wantErr: "delegation mode",
		},
		{
			name: "missing inheritance top-level key",
			mutate: func(c *AgentContract) {
				delete(c.Inheritance.Source, "model_policy")
			},
			wantErr: "model_policy",
		},
		{
			name: "invalid inheritance source value",
			mutate: func(c *AgentContract) {
				c.Inheritance.Source["identity"] = "invalid-source"
			},
			wantErr: "inheritance",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			contract := sampleContract()
			if tt.mutate != nil {
				tt.mutate(&contract)
			}

			err := contract.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("Validate() error = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.wantErr)) {
				t.Fatalf("Validate() error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestAgentContractJSONSerialization(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(sampleContract())
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	topLevel := []string{
		"identity",
		"mission",
		"system_prompt",
		"tool_policy",
		"delegation_policy",
		"memory_policy",
		"model_policy",
		"sandbox_policy",
		"observability_policy",
		"inheritance",
	}
	for _, key := range topLevel {
		if _, ok := payload[key]; !ok {
			t.Fatalf("top-level key %q missing in JSON payload", key)
		}
	}

	identity := payload["identity"].(map[string]any)
	if _, ok := identity["agent_id"]; !ok {
		t.Fatalf("identity.agent_id key missing")
	}
	if _, ok := identity["display_name"]; !ok {
		t.Fatalf("identity.display_name key missing")
	}

	delegation := payload["delegation_policy"].(map[string]any)
	if _, ok := delegation["mode"]; !ok {
		t.Fatalf("delegation_policy.mode key missing")
	}
	if _, ok := delegation["cooldown"]; !ok {
		t.Fatalf("delegation_policy.cooldown key missing")
	}

	memory := payload["memory_policy"].(map[string]any)
	if _, ok := memory["embeddings"]; !ok {
		t.Fatalf("memory_policy.embeddings key missing")
	}

	model := payload["model_policy"].(map[string]any)
	if _, ok := model["timeout_ms"]; !ok {
		t.Fatalf("model_policy.timeout_ms key missing")
	}

	sandbox := payload["sandbox_policy"].(map[string]any)
	docker := sandbox["docker"].(map[string]any)
	if _, ok := docker["image"]; !ok {
		t.Fatalf("sandbox_policy.docker.image key missing")
	}

	inheritance := payload["inheritance"].(map[string]any)
	if _, ok := inheritance["source"]; !ok {
		t.Fatalf("inheritance.source key missing")
	}
}

func TestAgentContractString(t *testing.T) {
	t.Parallel()

	contract := sampleContract()
	serialized := contract.String()
	if serialized == "" {
		t.Fatal("String() returned empty output")
	}
	if !strings.Contains(serialized, `"agent_id": "default"`) {
		t.Fatalf("String() missing agent_id value: %s", serialized)
	}
	if !json.Valid([]byte(serialized)) {
		t.Fatalf("String() output is not valid JSON: %s", serialized)
	}

	var decoded AgentContract
	if err := json.Unmarshal([]byte(serialized), &decoded); err != nil {
		t.Fatalf("failed to decode String() output: %v", err)
	}
	if decoded.Identity.AgentID != contract.Identity.AgentID {
		t.Fatalf("decoded identity.agent_id = %q, want %q", decoded.Identity.AgentID, contract.Identity.AgentID)
	}
}

func TestAllContractStructsHaveJSONTags(t *testing.T) {
	t.Parallel()

	types := []reflect.Type{
		reflect.TypeOf(AgentContract{}),
		reflect.TypeOf(Identity{}),
		reflect.TypeOf(Mission{}),
		reflect.TypeOf(SystemPrompt{}),
		reflect.TypeOf(ToolPolicy{}),
		reflect.TypeOf(DelegationPolicy{}),
		reflect.TypeOf(MemoryPolicy{}),
		reflect.TypeOf(ModelPolicy{}),
		reflect.TypeOf(SandboxPolicy{}),
		reflect.TypeOf(SandboxDockerConfig{}),
		reflect.TypeOf(ObservabilityPolicy{}),
		reflect.TypeOf(Inheritance{}),
	}

	for _, typ := range types {
		typ := typ
		t.Run(typ.Name(), func(t *testing.T) {
			t.Parallel()
			assertJSONTags(t, typ)
		})
	}
}

func assertJSONTags(t *testing.T, typ reflect.Type) {
	t.Helper()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}
		tag := strings.TrimSpace(field.Tag.Get("json"))
		if tag == "" {
			t.Fatalf("%s.%s missing json tag", typ.Name(), field.Name)
		}
		if tag == "-" {
			t.Fatalf("%s.%s has json tag '-'", typ.Name(), field.Name)
		}
	}
}
