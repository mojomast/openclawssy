package tools

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	defaultSkillsRoot     = "skills"
	defaultSkillsListSize = 64
	maxSkillFileBytes     = 128 * 1024
)

var skillFileExtensions = map[string]bool{
	".md":   true,
	".txt":  true,
	".json": true,
	".yml":  true,
	".yaml": true,
}

var secretTokenPattern = regexp.MustCompile(`\b[A-Z][A-Z0-9_]{2,}_(?:API_KEY|TOKEN|SECRET|KEY)\b`)
var providerSecretPattern = regexp.MustCompile(`provider/[a-z0-9._-]+/api_key`)
var quotedSecretKeyPattern = regexp.MustCompile(`(?i)"key"\s*:\s*"([^"]+)"`)
var apiKeyTokenPattern = regexp.MustCompile(`^([A-Z][A-Z0-9_]*)_API_KEY$`)

type workspaceSkill struct {
	Name            string
	Path            string
	Content         string
	Truncated       bool
	RequiredSecrets []string
}

func registerSkillTools(reg *Registry, configuredPath string) error {
	if err := reg.Register(ToolSpec{
		Name:        "skill.list",
		Description: "List workspace skills rooted under skills/ and report required secrets",
		ArgTypes: map[string]ArgType{
			"root":  ArgTypeString,
			"limit": ArgTypeNumber,
		},
	}, skillList(configuredPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "skill.read",
		Description: "Read a workspace skill by name/path with required secret diagnostics",
		ArgTypes: map[string]ArgType{
			"name": ArgTypeString,
			"path": ArgTypeString,
			"root": ArgTypeString,
		},
	}, skillRead(configuredPath)); err != nil {
		return err
	}
	return nil
}

func skillList(configuredPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		root := strings.TrimSpace(getStringOrDefault(req.Args, "root", defaultSkillsRoot))
		limit := getIntArg(req.Args, "limit", defaultSkillsListSize)
		if limit <= 0 {
			limit = defaultSkillsListSize
		}

		skills, err := discoverWorkspaceSkills(req, root, false, limit)
		if err != nil {
			return nil, err
		}

		secretFound := map[string]bool{}
		secretStoreErr := ""
		store, err := openSecretsStore(req.Workspace, configuredPath)
		if err != nil {
			secretStoreErr = err.Error()
		} else {
			secretFound, _ = readSecretPresenceMap(store)
		}

		items := make([]map[string]any, 0, len(skills))
		for _, skill := range skills {
			missing := missingSecrets(skill.RequiredSecrets, secretFound)
			required := standardizeRequiredSecrets(skill.RequiredSecrets)
			items = append(items, map[string]any{
				"name":             skill.Name,
				"path":             skill.Path,
				"required_secrets": required,
				"missing_secrets":  missing,
				"ready":            len(missing) == 0,
			})
		}

		res := map[string]any{
			"root":   root,
			"count":  len(items),
			"skills": items,
		}
		if secretStoreErr != "" {
			res["secret_store_error"] = secretStoreErr
		}
		if len(items) == 0 {
			res["summary"] = fmt.Sprintf("no skills found under %s", root)
		}
		return res, nil
	}
}

func skillRead(configuredPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		root := strings.TrimSpace(getStringOrDefault(req.Args, "root", defaultSkillsRoot))
		name := strings.TrimSpace(getStringOrDefault(req.Args, "name", ""))
		skillPath := strings.TrimSpace(getStringOrDefault(req.Args, "path", ""))
		if name == "" && skillPath == "" {
			return nil, fmt.Errorf("name or path is required")
		}

		skills, err := discoverWorkspaceSkills(req, root, true, defaultSkillsListSize*4)
		if err != nil {
			return nil, err
		}
		if len(skills) == 0 {
			return nil, fmt.Errorf("no skills found under %s", root)
		}

		selected, err := selectWorkspaceSkill(skills, name, skillPath)
		if err != nil {
			return nil, err
		}

		required := standardizeRequiredSecrets(selected.RequiredSecrets)
		missing := []string{}
		if len(required) > 0 {
			store, err := openSecretsStore(req.Workspace, configuredPath)
			if err != nil {
				return nil, fmt.Errorf("failed to open secret store: %w", err)
			}
			secretFound, err := readSecretPresenceMap(store)
			if err != nil {
				return nil, err
			}
			missing = missingSecrets(selected.RequiredSecrets, secretFound)
			if len(missing) > 0 {
				return nil, fmt.Errorf("missing required secrets for skill %s: %s (set via /api/admin/secrets or secrets.set)", selected.Name, strings.Join(missing, ", "))
			}
		}

		res := map[string]any{
			"name":             selected.Name,
			"path":             selected.Path,
			"content":          selected.Content,
			"truncated":        selected.Truncated,
			"required_secrets": required,
			"missing_secrets":  []string{},
			"ready":            true,
		}
		if selected.Truncated {
			res["summary"] = fmt.Sprintf("loaded skill %s (truncated to %d bytes)", selected.Name, maxSkillFileBytes)
		} else {
			res["summary"] = fmt.Sprintf("loaded skill %s", selected.Name)
		}
		return res, nil
	}
}

