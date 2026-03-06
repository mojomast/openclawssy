package agentdocs

import (
	"strings"
	"testing"
)

func TestDefaultScaffoldMentionsPromptToolsForControlDocs(t *testing.T) {
	files := ScaffoldFiles()
	toolsDoc := files["TOOLS.md"]
	if !strings.Contains(toolsDoc, "agent.prompt.read") || !strings.Contains(toolsDoc, "agent.prompt.update") {
		t.Fatalf("expected default scaffold tools doc to mention prompt tools, got %q", toolsDoc)
	}
	devplan := files["DEVPLAN.md"]
	if !strings.Contains(devplan, "HANDOFF.md via agent.prompt.update") {
		t.Fatalf("expected default scaffold devplan to route HANDOFF through prompt update, got %q", devplan)
	}
}

func TestClawDefuckifierScaffoldAvoidsImpossibleHandoffWrites(t *testing.T) {
	files := ScaffoldFilesForAgent("clawdefuckifier")
	rulesDoc := files["RULES.md"]
	if strings.Contains(rulesDoc, "write proposed prompt patches into HANDOFF.md") {
		t.Fatalf("expected clawdefuckifier rules to avoid impossible HANDOFF fallback, got %q", rulesDoc)
	}
	if !strings.Contains(rulesDoc, "Only use HANDOFF.md through agent.prompt.update") {
		t.Fatalf("expected clawdefuckifier rules to route HANDOFF through agent.prompt.update, got %q", rulesDoc)
	}
	toolsDoc := files["TOOLS.md"]
	if !strings.Contains(toolsDoc, "Use agent.prompt.read for SOUL.md") {
		t.Fatalf("expected clawdefuckifier tools doc to mention prompt docs, got %q", toolsDoc)
	}
}
