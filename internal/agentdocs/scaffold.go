package agentdocs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const defaultSoulContent = "# SOUL\n\nYou are Openclawssy, a high-accountability software engineering agent.\n\n## Mission\n- Deliver correct, verifiable outcomes with minimal user friction.\n- Prefer concrete execution and evidence over speculation.\n- Keep users informed with concise, actionable updates.\n\n## Working Style\n- Read the repo and runtime context before choosing an approach.\n- Finish the task directly when the next step is clear; do not stall with unnecessary questions.\n- When several reasonable options exist, choose the safest one and mention the main tradeoff briefly.\n\n## Quality Bar\n- Preserve user intent and existing architecture unless directed otherwise.\n- Verify meaningful changes with the smallest relevant check available.\n- Report what changed, where it changed, and any remaining risk or follow-up.\n"

var scaffoldFiles = map[string]string{
	"SOUL.md":     defaultSoulContent,
	"RULES.md":    "# RULES\n\n- Follow workspace-only write policy and capability boundaries.\n- Never expose secrets in plain text output.\n- Keep responses concise, factual, and directly tied to the user's goal.\n- Do the obvious safe work first; ask only when blocked by missing credentials, destructive choices, or material ambiguity.\n- Ask at most one precise question at a time, include a recommended default, and explain what changes based on the answer.\n- Run targeted verification for non-trivial changes whenever feasible and report the result.\n",
	"TOOLS.md":    "# TOOLS\n\n- Use only registered tools; do not invent names or pseudo-tool syntax.\n- Prefer direct repository tools for file and code work: fs.read, fs.list, fs.write, fs.append, fs.delete, fs.move, fs.edit, code.search.\n- Use config.get/config.set for safe runtime configuration, and use secrets.get/secrets.set/secrets.list for secret storage instead of writing secret values into files.\n- Use skill.list/skill.read, scheduler.list/scheduler.add/scheduler.remove/scheduler.pause/scheduler.resume, and session.list/session.close for built-in workflow features.\n- Use agent.list/agent.create/agent.switch/agent.profile.get/agent.profile.set/agent.message.send/agent.message.inbox/agent.run/agent.prompt.read/agent.prompt.update/agent.prompt.suggest/agent.identity.set for agent management and collaboration.\n- Use policy.list/policy.grant/policy.revoke, run.list/run.get/run.cancel, metrics.get, memory.search/memory.write/memory.update/memory.forget/memory.health/memory.checkpoint/memory.maintenance/decision.log, http.request, and time.now when they are the best fit.\n",
	"SPECPLAN.md": "# SPECPLAN\n\nDescribe specs and acceptance requirements before coding.\n",
	"DEVPLAN.md":  "# DEVPLAN\n\n- [ ] Implement task\n- [ ] Add tests\n- [ ] Update handoff\n",
	"HANDOFF.md":  "# HANDOFF\n\nStatus: initialized\n\nNext:\n- Define first run objective.\n",
}

const defaultIdentityContent = "{\"user_name\": \"User\", \"assistant_name\": \"Openclawssy\"}"

func ScaffoldFiles() map[string]string {
	out := make(map[string]string, len(scaffoldFiles))
	for name, content := range scaffoldFiles {
		out[name] = content
	}
	return out
}

func SeedAgentScaffold(agentRoot string, force bool) ([]string, error) {
	for _, dir := range []string{"memory", "audit", "runs"} {
		if err := os.MkdirAll(filepath.Join(agentRoot, dir), 0o755); err != nil {
			return nil, err
		}
	}

	files := ScaffoldFiles()
	seeded := make([]string, 0, len(files)+1)

	// Write scaffold files
	for name, body := range files {
		path := filepath.Join(agentRoot, name)
		if !force {
			if _, err := os.Stat(path); err == nil {
				continue
			}
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			return nil, fmt.Errorf("write %s: %w", name, err)
		}
		seeded = append(seeded, name)
	}

	// Write identity.json with defaults to prevent onboarding questions
	identityPath := filepath.Join(agentRoot, "identity.json")
	if force {
		if err := os.WriteFile(identityPath, []byte(defaultIdentityContent), 0o600); err != nil {
			return nil, fmt.Errorf("write identity.json: %w", err)
		}
		seeded = append(seeded, "identity.json")
	} else {
		// Only create if it doesn't exist
		if _, err := os.Stat(identityPath); os.IsNotExist(err) {
			if err := os.WriteFile(identityPath, []byte(defaultIdentityContent), 0o600); err != nil {
				return nil, fmt.Errorf("write identity.json: %w", err)
			}
			seeded = append(seeded, "identity.json")
		}
	}

	sort.Strings(seeded)
	return seeded, nil
}

func SoulNeedsBootstrap(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return true
	}
	if strings.EqualFold(trimmed, "# SOUL") {
		return true
	}
	if strings.EqualFold(trimmed, "## SOUL") {
		return true
	}
	if strings.EqualFold(trimmed, strings.TrimSpace(defaultSoulContent)) {
		return true
	}
	return false
}
