package promptstack

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const LintLayerAll = "all_layers"

type LintSeverity string

const (
	LintSeverityError   LintSeverity = "error"
	LintSeverityWarning LintSeverity = "warning"
	LintSeverityInfo    LintSeverity = "info"
)

type LintIssue struct {
	Severity     LintSeverity `json:"severity"`
	Description  string       `json:"description"`
	LayerID      string       `json:"layer_id"`
	SuggestedFix string       `json:"suggested_fix"`
}

type Linter struct {
	definedRoles map[string]struct{}
}

type lintIssueEmitter func(severity LintSeverity, description, layerID, suggestedFix string)

var (
	instructionPrefixPattern = regexp.MustCompile(`^\s*(?:[-*+]\s+|\d+[\.)]\s+)`)
	toolNamePattern          = regexp.MustCompile(`\b[a-z][a-z0-9_]*(?:\.[a-z][a-z0-9_]*)+\b`)
	delegateToPattern        = regexp.MustCompile(`\bdelegate(?:\s+\w+){0,3}\s+to\s+([a-z][a-z0-9_-]*)\b`)
	roleLabelPattern         = regexp.MustCompile(`\brole\s*[:=]\s*([a-z][a-z0-9_-]*)\b`)
	roleVerbPattern          = regexp.MustCompile(`\b(?:use|assign|select)\s+([a-z][a-z0-9_-]*)\s+role\b`)
	clearTriggerPattern      = regexp.MustCompile(`\b(when|if|after|once|unless|upon|whenever|as soon as)\b`)
	terminationPatternA      = regexp.MustCompile(`\b(stop|return|terminate|halt|end)\b[^\n\.]{0,80}\b(when|once|if|upon)\b`)
	terminationPatternB      = regexp.MustCompile(`\b(when|once|if|upon)\b[^\n\.]{0,80}\b(stop|return|terminate|halt|end)\b`)

	defaultDefinedRoles = []string{"scout", "planner", "implementer", "verifier", "reviewer", "operator"}

	vagueDelegationPhrases = []string{
		"maybe delegate",
		"consider delegating",
		"might delegate",
		"could delegate",
	}

	successCriteriaSignals = []string{
		"success criteria",
		"completion criteria",
		"acceptance criteria",
		"done when",
		"task is complete when",
		"consider task complete",
	}

	terminationSignals = []string{
		"stop and return",
		"stop when",
		"return when",
		"once complete, return",
		"if blocked, return",
		"terminate when",
		"halt when",
		"end when",
	}
)

func NewLinter(definedRoles []string) *Linter {
	return &Linter{definedRoles: normalizeDefinedRoles(definedRoles)}
}

func Lint(stack PromptStack, definedRoles []string) []LintIssue {
	return NewLinter(definedRoles).Lint(stack)
}

func (l *Linter) Lint(stack PromptStack) []LintIssue {
	active := l
	if active == nil {
		active = NewLinter(nil)
	}

	issues := make([]LintIssue, 0)
	seen := map[string]struct{}{}

	emit := func(severity LintSeverity, description, layerID, suggestedFix string) {
		description = strings.TrimSpace(description)
		layerID = strings.TrimSpace(layerID)
		suggestedFix = strings.TrimSpace(suggestedFix)
		if description == "" || layerID == "" {
			return
		}

		key := strings.Join([]string{string(severity), layerID, description}, "|")
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}

		issues = append(issues, LintIssue{
			Severity:     severity,
			Description:  description,
			LayerID:      layerID,
			SuggestedFix: suggestedFix,
		})
	}

	active.lintDuplicateInstructions(stack, emit)
	active.lintToolDirectiveConflicts(stack, emit)
	active.lintVagueDelegationLanguage(stack, emit)
	active.lintUndefinedRoleReferences(stack, emit)
	active.lintMissingSuccessCriteria(stack, emit)
	active.lintAbsentTerminationRules(stack, emit)

	return issues
}

