package toolparse

import "testing"

func FuzzParseStrictNoPanic(f *testing.F) {
	seeds := []string{
		"plain text",
		"```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```",
		"```tool\n{\"tool_name\":\"tool.result\",\"arguments\":{}}\n```",
		"```json\n{not-json}\n```",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		_ = ParseStrict(input, []string{"fs.list", "fs.read", "fs.write", "fs.append", "fs.delete", "fs.move", "config.get", "config.set", "secrets.set", "secrets.list", "skill.list", "skill.read", "scheduler.list", "scheduler.add", "scheduler.remove", "scheduler.pause", "scheduler.resume", "session.list", "session.close", "agent.list", "agent.create", "agent.switch", "agent.profile.get", "agent.profile.set", "agent.message.send", "agent.message.inbox", "agent.run", "agent.prompt.read", "agent.prompt.update", "agent.prompt.suggest", "agent.identity.set", "policy.list", "policy.grant", "policy.revoke", "run.list", "run.get", "run.cancel", "metrics.get", "http.request", "shell.exec"}, 1)
	})
}
