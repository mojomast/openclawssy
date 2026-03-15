package roles

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRoleTemplateValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*RoleTemplate)
		wantErr string
	}{
		{
			name: "valid template",
		},
		{
			name: "empty allowed tools",
			mutate: func(template *RoleTemplate) {
				template.AllowedTools = nil
			},
			wantErr: "allowed_tools",
		},
		{
			name: "negative timeout",
			mutate: func(template *RoleTemplate) {
				template.TimeoutMS = -1
			},
			wantErr: "timeout_ms",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			template := RoleTemplate{
				Name:          "custom-reviewer",
				Description:   "Custom review role",
				AllowedTools:  []string{"fs.read", "fs.search"},
				MaxIterations: 10,
				TimeoutMS:     1000,
			}

			if tt.mutate != nil {
				tt.mutate(&template)
			}

			err := template.Validate()
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

func TestBuiltInTemplates(t *testing.T) {
	t.Parallel()

	builtIns := BuiltInTemplates()
	if len(builtIns) != 6 {
		t.Fatalf("BuiltInTemplates() length = %d, want 6", len(builtIns))
	}

	byName := make(map[string]RoleTemplate, len(builtIns))
	for _, template := range builtIns {
		if !template.IsBuiltin {
			t.Fatalf("built-in template %q has IsBuiltin=false", template.Name)
		}
		if err := template.Validate(); err != nil {
			t.Fatalf("built-in template %q Validate() error = %v", template.Name, err)
		}
		byName[template.Name] = template
	}

	scout := byName["scout"]
	if !reflect.DeepEqual(scout.AllowedTools, []string{"fs.list", "fs.read", "fs.search", "web.get"}) {
		t.Fatalf("scout allowed tools = %v, want [fs.list fs.read fs.search web.get]", scout.AllowedTools)
	}
	if scout.MaxIterations != 20 {
		t.Fatalf("scout max_iterations = %d, want 20", scout.MaxIterations)
	}
	if len(scout.WritablePaths) != 0 {
		t.Fatalf("scout writable_paths = %v, want none", scout.WritablePaths)
	}

	planner := byName["planner"]
	if planner.MaxIterations != 30 {
		t.Fatalf("planner max_iterations = %d, want 30", planner.MaxIterations)
	}
	assertContainsTools(t, planner.AllowedTools, []string{"fs.list", "fs.read", "fs.search", "web.get", "task.decompose"})

	implementer := byName["implementer"]
	if implementer.MaxIterations != 100 {
		t.Fatalf("implementer max_iterations = %d, want 100", implementer.MaxIterations)
	}
	assertContainsTools(t, implementer.AllowedTools, []string{"fs.list", "fs.read", "fs.search", "fs.write", "fs.append", "fs.delete", "fs.move", "fs.edit", "fs.mkdir", "shell.exec"})

	verifier := byName["verifier"]
	if verifier.MaxIterations != 50 {
		t.Fatalf("verifier max_iterations = %d, want 50", verifier.MaxIterations)
	}
	assertContainsTools(t, verifier.AllowedTools, []string{"fs.list", "fs.read", "fs.search", "web.get", "test.run", "check.run"})

	reviewer := byName["reviewer"]
	if reviewer.MaxIterations != 30 {
		t.Fatalf("reviewer max_iterations = %d, want 30", reviewer.MaxIterations)
	}
	assertContainsTools(t, reviewer.AllowedTools, []string{"fs.list", "fs.read", "fs.search", "web.get", "decision.log"})

	operator := byName["operator"]
	if operator.MaxIterations != 10 {
		t.Fatalf("operator max_iterations = %d, want 10", operator.MaxIterations)
	}
	if !reflect.DeepEqual(operator.AllowedTools, []string{"config.get", "config.set"}) {
		t.Fatalf("operator allowed tools = %v, want [config.get config.set]", operator.AllowedTools)
	}
}

func TestRoleStoreCustomCRUD(t *testing.T) {
	t.Parallel()

	store, err := NewRoleStore(nil)
	if err != nil {
		t.Fatalf("NewRoleStore() error = %v", err)
	}

	custom := RoleTemplate{
		Name:          "analyst",
		Description:   "Custom analyst",
		AllowedTools:  []string{"fs.read"},
		MaxIterations: 12,
		TimeoutMS:     2000,
		IsBuiltin:     true,
	}

	if err := store.CreateCustom(custom); err != nil {
		t.Fatalf("CreateCustom() error = %v", err)
	}

	created, ok := store.Get("analyst")
	if !ok {
		t.Fatal("Get(analyst) ok = false, want true")
	}
	if created.IsBuiltin {
		t.Fatal("created custom role has IsBuiltin=true, want false")
	}

	list := store.List()
	if len(list) != 7 {
		t.Fatalf("List() length = %d, want 7", len(list))
	}

	updated := created
	updated.Description = "Updated custom analyst"
	updated.AllowedTools = []string{"fs.read", "fs.search"}

	if err := store.UpdateCustom(updated); err != nil {
		t.Fatalf("UpdateCustom() error = %v", err)
	}

	reloaded, ok := store.Get("analyst")
	if !ok {
		t.Fatal("Get(analyst after update) ok = false, want true")
	}
	if reloaded.Description != "Updated custom analyst" {
		t.Fatalf("updated description = %q, want %q", reloaded.Description, "Updated custom analyst")
	}
	if !reflect.DeepEqual(reloaded.AllowedTools, []string{"fs.read", "fs.search"}) {
		t.Fatalf("updated allowed tools = %v, want [fs.read fs.search]", reloaded.AllowedTools)
	}

	if err := store.Delete("analyst"); err != nil {
		t.Fatalf("Delete(analyst) error = %v", err)
	}

	if _, ok := store.Get("analyst"); ok {
		t.Fatal("Get(analyst) ok = true after delete, want false")
	}

	if len(store.List()) != 6 {
		t.Fatalf("List() length after delete = %d, want 6", len(store.List()))
	}
}

func TestRoleStoreBuiltInTemplatesCannotBeDeletedOrUpdated(t *testing.T) {
	t.Parallel()

	store, err := NewRoleStore(nil)
	if err != nil {
		t.Fatalf("NewRoleStore() error = %v", err)
	}

	if err := store.Delete("scout"); !errors.Is(err, ErrBuiltInRoleImmutable) {
		t.Fatalf("Delete(scout) error = %v, want ErrBuiltInRoleImmutable", err)
	}

	err = store.UpdateCustom(RoleTemplate{Name: "scout", AllowedTools: []string{"fs.read"}})
	if !errors.Is(err, ErrBuiltInRoleImmutable) {
		t.Fatalf("UpdateCustom(scout) error = %v, want ErrBuiltInRoleImmutable", err)
	}
}

func TestRoleStoreRejectsDuplicateNames(t *testing.T) {
	t.Parallel()

	_, err := NewRoleStore([]RoleTemplate{{Name: "scout", AllowedTools: []string{"fs.read"}}})
	if !errors.Is(err, ErrDuplicateRoleName) {
		t.Fatalf("NewRoleStore(duplicate with built-in) error = %v, want ErrDuplicateRoleName", err)
	}

	store, err := NewRoleStore(nil)
	if err != nil {
		t.Fatalf("NewRoleStore() error = %v", err)
	}

	if err := store.CreateCustom(RoleTemplate{Name: "analyst", AllowedTools: []string{"fs.read"}}); err != nil {
		t.Fatalf("CreateCustom(analyst) error = %v", err)
	}

	err = store.CreateCustom(RoleTemplate{Name: "Analyst", AllowedTools: []string{"fs.read"}})
	if !errors.Is(err, ErrDuplicateRoleName) {
		t.Fatalf("CreateCustom(Analyst duplicate) error = %v, want ErrDuplicateRoleName", err)
	}
}

func TestRoleTemplateJSONSerialization(t *testing.T) {
	t.Parallel()

	template := RoleTemplate{
		Name:                  "scout",
		Description:           "Read-only research specialist",
		AllowedTools:          []string{"fs.list", "fs.read"},
		DeniedTools:           []string{"shell.exec"},
		MaxIterations:         20,
		TimeoutMS:             5000,
		MemoryAccessScope:     "read_only",
		WritablePaths:         []string{},
		PromptContract:        "Return only findings with evidence.",
		OutputSchema:          map[string]any{"type": "object"},
		HandoffSchema:         map[string]any{"type": "object"},
		EscalationRules:       []string{"Escalate when data is missing."},
		DelegationPermissions: []string{"planner"},
		IsBuiltin:             true,
	}

	raw, err := json.Marshal(template)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("Unmarshal() map error = %v", err)
	}

	keys := []string{
		"name",
		"description",
		"allowed_tools",
		"denied_tools",
		"max_iterations",
		"timeout_ms",
		"memory_access_scope",
		"writable_paths",
		"prompt_contract",
		"output_schema",
		"handoff_schema",
		"escalation_rules",
		"delegation_permissions",
		"is_builtin",
	}

	for _, key := range keys {
		if _, ok := payload[key]; !ok {
			t.Fatalf("json payload missing key %q", key)
		}
	}

	var decoded RoleTemplate
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal() struct error = %v", err)
	}

	if !decoded.IsBuiltin {
		t.Fatal("decoded IsBuiltin = false, want true")
	}
	if decoded.TimeoutMS != 5000 {
		t.Fatalf("decoded TimeoutMS = %d, want 5000", decoded.TimeoutMS)
	}
	if len(decoded.AllowedTools) != 2 {
		t.Fatalf("decoded AllowedTools length = %d, want 2", len(decoded.AllowedTools))
	}
}

func assertContainsTools(t *testing.T, actual []string, expected []string) {
	t.Helper()

	have := make(map[string]struct{}, len(actual))
	for _, tool := range actual {
		have[tool] = struct{}{}
	}

	for _, tool := range expected {
		if _, ok := have[tool]; !ok {
			t.Fatalf("allowed tools %v do not contain required tool %q", actual, tool)
		}
	}
}