func discoverWorkspaceSkills(req Request, root string, includeContent bool, limit int) ([]workspaceSkill, error) {
	if strings.TrimSpace(req.Workspace) == "" {
		return nil, fmt.Errorf("workspace is required")
	}
	if limit <= 0 {
		limit = defaultSkillsListSize
	}

	rootRel, rootAbs, err := normalizeSkillsRoot(req.Workspace, root)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(rootAbs); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []workspaceSkill{}, nil
		}
		return nil, err
	}

	if req.Policy != nil {
		resolved, err := req.Policy.ResolveReadPath(req.Workspace, rootRel)
		if err != nil {
			return nil, err
		}
		rootAbs = resolved
	}

	skills := make([]workspaceSkill, 0, limit)
	errStop := errors.New("skills.stop")
	walkErr := filepath.WalkDir(rootAbs, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			if path != rootAbs {
				name := strings.ToLower(strings.TrimSpace(d.Name()))
				if strings.HasPrefix(name, ".") || name == "node_modules" || name == ".git" {
					return filepath.SkipDir
				}
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(d.Name()))
		if !skillFileExtensions[ext] {
			return nil
		}

		relPath, err := filepath.Rel(req.Workspace, path)
		if err != nil {
			return nil
		}
		relPath = filepath.ToSlash(relPath)

		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		truncated := false
		if len(raw) > maxSkillFileBytes {
			raw = raw[:maxSkillFileBytes]
			truncated = true
		}
		content := string(raw)
		skill := workspaceSkill{
			Name:            inferSkillName(filepath.Base(path)),
			Path:            relPath,
			Truncated:       truncated,
			RequiredSecrets: detectRequiredSecrets(content),
		}
		if includeContent {
			skill.Content = content
		}
		skills = append(skills, skill)
		if len(skills) >= limit {
			return errStop
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, errStop) {
		return nil, walkErr
	}

	sort.Slice(skills, func(i, j int) bool {
		if strings.EqualFold(skills[i].Name, skills[j].Name) {
			return skills[i].Path < skills[j].Path
		}
		return strings.ToLower(skills[i].Name) < strings.ToLower(skills[j].Name)
	})

	return skills, nil
}

func normalizeSkillsRoot(workspace, root string) (string, string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = defaultSkillsRoot
	}
	root = filepath.Clean(root)
	if root == "." {
		return "", "", fmt.Errorf("root must be a relative skills directory")
	}
	if filepath.IsAbs(root) || strings.HasPrefix(root, "..") || strings.Contains(root, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("root must stay within workspace")
	}

	rootAbs := filepath.Join(workspace, root)
	within, err := isWithinWorkspace(workspace, rootAbs)
	if err != nil {
		return "", "", err
	}
	if !within {
		return "", "", fmt.Errorf("root must stay within workspace")
	}
	return root, rootAbs, nil
}

