package roles

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
)

type RouteSelection struct {
	RoleName   string       `json:"role"`
	Template   RoleTemplate `json:"template"`
	Confidence float64      `json:"confidence"`
	Rationale  string       `json:"rationale"`
}

type Router struct {
	templates map[string]RoleTemplate
}

type routeCategory struct {
	roleName      string
	keywords      []string
	structureCues []string
}

type routeScore struct {
	roleName      string
	score         float64
	hits          int
	totalTokens   int
	matched       []string
	structureCue  bool
	firstMatchPos int
}

var routeCategories = []routeCategory{
	{roleName: "scout", keywords: []string{"read", "search", "discover"}, structureCues: []string{"find", "inspect", "list", "explore"}},
	{roleName: "planner", keywords: []string{"plan", "decompose", "design"}, structureCues: []string{"steps", "phases", "roadmap", "strategy"}},
	{roleName: "implementer", keywords: []string{"write", "implement", "fix", "patch"}, structureCues: []string{"change", "modify", "update", "code", "file"}},
	{roleName: "verifier", keywords: []string{"test", "verify", "check", "validate"}, structureCues: []string{"assert", "coverage", "regression", "pass"}},
	{roleName: "reviewer", keywords: []string{"review", "critique", "audit"}, structureCues: []string{"risk", "security", "quality", "finding"}},
	{roleName: "operator", keywords: []string{"configure", "setup", "settings"}, structureCues: []string{"config", "policy", "environment", "runtime"}},
}

func NewRouter(templates []RoleTemplate) *Router {
	if len(templates) == 0 {
		templates = BuiltInTemplates()
	}

	resolved := make(map[string]RoleTemplate, len(templates))
	for _, template := range templates {
		normalizedName := normalizeRoleName(template.Name)
		if normalizedName == "" {
			continue
		}
		resolved[normalizedName] = cloneTemplate(template)
	}

	if len(resolved) == 0 {
		for _, template := range BuiltInTemplates() {
			resolved[template.Name] = cloneTemplate(template)
		}
	}

	return &Router{templates: resolved}
}

func (r *Router) Route(taskDescription string) RouteSelection {
	tokens := tokenize(taskDescription)
	if len(tokens) == 0 {
		return r.fallbackSelection("task description has no routable keywords")
	}

	scores := make([]routeScore, 0, len(routeCategories))
	for _, category := range routeCategories {
		score := scoreCategory(tokens, category)
		if score.hits == 0 {
			continue
		}
		scores = append(scores, score)
	}

	if len(scores) == 0 {
		return r.fallbackSelection("no role keywords matched; using implementer fallback")
	}

	sort.Slice(scores, func(i, j int) bool {
		if scores[i].score == scores[j].score {
			if scores[i].hits == scores[j].hits {
				return scores[i].roleName < scores[j].roleName
			}
			return scores[i].hits > scores[j].hits
		}
		return scores[i].score > scores[j].score
	})

	best := scores[0]
	if len(scores) > 1 {
		second := scores[1]
		if math.Abs(best.score-second.score) < 0.08 && best.hits == second.hits {
			return r.fallbackSelection(fmt.Sprintf("ambiguous match between %s and %s; using implementer fallback", best.roleName, second.roleName))
		}
	}

	template, ok := r.templateForRole(best.roleName)
	if !ok {
		return r.fallbackSelection(fmt.Sprintf("role template %q not found; using implementer fallback", best.roleName))
	}

	rationale := fmt.Sprintf(
		"matched role %q via keywords [%s] (%d/%d tokens, density %.2f)",
		best.roleName,
		strings.Join(best.matched, ", "),
		best.hits,
		best.totalTokens,
		float64(best.hits)/float64(best.totalTokens),
	)
	if best.structureCue {
		rationale += "; task structure cues reinforced this route"
	}

	return RouteSelection{
		RoleName:   template.Name,
		Template:   template,
		Confidence: clamp(best.score, 0.05, 0.99),
		Rationale:  rationale,
	}
}

func (r *Router) templateForRole(roleName string) (RoleTemplate, bool) {
	if r == nil {
		return RoleTemplate{}, false
	}
	template, ok := r.templates[normalizeRoleName(roleName)]
	if !ok {
		return RoleTemplate{}, false
	}
	return cloneTemplate(template), true
}

func (r *Router) fallbackSelection(reason string) RouteSelection {
	template, ok := r.templateForRole("implementer")
	if !ok {
		for _, candidate := range BuiltInTemplates() {
			if candidate.Name == "implementer" {
				template = candidate
				ok = true
				break
			}
		}
	}

	if !ok {
		template = RoleTemplate{Name: "implementer", AllowedTools: []string{"fs.read"}, MaxIterations: 1, TimeoutMS: 60000}
	}

	return RouteSelection{
		RoleName:   template.Name,
		Template:   template,
		Confidence: 0.2,
		Rationale:  strings.TrimSpace(reason) + "; fallback role is implementer",
	}
}

func scoreCategory(tokens []string, category routeCategory) routeScore {
	hits, matched, firstMatchPos := keywordStats(tokens, category.keywords)
	structureCue := hasStructureCue(tokens, category.structureCues)

	base := float64(hits) * 0.20
	density := float64(hits) / float64(len(tokens))
	densityBonus := density * 0.45
	leadBonus := 0.0
	if firstMatchPos >= 0 && firstMatchPos <= 2 {
		leadBonus = 0.18
	}
	structureBonus := 0.0
	if structureCue {
		structureBonus = 0.12
	}

	return routeScore{
		roleName:      category.roleName,
		score:         clamp(base+densityBonus+leadBonus+structureBonus, 0.05, 0.99),
		hits:          hits,
		totalTokens:   len(tokens),
		matched:       matched,
		structureCue:  structureCue,
		firstMatchPos: firstMatchPos,
	}
}

func keywordStats(tokens []string, keywords []string) (int, []string, int) {
	matchedSet := make(map[string]struct{}, len(keywords))
	matched := make([]string, 0, len(keywords))
	hits := 0
	firstMatch := -1

	for idx, token := range tokens {
		for _, keyword := range keywords {
			if tokenMatchesKeyword(token, keyword) {
				hits++
				if firstMatch == -1 {
					firstMatch = idx
				}
				if _, exists := matchedSet[keyword]; !exists {
					matchedSet[keyword] = struct{}{}
					matched = append(matched, keyword)
				}
				break
			}
		}
	}

	sort.Strings(matched)
	return hits, matched, firstMatch
}

func hasStructureCue(tokens []string, cues []string) bool {
	for _, token := range tokens {
		for _, cue := range cues {
			if tokenMatchesKeyword(token, cue) {
				return true
			}
		}
	}
	return false
}

func tokenMatchesKeyword(token, keyword string) bool {
	token = strings.TrimSpace(strings.ToLower(token))
	keyword = strings.TrimSpace(strings.ToLower(keyword))
	if token == "" || keyword == "" {
		return false
	}
	return token == keyword || strings.HasPrefix(token, keyword)
}

func tokenize(text string) []string {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return nil
	}

	parts := strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func clamp(value, minVal, maxVal float64) float64 {
	if value < minVal {
		return minVal
	}
	if value > maxVal {
		return maxVal
	}
	return value
}