func (l *Linter) lintDuplicateInstructions(stack PromptStack, emit lintIssueEmitter) {
	directiveFirstLayer := make(map[string]string)

	for _, layer := range stack.LayersInOrder() {
		for _, directive := range extractInstructionDirectives(layer.Content) {
			firstLayer, exists := directiveFirstLayer[directive]
			if !exists {
				directiveFirstLayer[directive] = layer.LayerID
				continue
			}
			if firstLayer == layer.LayerID {
				continue
			}

			emit(
				LintSeverityWarning,
				fmt.Sprintf("Duplicate instruction appears in %s and %s: %q.", firstLayer, layer.LayerID, directive),
				layer.LayerID,
				"Keep this directive in one canonical layer and reference it from other layers.",
			)
		}
	}
}

func (l *Linter) lintToolDirectiveConflicts(stack PromptStack, emit lintIssueEmitter) {
	type toolDirectiveState struct {
		allowLayers map[string]struct{}
		denyLayers  map[string]struct{}
	}

	states := map[string]*toolDirectiveState{}

	for _, layer := range stack.LayersInOrder() {
		for _, rawLine := range splitContentLines(layer.Content) {
			line := strings.ToLower(strings.TrimSpace(rawLine))
			if line == "" {
				continue
			}

			directiveType := classifyToolDirective(line)
			if directiveType == "" {
				continue
			}

			tools := toolNamePattern.FindAllString(line, -1)
			if len(tools) == 0 {
				continue
			}

			for _, tool := range tools {
				state, ok := states[tool]
				if !ok {
					state = &toolDirectiveState{allowLayers: map[string]struct{}{}, denyLayers: map[string]struct{}{}}
					states[tool] = state
				}

				if directiveType == "allow" {
					state.allowLayers[layer.LayerID] = struct{}{}
				} else {
					state.denyLayers[layer.LayerID] = struct{}{}
				}
			}
		}
	}

	tools := make([]string, 0, len(states))
	for tool := range states {
		tools = append(tools, tool)
	}
	sort.Strings(tools)

	for _, tool := range tools {
		state := states[tool]
		if len(state.allowLayers) == 0 || len(state.denyLayers) == 0 {
			continue
		}

		allowLayers := sortedLayerIDs(state.allowLayers)
		denyLayers := sortedLayerIDs(state.denyLayers)
		affectedLayer := denyLayers[0]
		if affectedLayer == "" && len(allowLayers) > 0 {
			affectedLayer = allowLayers[0]
		}

		emit(
			LintSeverityError,
			fmt.Sprintf("Conflicting tool-use directives for %q: allowed in %s but denied in %s.", tool, strings.Join(allowLayers, ", "), strings.Join(denyLayers, ", ")),
			affectedLayer,
			"Define a single authoritative allow/deny directive for this tool.",
		)
	}
}

func (l *Linter) lintVagueDelegationLanguage(stack PromptStack, emit lintIssueEmitter) {
	for _, layer := range stack.LayersInOrder() {
		for _, rawLine := range splitContentLines(layer.Content) {
			line := strings.ToLower(strings.TrimSpace(rawLine))
			if line == "" {
				continue
			}

			for _, phrase := range vagueDelegationPhrases {
				if !strings.Contains(line, phrase) {
					continue
				}
				if clearTriggerPattern.MatchString(line) {
					continue
				}

				emit(
					LintSeverityWarning,
					fmt.Sprintf("Vague delegation language in %s: %q lacks an explicit trigger.", layer.LayerID, strings.TrimSpace(rawLine)),
					layer.LayerID,
					"Specify when delegation should happen (for example, complexity threshold, file count, or failure condition).",
				)
				break
			}
		}
	}
}

func (l *Linter) lintUndefinedRoleReferences(stack PromptStack, emit lintIssueEmitter) {
	for _, layer := range stack.LayersInOrder() {
		for _, roleName := range extractRoleReferences(layer.Content) {
			if _, ok := l.definedRoles[roleName]; ok {
				continue
			}

			emit(
				LintSeverityError,
				fmt.Sprintf("Undefined role reference %q does not match any defined role template.", roleName),
				layer.LayerID,
				"Use an existing role template name or define the missing role before referencing it.",
			)
		}
	}
}

