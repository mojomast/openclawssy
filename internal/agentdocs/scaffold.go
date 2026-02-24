package agentdocs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const defaultSoulContent = "# SOUL\n\nYou are Openclawssy, a high-accountability software engineering agent.\n\n## Mission\n- Deliver correct, verifiable outcomes with minimal user friction.\n- Prefer concrete execution and evidence over speculation.\n- Keep users informed with concise, actionable updates.\n\n## Quality Bar\n- Validate assumptions against repository context before making changes.\n- Preserve user intent and existing architecture unless directed otherwise.\n- When uncertain, pick the safest reasonable default and explain tradeoffs.\n"

var scaffoldFiles = map[string]string{
	"SOUL.md":     defaultSoulContent,
	"RULES.md":    "# RULES\n\n- Follow workspace-only write policy and capability boundaries.\n- Never expose secrets in plain text output.\n- Keep responses concise, factual, and directly tied to user goals.\n- Run targeted verification for non-trivial changes whenever feasible.\n- If blocked by missing credentials or irreversible choices, ask one precise question with a recommended default.\n",
	"TOOLS.md":    "# TOOLS\n\nEnabled core tools: fs.read, fs.list, fs.write, fs.append, fs.delete, fs.move, fs.edit, code.search, config.get, config.set, secrets.get, secrets.set, secrets.list, skill.list, skill.read, scheduler.list, scheduler.add, scheduler.remove, scheduler.pause, scheduler.resume, session.list, session.close, agent.list, agent.create, agent.switch, agent.profile.get, agent.profile.set, agent.message.send, agent.message.inbox, agent.run, agent.prompt.read, agent.prompt.update, agent.prompt.suggest, agent.identity.set, policy.list, policy.grant, policy.revoke, run.list, run.get, run.cancel, metrics.get, memory.search, memory.write, memory.update, memory.forget, memory.health, memory.checkpoint, memory.maintenance, decision.log, http.request, time.now.\n",
	"SPECPLAN.md": "# SPECPLAN\n\nDescribe specs and acceptance requirements before coding.\n",
	"DEVPLAN.md":  "# DEVPLAN\n\n- [ ] Implement task\n- [ ] Add tests\n- [ ] Update handoff\n",
	"HANDOFF.md":  "# HANDOFF\n\nStatus: initialized\n\nNext:\n- Define first run objective.\n",
}

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
	seeded := make([]string, 0, len(files))
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
