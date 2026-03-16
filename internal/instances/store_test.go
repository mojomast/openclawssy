package instances

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"openclawssy/internal/config"
	"openclawssy/internal/promptstack"
	"openclawssy/internal/roles"
)

func TestSaveLoadInstanceManifest(t *testing.T) {
	root := t.TempDir()
	manifest := DefaultInstanceManifest("alpha")
	manifest.DisplayName = "Alpha"
	manifest.Description = "Alpha instance"
	manifest.Runtime.DefaultAgentID = "worker"
	manifest.Runtime.EnabledAgentIDs = []string{"worker", "reviewer"}
	manifest.Runtime.MaxConcurrentRuns = 4
	manifest.Workspace.Root = "workspace/instances/alpha"
	manifest.Channels = map[string]ChannelRoute{
		"dashboard": {DefaultAgentID: "worker"},
		"discord":   {DefaultAgentID: "reviewer"},
	}
	if err := SaveInstanceManifest(root, manifest); err != nil {
		t.Fatalf("save instance manifest: %v", err)
	}
	loaded, err := LoadInstanceManifest(root, "alpha")
	if err != nil {
		t.Fatalf("load instance manifest: %v", err)
	}
	if loaded.InstanceID != "alpha" || loaded.Runtime.DefaultAgentID != "worker" {
		t.Fatalf("unexpected manifest: %#v", loaded)
	}
	if loaded.CreatedAt.IsZero() || loaded.UpdatedAt.IsZero() {
		t.Fatalf("expected timestamps, got created=%v updated=%v", loaded.CreatedAt, loaded.UpdatedAt)
	}
	manifestPath, err := InstanceManifestPath(root, "alpha")
	if err != nil {
		t.Fatalf("manifest path: %v", err)
	}
	if filepath.Base(manifestPath) != "manifest.json" {
		t.Fatalf("expected manifest.json path, got %q", manifestPath)
	}
}

func TestSaveInstanceManifestPreservesDisabledState(t *testing.T) {
	root := t.TempDir()
	manifest := DefaultInstanceManifest("disabled")
	manifest.Enabled = false
	if err := SaveInstanceManifest(root, manifest); err != nil {
		t.Fatalf("save instance manifest: %v", err)
	}
	loaded, err := LoadInstanceManifest(root, "disabled")
	if err != nil {
		t.Fatalf("load instance manifest: %v", err)
	}
	if loaded.Enabled {
		t.Fatalf("expected disabled instance, got %#v", loaded)
	}
}

func TestSaveLoadListAgents(t *testing.T) {
	root := t.TempDir()
	if err := SaveInstanceManifest(root, DefaultInstanceManifest(DefaultInstanceID)); err != nil {
		t.Fatalf("save default instance: %v", err)
	}
	for _, id := range []string{"worker", "alpha"} {
		manifest := DefaultAgentManifest(id)
		manifest.LegacySourcePath = filepath.Join(root, ".openclawssy", "agents", id)
		if err := SaveAgentManifest(root, DefaultInstanceID, manifest); err != nil {
			t.Fatalf("save agent %s: %v", id, err)
		}
	}
	loaded, err := LoadAgentManifest(root, DefaultInstanceID, "worker")
	if err != nil {
		t.Fatalf("load worker manifest: %v", err)
	}
	if loaded.AgentID != "worker" || loaded.LegacySourcePath == "" {
		t.Fatalf("unexpected loaded agent manifest: %#v", loaded)
	}
	agents, err := ListAgents(root, DefaultInstanceID)
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	got := []string{agents[0].AgentID, agents[1].AgentID}
	if !reflect.DeepEqual(got, []string{"alpha", "worker"}) {
		t.Fatalf("unexpected agent order: %#v", got)
	}
}