func (l *Linter) lintMissingSuccessCriteria(stack PromptStack, emit lintIssueEmitter) {
	assembled := strings.ToLower(Assemble(stack))
	if hasAnySignal(assembled, successCriteriaSignals) {
		return
	}

	emit(
		LintSeverityWarning,
		"Missing success criteria: prompt stack does not define how completion is measured.",
		LintLayerAll,
		"Add explicit success criteria (for example, \"task is complete when all requested checks pass\").",
	)
}

func (l *Linter) lintAbsentTerminationRules(stack PromptStack, emit lintIssueEmitter) {
	assembled := strings.ToLower(Assemble(stack))
	if hasAnySignal(assembled, terminationSignals) || terminationPatternA.MatchString(assembled) || terminationPatternB.MatchString(assembled) {
		return
	}

	emit(
		LintSeverityWarning,
		"Absent termination rules: prompt stack does not define when to stop or return control.",
		LintLayerAll,
		"Add explicit stop/return conditions (for example, \"stop and return when completion criteria are satisfied\").",
	)
}

func normalizeDefinedRoles(definedRoles []string) map[string]struct{} {
	normalized := make(map[string]struct{}, len(definedRoles))
	for _, roleName := range definedRoles {
		roleName = normalizeFreeText(roleName)
		if roleName == "" {
			continue
		}
		normalized[roleName] = struct{}{}
	}

	if len(normalized) > 0 {
		return normalized
	}

	defaults := make(map[string]struct{}, len(defaultDefinedRoles))
	for _, roleName := range defaultDefinedRoles {
		defaults[roleName] = struct{}{}
	}
	return defaults
}

func extractInstructionDirectives(content string) []string {
	out := make([]string, 0)
	seen := map[string]struct{}{}

	for _, rawLine := range splitContentLines(content) {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ">") {
			continue
		}

		line = instructionPrefixPattern.ReplaceAllString(line, "")
		normalized := normalizeFreeText(line)
		if normalized == "" || len(normalized) < 12 || strings.HasSuffix(normalized, ":") {
			continue
		}

		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}

	return out
}

func classifyToolDirective(line string) string {
	denySignals := []string{"deny", "disallow", "forbid", "blocked", "block", "never use"}
	for _, signal := range denySignals {
		if strings.Contains(line, signal) {
			return "deny"
		}
	}

	allowSignals := []string{"allow", "allowed", "permit", "approved"}
	for _, signal := range allowSignals {
		if strings.Contains(line, signal) {
			return "allow"
		}
	}

	return ""
}

func extractRoleReferences(content string) []string {
	normalized := strings.ToLower(content)
	roles := map[string]struct{}{}

	for _, match := range delegateToPattern.FindAllStringSubmatch(normalized, -1) {
		if len(match) < 2 {
			continue
		}
		roles[match[1]] = struct{}{}
	}
	for _, match := range roleLabelPattern.FindAllStringSubmatch(normalized, -1) {
		if len(match) < 2 {
			continue
		}
		roles[match[1]] = struct{}{}
	}
	for _, match := range roleVerbPattern.FindAllStringSubmatch(normalized, -1) {
		if len(match) < 2 {
			continue
		}
		roles[match[1]] = struct{}{}
	}

	out := make([]string, 0, len(roles))
	for role := range roles {
		out = append(out, role)
	}
	sort.Strings(out)
	return out
}

func sortedLayerIDs(in map[string]struct{}) []string {
	out := make([]string, 0, len(in))
	for layerID := range in {
		out = append(out, layerID)
	}
	sort.Strings(out)
	return out
}

func hasAnySignal(content string, signals []string) bool {
	for _, signal := range signals {
		if strings.Contains(content, signal) {
			return true
		}
	}
	return false
}

func normalizeFreeText(text string) string {
	text = strings.TrimSpace(strings.ToLower(text))
	text = strings.Trim(text, "`*_\"'()[]{}")
	text = strings.TrimRight(text, ".,;!?")
	if text == "" {
		return ""
	}
	return strings.Join(strings.Fields(text), " ")
}