func selectWorkspaceSkill(skills []workspaceSkill, name, path string) (workspaceSkill, error) {
	if path != "" {
		normalized := filepath.ToSlash(filepath.Clean(path))
		for _, skill := range skills {
			if filepath.ToSlash(filepath.Clean(skill.Path)) == normalized {
				return skill, nil
			}
		}
		return workspaceSkill{}, fmt.Errorf("skill path not found: %s", path)
	}

	needle := strings.ToLower(strings.TrimSpace(name))
	if needle == "" {
		return workspaceSkill{}, fmt.Errorf("skill name is required")
	}

	matches := make([]workspaceSkill, 0, 2)
	for _, skill := range skills {
		if strings.ToLower(strings.TrimSpace(skill.Name)) == needle {
			matches = append(matches, skill)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		paths := make([]string, 0, len(matches))
		for _, item := range matches {
			paths = append(paths, item.Path)
		}
		sort.Strings(paths)
		return workspaceSkill{}, fmt.Errorf("multiple skills named %s found; use path (%s)", name, strings.Join(paths, ", "))
	}

	available := make([]string, 0, len(skills))
	for _, skill := range skills {
		available = append(available, skill.Name)
	}
	sort.Strings(available)
	return workspaceSkill{}, fmt.Errorf("skill %s not found (available: %s)", name, strings.Join(available, ", "))
}

func detectRequiredSecrets(content string) []string {
	if strings.TrimSpace(content) == "" {
		return nil
	}

	seen := map[string]struct{}{}
	add := func(key string) {
		key = strings.TrimSpace(key)
		if key == "" {
			return
		}
		seen[key] = struct{}{}
	}

	for _, match := range secretTokenPattern.FindAllString(content, -1) {
		add(match)
	}
	for _, match := range providerSecretPattern.FindAllString(strings.ToLower(content), -1) {
		add(match)
	}

	quoted := quotedSecretKeyPattern.FindAllStringSubmatch(content, -1)
	for _, pair := range quoted {
		if len(pair) < 2 {
			continue
		}
		key := strings.TrimSpace(pair[1])
		if secretTokenPattern.MatchString(key) || providerSecretPattern.MatchString(strings.ToLower(key)) {
			add(key)
		}
	}

	out := make([]string, 0, len(seen))
	for key := range seen {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func readSecretPresenceMap(store interface {
	ListKeys() ([]string, error)
}) (map[string]bool, error) {
	keys, err := store.ListKeys()
	if err != nil {
		return nil, err
	}
	found := make(map[string]bool, len(keys))
	for _, key := range keys {
		found[strings.TrimSpace(key)] = true
	}
	return found, nil
}

func missingSecrets(required []string, found map[string]bool) []string {
	if len(required) == 0 {
		return []string{}
	}
	missingSet := map[string]struct{}{}
	for _, key := range required {
		candidates := secretKeyCandidates(key)
		if len(candidates) == 0 {
			candidates = []string{strings.TrimSpace(key)}
		}
		present := false
		for _, candidate := range candidates {
			if found[candidate] {
				present = true
				break
			}
		}
		if present {
			continue
		}
		missingSet[canonicalSecretKey(key)] = struct{}{}
	}
	missing := make([]string, 0, len(missingSet))
	for key := range missingSet {
		missing = append(missing, key)
	}
	sort.Strings(missing)
	return missing
}

func standardizeRequiredSecrets(required []string) []string {
	if len(required) == 0 {
		return []string{}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(required))
	for _, key := range required {
		canonical := canonicalSecretKey(key)
		if canonical == "" {
			continue
		}
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		out = append(out, canonical)
	}
	sort.Strings(out)
	return out
}

func canonicalSecretKey(key string) string {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	if providerSecretPattern.MatchString(lower) {
		return lower
	}
	match := apiKeyTokenPattern.FindStringSubmatch(strings.ToUpper(trimmed))
	if len(match) == 2 {
		provider := strings.ToLower(strings.ReplaceAll(match[1], "_", "-"))
		return fmt.Sprintf("provider/%s/api_key", provider)
	}
	return trimmed
}

func secretKeyCandidates(key string) []string {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, 2)
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	add(trimmed)
	canonical := canonicalSecretKey(trimmed)
	add(canonical)
	if providerSecretPattern.MatchString(strings.ToLower(trimmed)) {
		parts := strings.Split(strings.ToLower(trimmed), "/")
		if len(parts) >= 3 {
			name := strings.ToUpper(strings.ReplaceAll(parts[1], "-", "_"))
			add(name + "_API_KEY")
		}
	}
	if len(out) > 1 {
		sort.Strings(out)
	}
	return out
}

func inferSkillName(filename string) string {
	base := strings.TrimSpace(filename)
	for {
		ext := filepath.Ext(base)
		if ext == "" {
			break
		}
		base = strings.TrimSuffix(base, ext)
	}
	return strings.TrimSpace(base)
}

func getStringOrDefault(args map[string]any, key, fallback string) string {
	if args == nil {
		return fallback
	}
	v, ok := args[key]
	if !ok {
		return fallback
	}
	s, ok := v.(string)
	if !ok {
		return fallback
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	return s
}