func TestBootstrapDefaultInstanceCopiesLegacyDocsAndSeedsSharedFiles(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Workspace.Root = "workspace/current"
	cfg.Chat.DefaultAgentID = "default"
	cfg.Discord.Enabled = true
	cfg.Discord.DefaultAgentID = "ops"
	cfg.Agents.AllowInterAgentMessaging = false
	cfg.Agents.DelegationMode = "approve_plan"
	cfg.Agents.DelegationThreshold = 3
	cfg.Agents.DelegationAgentID = "ops"
	cfg.Agents.DelegationCooldownIter = 9
	cfg.Engine.MaxConcurrentRuns = 7
	cfg.Agents.CustomRoleTemplates = []roles.RoleTemplate{{Name: "repo-reviewer", AllowedTools: []string{"fs.read"}}}
	if err := config.Save(filepath.Join(root, ".openclawssy", "config.json"), cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	legacyDefaultDir := filepath.Join(root, ".openclawssy", "agents", "default")
	legacyOpsDir := filepath.Join(root, ".openclawssy", "agents", "ops")
	for _, dir := range []string{legacyDefaultDir, legacyOpsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(legacyDefaultDir, "SOUL.md"), []byte("# SOUL\nlegacy default"), 0o600); err != nil {
		t.Fatalf("write legacy default soul: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDefaultDir, "TOOLS.md"), []byte("<!-- OPENCLAWSSY_ACTIVATED_SKILLS_START -->\n- repo-research\n<!-- OPENCLAWSSY_ACTIVATED_SKILLS_END -->\n"), 0o600); err != nil {
		t.Fatalf("write legacy tools: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyOpsDir, "HANDOFF.md"), []byte("# HANDOFF\nlegacy ops"), 0o600); err != nil {
		t.Fatalf("write legacy ops handoff: %v", err)
	}
	manifest, err := BootstrapDefaultInstance(root)
	if err != nil {
		t.Fatalf("bootstrap default instance: %v", err)
	}
	if manifest.InstanceID != DefaultInstanceID {
		t.Fatalf("unexpected instance id: %#v", manifest)
	}
	if manifest.Runtime.DefaultAgentID != "default" || manifest.Runtime.MaxConcurrentRuns != 7 {
		t.Fatalf("unexpected runtime config: %#v", manifest.Runtime)
	}
	if manifest.Messaging.AllowInterAgentMessaging {
		t.Fatalf("expected messaging to reflect legacy config, got %#v", manifest.Messaging)
	}
	defaultSoulPath, err := AgentDocPath(root, DefaultInstanceID, "default", "SOUL.md")
	if err != nil {
		t.Fatalf("default soul path: %v", err)
	}
	defaultSoul, err := os.ReadFile(defaultSoulPath)
	if err != nil {
		t.Fatalf("read bootstrapped default soul: %v", err)
	}
	if string(defaultSoul) != "# SOUL\nlegacy default" {
		t.Fatalf("expected legacy SOUL.md copy, got %q", string(defaultSoul))
	}
	rolesPath, err := InstanceRolesPath(root, DefaultInstanceID)
	if err != nil {
		t.Fatalf("roles path: %v", err)
	}
	if _, err := os.Stat(rolesPath); err != nil {
		t.Fatalf("expected roles.json: %v", err)
	}
	skills, err := LoadInstanceSkills(root, DefaultInstanceID)
	if err != nil {
		t.Fatalf("load skills: %v", err)
	}
	if !reflect.DeepEqual(skills.Activated, []string{"repo-research"}) {
		t.Fatalf("unexpected skills: %#v", skills)
	}
	activeID, err := LoadActiveInstanceID(root)
	if err != nil {
		t.Fatalf("load active instance id: %v", err)
	}
	if activeID != DefaultInstanceID {
		t.Fatalf("expected active instance default, got %q", activeID)
	}
}

func TestResolveEffectiveRuntimeUsesInstanceAndAgentState(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Model.Provider = "zai"
	cfg.Model.Name = "GLM-4.7"
	cfg.Output.ThinkingMode = config.ThinkingModeOnError
	if err := config.Save(filepath.Join(root, ".openclawssy", "config.json"), cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	instance := DefaultInstanceManifest("lab")
	instance.Runtime.DefaultAgentID = "orchestrator"
	instance.Workspace.Root = "workspace/instances/lab"
	if err := SaveInstanceManifest(root, instance); err != nil {
		t.Fatalf("save instance: %v", err)
	}
	agent := DefaultAgentManifest("coder")
	agent.Model.Provider = "openai"
	agent.Model.Name = "gpt-4.1-mini"
	agent.Restrictions.AllowedTools = []string{"fs.read", "code.search"}
	agent.Workspace.OverlayRoot = "agents/coder"
	agent.Behavior.DefaultThinkingMode = config.ThinkingModeAlways
	if err := SaveAgentManifest(root, "lab", agent); err != nil {
		t.Fatalf("save agent: %v", err)
	}
	store, err := promptstack.NewVersionStore(filepath.Join(root, ".openclawssy"), "lab")
	if err != nil {
		t.Fatalf("new prompt stack store: %v", err)
	}
	if _, err := store.UpdateLayer("coder", promptstack.LayerAgentIdentity, "You are coder"); err != nil {
		t.Fatalf("seed prompt stack: %v", err)
	}
	resolved, err := ResolveEffectiveRuntime(root, "lab", "coder")
	if err != nil {
		t.Fatalf("resolve effective runtime: %v", err)
	}
	if resolved.InstanceID != "lab" || resolved.AgentID != "coder" {
		t.Fatalf("unexpected runtime identity: %#v", resolved)
	}
	if resolved.Model.Provider != "openai" || resolved.Model.Name != "gpt-4.1-mini" {
		t.Fatalf("expected agent model override, got %#v", resolved.Model)
	}
	if resolved.AgentWorkspaceRoot != filepath.Join(root, "workspace/instances/lab", "agents/coder") {
		t.Fatalf("unexpected agent workspace root: %q", resolved.AgentWorkspaceRoot)
	}
	if resolved.ThinkingMode != config.ThinkingModeAlways {
		t.Fatalf("expected thinking mode override, got %q", resolved.ThinkingMode)
	}
	if resolved.PromptStackState.AgentIdentity.Content != "You are coder" {
		t.Fatalf("expected prompt stack content, got %#v", resolved.PromptStackState)
	}
}

func TestActivateInstanceAndLoadActiveManifest(t *testing.T) {
	root := t.TempDir()
	if err := SaveInstanceManifest(root, DefaultInstanceManifest("alpha")); err != nil {
		t.Fatalf("save alpha instance: %v", err)
	}
	if err := SaveInstanceManifest(root, DefaultInstanceManifest("beta")); err != nil {
		t.Fatalf("save beta instance: %v", err)
	}
	active, err := ActivateInstance(root, "beta")
	if err != nil {
		t.Fatalf("activate beta instance: %v", err)
	}
	if active.InstanceID != "beta" {
		t.Fatalf("expected activated beta manifest, got %#v", active)
	}
	activeID, err := LoadActiveInstanceID(root)
	if err != nil {
		t.Fatalf("load active instance id: %v", err)
	}
	if activeID != "beta" {
		t.Fatalf("expected active instance beta, got %q", activeID)
	}
	loadedActive, err := LoadActiveInstanceManifest(root)
	if err != nil {
		t.Fatalf("load active instance manifest: %v", err)
	}
	if loadedActive.InstanceID != "beta" {
		t.Fatalf("expected active manifest beta, got %#v", loadedActive)
	}
}

func TestActivateInstanceRequiresExistingManifest(t *testing.T) {
	root := t.TempDir()
	if _, err := ActivateInstance(root, "missing"); err == nil {
		t.Fatal("expected activate missing instance to fail")
	}
}
