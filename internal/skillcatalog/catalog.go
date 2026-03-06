package skillcatalog

import "strings"

var builtInSkills = map[string]string{
	"playwrite": `---
name: playwrite
required_secrets: []
---

# Playwrite Skill

Use this skill for browser automation and UI verification tasks.

Workflow:
1. Ensure browser dependencies are installed for your environment.
2. Use Playwright scripts for deterministic UI checks.
3. Prefer isolated test selectors and explicit waits.

When this skill is activated for an agent, load it with skill.read and then execute the generated script with shell.exec if that capability is available.
`,
	"clawdefuckifier": `---
name: clawdefuckifier
required_secrets: []
---

# ClawDefuckifier Skill

Use this skill when an OpenClawssy mission keeps stalling on harness friction: agent creation dead-ends, repeated reads, timeout chains, fragmented handoffs, or prompt rules that are too brittle to finish the job.

## Reality Check

- Use only real OpenClawssy tools. Do not invent helper functions.
- There is no exact remaining-create-slots API. Use agent.list to inspect the current roster and treat agent creation as expensive and optional.
- There is no automatic cross-run file cache. If context must survive retries, checkpoint it yourself.
- Long tasks succeed by being split into smaller phases with resume points.
- When this agent is scaffolded as clawdefuckifier, self-improvement and self-runs are enabled so it can iterate on its own prompt/docs safely.

## Primary Loop

1. Load evidence first with run.list, run.get, metrics.get, agent.prompt.read, and skill.read.
2. State one failure hypothesis in a single sentence.
3. Save a checkpoint before risky work using HANDOFF.md, a workspace note, decision.log, or memory.write.
4. Prefer a bounded self-run before spawning new agents.
5. Apply the smallest fix that directly addresses the current failure.
6. Verify with the narrowest relevant check.
7. If the issue remains, harvest new evidence, save the rejected hypothesis, change strategy, and repeat. Never rerun the exact same failed plan unchanged.

## Self-Run Pattern

Use agent.run on your own agent id for diagnose, patch, and verify phases.

~~~json
{"tool_name":"agent.run","arguments":{"agent_id":"<same-agent-id>","message":"Diagnose the current failure. Return findings, likely root cause, and the next smallest repair step.","task_id":"cdf-diagnose-1","allowed_tools":["run.list","run.get","metrics.get","agent.prompt.read","fs.read","fs.list","code.search","memory.search","skill.read"],"max_tool_iterations":8,"timeout_ms":45000}}
~~~

For follow-up loops, change task_id, hypothesis, or allowed_tools so the retry is materially different.

Those task ids are surfaced in Agent Monitor, so make them descriptive.

## Coordination Rules

### Reuse Before Create

- Call agent.list before any agent.create.
- Reuse an existing specialist whenever one already fits the role.
- Create at most one new specialist only when the role is clear, bounded, and impossible to cover with the current agent set.

### Checkpoint Everything Important

At the end of each phase, save:
- what completed
- what failed
- evidence gathered
- the next exact step
- the resume point after timeout

Good checkpoint targets:
- HANDOFF.md for agent-level state
- workspace/clawdefuckifier/<topic>/phase-<n>.md for detailed mission state
- memory.write or decision.log for durable summaries

When the agent id starts with clawdefuckifier, the runtime also writes automatic run checkpoints under workspace/clawdefuckifier/<agent-id>/runs/ and mirrors the newest one to LATEST.md.

### Prompt Hardening

- Use agent.prompt.read on SOUL.md, RULES.md, and TOOLS.md.
- Use agent.prompt.suggest with focus terms like timeouts, retries, quota awareness, checkpointing, or tool recovery.
- Use agent.prompt.update only if self-improvement is enabled for the current agent.
- If prompt updates are not allowed, write the proposed patch into HANDOFF.md and continue operational fixes.

## Research And Tome Pattern

When a task produces many scattered notes, create a single synthesis file and keep appending structured sections instead of spraying many tiny outputs.

Recommended section order:
1. Goal
2. Constraints observed
3. Evidence and sources
4. Findings
5. Open questions
6. Resume point
7. Embedding-ready chunks with stable headings

## Stop Conditions

- Stop when the defect is reproduced, patched, and verified.
- Stop when the remaining blocker is external and described precisely.
- Stop when the next loop would just repeat the same failed plan without new evidence.
`,
}

func Catalog() map[string]string {
	out := make(map[string]string, len(builtInSkills))
	for name, body := range builtInSkills {
		out[name] = body
	}
	return out
}

func Body(name string) (string, bool) {
	body, ok := builtInSkills[strings.ToLower(strings.TrimSpace(name))]
	return body, ok
}
