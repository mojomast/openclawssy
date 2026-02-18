package tools

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultFSReadMaxBytes   = 256 * 1024
	defaultSearchMaxFiles   = 2000
	defaultSearchMaxFileBty = 512 * 1024
)

var workspaceControlPlaneFilenames = map[string]bool{
	"SOUL.MD":     true,
	"RULES.MD":    true,
	"TOOLS.MD":    true,
	"HANDOFF.MD":  true,
	"DEVPLAN.MD":  true,
	"SPECPLAN.MD": true,
}

type CoreOptions struct {
	EnableShellExec bool
	ConfigPath      string
	AgentsPath      string
	SchedulerPath   string
	ChatstorePath   string
	PolicyPath      string
	DefaultGrants   []string
	RunsPath        string
	RunTracker      RunCanceller
}

func RegisterCore(reg *Registry) error {
	return RegisterCoreWithOptions(reg, CoreOptions{EnableShellExec: true})
}

func RegisterCoreWithOptions(reg *Registry, opts CoreOptions) error {
	if err := reg.Register(ToolSpec{Name: "fs.read", Description: "Read text file", Required: []string{"path"}, ArgTypes: map[string]ArgType{"path": ArgTypeString, "max_bytes": ArgTypeNumber}}, fsRead); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{Name: "fs.list", Description: "List directory entries", Required: []string{"path"}, ArgTypes: map[string]ArgType{"path": ArgTypeString}}, fsList); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{Name: "fs.write", Description: "Write text file", Required: []string{"path", "content"}, ArgTypes: map[string]ArgType{"path": ArgTypeString, "content": ArgTypeString}}, fsWrite); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{Name: "fs.append", Description: "Append text to file", Required: []string{"path", "content"}, ArgTypes: map[string]ArgType{"path": ArgTypeString, "content": ArgTypeString}}, fsAppend); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{Name: "fs.delete", Description: "Delete file or directory", Required: []string{"path"}, ArgTypes: map[string]ArgType{"path": ArgTypeString, "recursive": ArgTypeBool, "force": ArgTypeBool}}, fsDelete); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{Name: "fs.move", Description: "Move or rename file or directory", Required: []string{"src", "dst"}, ArgTypes: map[string]ArgType{"src": ArgTypeString, "dst": ArgTypeString, "overwrite": ArgTypeBool}}, fsMove); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{Name: "fs.edit", Description: "Apply file edits (replace, line patch, unified diff)", Required: []string{"path"}, ArgTypes: map[string]ArgType{"path": ArgTypeString, "old": ArgTypeString, "new": ArgTypeString, "edits": ArgTypeArray, "patch": ArgTypeString}}, fsEdit); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{Name: "code.search", Description: "Search code with regex", Required: []string{"pattern"}, ArgTypes: map[string]ArgType{"pattern": ArgTypeString, "path": ArgTypeString, "max_files": ArgTypeNumber, "max_file_bytes": ArgTypeNumber}}, codeSearch); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{Name: "time.now", Description: "Get current time"}, timeNow); err != nil {
		return err
	}
	if err := registerConfigTools(reg, opts.ConfigPath); err != nil {
		return err
	}
	if err := registerSecretsTools(reg, opts.ConfigPath); err != nil {
		return err
	}
	if err := registerSkillTools(reg, opts.ConfigPath); err != nil {
		return err
	}
	if err := registerSchedulerTools(reg, opts.SchedulerPath); err != nil {
		return err
	}
	if err := registerSessionTools(reg, opts.ChatstorePath); err != nil {
		return err
	}
	if err := registerAgentTools(reg, opts.AgentsPath, opts.ConfigPath); err != nil {
		return err
	}
	if err := registerPolicyTools(reg, opts.PolicyPath, opts.DefaultGrants); err != nil {
		return err
	}
	if err := registerRunTools(reg, opts.RunsPath, opts.RunTracker); err != nil {
		return err
	}
	if err := registerMetricsTools(reg, opts.RunsPath); err != nil {
		return err
	}
	if err := registerNetworkTools(reg, opts.ConfigPath); err != nil {
		return err
	}
	if opts.EnableShellExec {
		if err := reg.Register(ToolSpec{Name: "shell.exec", Description: "Run command in sandbox", Required: []string{"command"}, ArgTypes: map[string]ArgType{"command": ArgTypeString, "args": ArgTypeArray, "timeout_ms": ArgTypeNumber}}, shellExec); err != nil {
			return err
		}
	}
	return nil
}

func fsRead(_ context.Context, req Request) (map[string]any, error) {
	path, err := getString(req.Args, "path")
	if err != nil {
		return nil, err
	}
	if req.Policy == nil {
		return nil, errors.New("policy is required")
	}
	resolved, err := req.Policy.ResolveReadPath(req.Workspace, path)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(resolved)
	if err != nil {
		return nil, err
	}
	maxBytes := getIntArg(req.Args, "max_bytes", defaultFSReadMaxBytes)
	if maxBytes <= 0 {
		maxBytes = defaultFSReadMaxBytes
	}
	if len(b) > maxBytes {
		b = b[:maxBytes]
	}
	return map[string]any{"path": path, "content": string(b), "truncated": len(b) == maxBytes}, nil
}

func fsList(_ context.Context, req Request) (map[string]any, error) {
	path, err := getString(req.Args, "path")
	if err != nil {
		return nil, err
	}
	if req.Policy == nil {
		return nil, errors.New("policy is required")
	}
	resolved, err := req.Policy.ResolveReadPath(req.Workspace, path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return map[string]any{"path": path, "entries": out}, nil
}

func fsWrite(_ context.Context, req Request) (map[string]any, error) {
	path, err := getString(req.Args, "path")
	if err != nil {
		return nil, err
	}
	content, err := getString(req.Args, "content")
	if err != nil {
		return nil, err
	}
	if req.Policy == nil {
		return nil, errors.New("policy is required")
	}
	resolved, err := req.Policy.ResolveWritePath(req.Workspace, path)
	if err != nil {
		return nil, err
	}
	if err := guardWorkspaceControlPlaneFilename(req.Workspace, resolved, req.AgentID); err != nil {
		return nil, err
	}
	if err := os.WriteFile(resolved, []byte(content), 0o600); err != nil {
		return nil, err
	}
	lines := 0
	if content != "" {
		lines = strings.Count(content, "\n") + 1
	}
	return map[string]any{
		"path":    path,
		"bytes":   len(content),
		"lines":   lines,
		"summary": fmt.Sprintf("wrote %d line(s) to %s", lines, path),
	}, nil
}

func fsAppend(_ context.Context, req Request) (map[string]any, error) {
	path, err := getString(req.Args, "path")
	if err != nil {
		return nil, err
	}
	content, err := getString(req.Args, "content")
	if err != nil {
		return nil, err
	}
	if req.Policy == nil {
		return nil, errors.New("policy is required")
	}
	resolved, err := req.Policy.ResolveWritePath(req.Workspace, path)
	if err != nil {
		return nil, err
	}
	if err := guardWorkspaceControlPlaneFilename(req.Workspace, resolved, req.AgentID); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(resolved, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	lines := 0
	if content != "" {
		lines = strings.Count(content, "\n") + 1
	}
	return map[string]any{
		"path":           path,
		"bytes_appended": len(content),
		"lines_appended": lines,
		"summary":        fmt.Sprintf("appended %d line(s) to %s", lines, path),
	}, nil
}

func fsDelete(_ context.Context, req Request) (map[string]any, error) {
	path, err := getString(req.Args, "path")
	if err != nil {
		return nil, err
	}
	if req.Policy == nil {
		return nil, errors.New("policy is required")
	}
	resolved, err := req.Policy.ResolveWritePath(req.Workspace, path)
	if err != nil {
		return nil, err
	}
	if err := guardWorkspaceControlPlaneFilename(req.Workspace, resolved, req.AgentID); err != nil {
		return nil, err
	}

	recursive := getBoolArg(req.Args, "recursive", false)
	force := getBoolArg(req.Args, "force", false)

	info, err := os.Lstat(resolved)
	if err != nil {
		if os.IsNotExist(err) && force {
			return map[string]any{
				"path":    path,
				"deleted": false,
				"skipped": "not_found",
				"summary": fmt.Sprintf("path not found, skipped delete: %s", path),
			}, nil
		}
		return nil, err
	}

	isDir := info.IsDir()
	if isDir && !recursive {
		return nil, errors.New("path is a directory; set recursive=true to delete directories")
	}

	if isDir {
		err = os.RemoveAll(resolved)
	} else {
		err = os.Remove(resolved)
	}
	if err != nil {
		if os.IsNotExist(err) && force {
			return map[string]any{
				"path":    path,
				"deleted": false,
				"skipped": "not_found",
				"summary": fmt.Sprintf("path not found, skipped delete: %s", path),
			}, nil
		}
		return nil, err
	}

	targetType := "file"
	if isDir {
		targetType = "dir"
	}
	return map[string]any{
		"path":      path,
		"deleted":   true,
		"type":      targetType,
		"recursive": recursive,
		"summary":   fmt.Sprintf("deleted %s", path),
	}, nil
}

func fsMove(_ context.Context, req Request) (map[string]any, error) {
	src, err := getString(req.Args, "src")
	if err != nil {
		return nil, err
	}
	dst, err := getString(req.Args, "dst")
	if err != nil {
		return nil, err
	}
	if req.Policy == nil {
		return nil, errors.New("policy is required")
	}
	srcResolved, err := req.Policy.ResolveWritePath(req.Workspace, src)
	if err != nil {
		return nil, err
	}
	dstResolved, err := req.Policy.ResolveWritePath(req.Workspace, dst)
	if err != nil {
		return nil, err
	}
	if err := guardWorkspaceControlPlaneFilename(req.Workspace, srcResolved, req.AgentID); err != nil {
		return nil, err
	}
	if err := guardWorkspaceControlPlaneFilename(req.Workspace, dstResolved, req.AgentID); err != nil {
		return nil, err
	}

	overwrite := getBoolArg(req.Args, "overwrite", false)

	srcInfo, err := os.Lstat(srcResolved)
	if err != nil {
		return nil, err
	}

	if srcResolved == dstResolved {
		return map[string]any{
			"src":       src,
			"dst":       dst,
			"moved":     true,
			"overwrite": overwrite,
			"summary":   fmt.Sprintf("moved %s to %s", src, dst),
		}, nil
	}

	if dstInfo, err := os.Lstat(dstResolved); err == nil {
		if !overwrite {
			return nil, fmt.Errorf("destination already exists: %s", dst)
		}
		if dstInfo.IsDir() {
			if err := os.RemoveAll(dstResolved); err != nil {
				return nil, err
			}
		} else if err := os.Remove(dstResolved); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	if err := os.Rename(srcResolved, dstResolved); err != nil {
		return nil, err
	}

	kind := "file"
	if srcInfo.IsDir() {
		kind = "dir"
	}
	return map[string]any{
		"src":       src,
		"dst":       dst,
		"moved":     true,
		"type":      kind,
		"overwrite": overwrite,
		"summary":   fmt.Sprintf("moved %s to %s", src, dst),
	}, nil
}

type lineEdit struct {
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	NewText   string `json:"newText"`
}

type unifiedDiffHunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Lines    []string
}

var unifiedDiffHeaderPattern = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(?: .*)?$`)

func fsEdit(_ context.Context, req Request) (map[string]any, error) {
	path, err := getString(req.Args, "path")
	if err != nil {
		return nil, err
	}
	if req.Policy == nil {
		return nil, errors.New("policy is required")
	}
	resolved, err := req.Policy.ResolveWritePath(req.Workspace, path)
	if err != nil {
		return nil, err
	}
	if err := guardWorkspaceControlPlaneFilename(req.Workspace, resolved, req.AgentID); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(resolved)
	if err != nil {
		return nil, err
	}

	_, hasOld := req.Args["old"]
	_, hasNew := req.Args["new"]
	_, hasEdits := req.Args["edits"]
	_, hasPatch := req.Args["patch"]
	hasReplaceMode := hasOld || hasNew

	modeCount := 0
	if hasReplaceMode {
		modeCount++
	}
	if hasEdits {
		modeCount++
	}
	if hasPatch {
		modeCount++
	}
	if modeCount == 0 {
		return nil, errors.New("fs.edit requires one edit mode: old/new, edits, or patch")
	}
	if modeCount > 1 {
		return nil, errors.New("fs.edit accepts exactly one edit mode: old/new, edits, or patch")
	}

	if hasReplaceMode {
		oldText, ok := req.Args["old"]
		if !ok {
			return nil, errors.New("missing argument: old")
		}
		oldS, ok := oldText.(string)
		if !ok {
			return nil, errors.New("old must be string")
		}
		newS, err := getString(req.Args, "new")
		if err != nil {
			return nil, err
		}
		orig := string(b)
		updated := strings.Replace(orig, oldS, newS, 1)
		if updated == orig {
			return nil, errors.New("edit pattern not found")
		}
		if err := os.WriteFile(resolved, []byte(updated), 0o600); err != nil {
			return nil, err
		}
		return map[string]any{"path": path, "updated": true, "mode": "replace_once"}, nil
	}

	if hasPatch {
		patch, err := getString(req.Args, "patch")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(patch) == "" {
			return nil, errors.New("patch must not be empty")
		}
		hunks, err := parseUnifiedDiff(patch)
		if err != nil {
			return nil, err
		}
		updated, applied, err := applyUnifiedDiff(string(b), hunks)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(resolved, []byte(updated), 0o600); err != nil {
			return nil, err
		}
		return map[string]any{"path": path, "updated": true, "applied_edits": applied, "mode": "unified_diff"}, nil
	}

	rawEdits, ok := req.Args["edits"]
	if !ok {
		return nil, errors.New("missing argument: edits")
	}
	edits, err := parseLineEdits(rawEdits)
	if err != nil {
		return nil, err
	}
	updated, applied, err := applyLineEdits(string(b), edits)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(resolved, []byte(updated), 0o600); err != nil {
		return nil, err
	}
	return map[string]any{"path": path, "updated": true, "applied_edits": applied, "mode": "line_patch"}, nil
}

func codeSearch(_ context.Context, req Request) (map[string]any, error) {
	pattern, err := getString(req.Args, "pattern")
	if err != nil {
		return nil, err
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}

	root := req.Workspace
	if custom, ok := req.Args["path"]; ok {
		customPath, ok := custom.(string)
		if !ok {
			return nil, fmt.Errorf("path must be string")
		}
		if req.Policy != nil {
			resolved, err := req.Policy.ResolveReadPath(req.Workspace, customPath)
			if err != nil {
				return nil, err
			}
			root = resolved
		} else {
			root = customPath
		}
	}

	files, err := listFiles(root, getIntArg(req.Args, "max_files", defaultSearchMaxFiles))
	if err != nil {
		return nil, err
	}

	maxFileBytes := getIntArg(req.Args, "max_file_bytes", defaultSearchMaxFileBty)
	if maxFileBytes <= 0 {
		maxFileBytes = defaultSearchMaxFileBty
	}

	results := make([]map[string]any, 0)
	for _, p := range files {
		info, err := os.Stat(p)
		if err != nil || info.Size() > int64(maxFileBytes) {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil || !isText(data) {
			continue
		}
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()
			if re.MatchString(line) {
				rel, _ := filepath.Rel(req.Workspace, p)
				results = append(results, map[string]any{"path": rel, "line": lineNo, "text": line})
			}
		}
	}

	return map[string]any{"matches": results}, nil
}

func timeNow(_ context.Context, _ Request) (map[string]any, error) {
	now := time.Now().UTC()
	return map[string]any{"rfc3339": now.Format(time.RFC3339), "unix": now.Unix()}, nil
}

func shellExec(ctx context.Context, req Request) (map[string]any, error) {
	if req.Shell == nil {
		return nil, errors.New("shell executor is not configured")
	}
	command, err := getString(req.Args, "command")
	if err != nil {
		return nil, err
	}
	args := []string{}
	if raw, ok := req.Args["args"]; ok {
		switch t := raw.(type) {
		case []any:
			for _, item := range t {
				args = append(args, fmt.Sprintf("%v", item))
			}
		case []string:
			args = append(args, t...)
		default:
			return nil, fmt.Errorf("args must be an array")
		}
	}
	invocation := strings.TrimSpace(strings.Join(append([]string{command}, args...), " "))
	allowed := false
	for _, prefix := range req.ShellAllowedCommands {
		if commandMatchesPrefix(invocation, prefix) {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, &ToolError{Code: ErrCodePolicyDenied, Tool: req.Tool, Message: "command is not allowed"}
	}

	// Apply timeout if specified
	timeoutMS := getIntArg(req.Args, "timeout_ms", 0)
	execCtx := ctx
	var cancel context.CancelFunc
	if timeoutMS > 0 {
		execCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
		defer cancel()
	}

	stdout, stderr, exitCode, execErr := req.Shell.Exec(execCtx, command, args)
	fallbackUsed := ""
	if execErr != nil && command == "bash" && isExecutableNotFound(execErr) {
		for _, candidate := range []string{"/bin/bash", "/usr/bin/bash", "sh"} {
			fbStdout, fbStderr, fbExitCode, fbErr := req.Shell.Exec(execCtx, candidate, args)
			if fbErr == nil || !isExecutableNotFound(fbErr) {
				stdout, stderr, exitCode, execErr = fbStdout, fbStderr, fbExitCode, fbErr
				fallbackUsed = candidate
				break
			}
		}
	}
	res := map[string]any{"stdout": stdout, "stderr": stderr, "exit_code": exitCode}
	if fallbackUsed != "" {
		res["shell_fallback"] = fallbackUsed
	}
	returnErr := execErr
	if isProcessExitStatusError(execErr) {
		returnErr = nil
	}
	if execErr != nil {
		res["error"] = execErr.Error()
	}
	if timeoutMS > 0 {
		res["timeout_ms"] = timeoutMS
	}
	return res, returnErr
}

func commandMatchesPrefix(invocation, prefix string) bool {
	invocation = strings.TrimSpace(invocation)
	prefix = strings.TrimSpace(prefix)
	if invocation == "" || prefix == "" {
		return false
	}
	if prefix == "*" {
		return true
	}
	if invocation == prefix {
		return true
	}
	return strings.HasPrefix(invocation, prefix+" ")
}

func isExecutableNotFound(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	if text == "" {
		return false
	}
	return strings.Contains(text, "executable file not found") || strings.Contains(text, "not found in $path") || strings.Contains(text, "no such file or directory")
}

func isProcessExitStatusError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	if text == "" {
		return false
	}
	return strings.HasPrefix(text, "exit status ")
}

func normalizeInt(v any) int {
	s := fmt.Sprintf("%v", v)
	n, _ := strconv.Atoi(s)
	return n
}

func listFiles(root string, maxFiles int) ([]string, error) {
	if maxFiles <= 0 {
		maxFiles = defaultSearchMaxFiles
	}
	stack := []string{root}
	files := make([]string, 0, 64)
	for len(stack) > 0 {
		n := len(stack) - 1
		dir := stack[n]
		stack = stack[:n]
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			name := e.Name()
			if name == ".git" || name == ".openclawssy" {
				continue
			}
			full := filepath.Join(dir, name)
			if e.IsDir() {
				stack = append(stack, full)
				continue
			}
			files = append(files, full)
			if len(files) >= maxFiles {
				return files, nil
			}
		}
	}
	return files, nil
}

func isText(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	if !utf8.Valid(data) {
		return false
	}
	for _, b := range data {
		if b == 0 {
			return false
		}
	}
	return true
}

func parseLineEdits(raw any) ([]lineEdit, error) {
	rows, ok := raw.([]any)
	if !ok {
		return nil, errors.New("edits must be an array")
	}
	edits := make([]lineEdit, 0, len(rows))
	for _, row := range rows {
		obj, ok := row.(map[string]any)
		if !ok {
			return nil, errors.New("each edit must be an object")
		}
		start := getIntArg(obj, "startLine", 0)
		end := getIntArg(obj, "endLine", 0)
		newText, _ := obj["newText"].(string)
		if start <= 0 || end < start {
			return nil, fmt.Errorf("invalid edit range: %d-%d", start, end)
		}
		edits = append(edits, lineEdit{StartLine: start, EndLine: end, NewText: newText})
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].StartLine < edits[j].StartLine })
	for i := 1; i < len(edits); i++ {
		if edits[i].StartLine <= edits[i-1].EndLine {
			return nil, errors.New("overlapping edits are not allowed")
		}
	}
	return edits, nil
}

func applyLineEdits(content string, edits []lineEdit) (string, int, error) {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	lineIdx := 1
	applied := 0
	for _, e := range edits {
		if e.EndLine > len(lines) {
			return "", 0, fmt.Errorf("edit range out of bounds: %d-%d", e.StartLine, e.EndLine)
		}
		for lineIdx < e.StartLine {
			out = append(out, lines[lineIdx-1])
			lineIdx++
		}
		if e.NewText != "" {
			out = append(out, strings.Split(e.NewText, "\n")...)
		}
		lineIdx = e.EndLine + 1
		applied++
	}
	for lineIdx <= len(lines) {
		out = append(out, lines[lineIdx-1])
		lineIdx++
	}
	return strings.Join(out, "\n"), applied, nil
}

func parseUnifiedDiff(patch string) ([]unifiedDiffHunk, error) {
	text := strings.ReplaceAll(patch, "\r\n", "\n")
	rows := strings.Split(text, "\n")

	hunks := make([]unifiedDiffHunk, 0)
	for i := 0; i < len(rows); {
		line := rows[i]
		if strings.TrimSpace(line) == "" {
			i++
			continue
		}
		if !strings.HasPrefix(line, "@@") {
			i++
			continue
		}

		match := unifiedDiffHeaderPattern.FindStringSubmatch(line)
		if match == nil {
			return nil, fmt.Errorf("invalid unified diff hunk header: %s", line)
		}
		oldStart, _ := strconv.Atoi(match[1])
		oldCount := 1
		if strings.TrimSpace(match[2]) != "" {
			oldCount, _ = strconv.Atoi(match[2])
		}
		newStart, _ := strconv.Atoi(match[3])
		newCount := 1
		if strings.TrimSpace(match[4]) != "" {
			newCount, _ = strconv.Atoi(match[4])
		}
		if oldStart < 0 || oldCount < 0 || newStart < 0 || newCount < 0 {
			return nil, fmt.Errorf("invalid unified diff hunk header: %s", line)
		}

		hunk := unifiedDiffHunk{OldStart: oldStart, OldCount: oldCount, NewStart: newStart, NewCount: newCount}
		i++
		oldSeen := 0
		newSeen := 0
		for i < len(rows) {
			body := rows[i]
			if strings.HasPrefix(body, "@@") {
				break
			}
			if body == "" {
				i++
				continue
			}
			if body == `\ No newline at end of file` {
				i++
				continue
			}
			prefix := body[0]
			if prefix != ' ' && prefix != '+' && prefix != '-' {
				return nil, fmt.Errorf("invalid unified diff line prefix at hunk %d: %s", len(hunks)+1, body)
			}
			if prefix == ' ' || prefix == '-' {
				oldSeen++
			}
			if prefix == ' ' || prefix == '+' {
				newSeen++
			}
			hunk.Lines = append(hunk.Lines, body)
			i++
		}

		if oldSeen != oldCount || newSeen != newCount {
			return nil, fmt.Errorf("hunk line counts do not match header at hunk %d", len(hunks)+1)
		}
		hunks = append(hunks, hunk)
	}

	if len(hunks) == 0 {
		return nil, errors.New("patch does not contain any @@ hunks")
	}
	return hunks, nil
}

func applyUnifiedDiff(content string, hunks []unifiedDiffHunk) (string, int, error) {
	src, hadTrailingNewline := splitContentLines(content)
	out := make([]string, 0, len(src))
	total := len(src)
	cursor := 1

	for idx, hunk := range hunks {
		target := hunk.OldStart
		if target == 0 && hunk.OldCount == 0 {
			target = 1
		}
		if target <= 0 {
			return "", 0, fmt.Errorf("hunk start out of bounds at old line %d", hunk.OldStart)
		}
		if target < cursor {
			return "", 0, fmt.Errorf("hunks out of order or overlapping at hunk %d", idx+1)
		}
		if target > total+1 {
			return "", 0, fmt.Errorf("hunk start out of bounds at old line %d", hunk.OldStart)
		}

		for cursor < target {
			out = append(out, src[cursor-1])
			cursor++
		}

		for _, row := range hunk.Lines {
			prefix := row[0]
			text := row[1:]
			switch prefix {
			case ' ':
				if cursor > total || src[cursor-1] != text {
					return "", 0, fmt.Errorf("hunk context mismatch at old line %d", cursor)
				}
				out = append(out, text)
				cursor++
			case '-':
				if cursor > total || src[cursor-1] != text {
					return "", 0, fmt.Errorf("hunk context mismatch at old line %d", cursor)
				}
				cursor++
			case '+':
				out = append(out, text)
			}
		}
	}

	for cursor <= total {
		out = append(out, src[cursor-1])
		cursor++
	}

	updated := strings.Join(out, "\n")
	if hadTrailingNewline {
		updated += "\n"
	}
	return updated, len(hunks), nil
}

func splitContentLines(content string) ([]string, bool) {
	if content == "" {
		return nil, false
	}
	hadTrailingNewline := strings.HasSuffix(content, "\n")
	if hadTrailingNewline {
		content = strings.TrimSuffix(content, "\n")
	}
	if content == "" {
		return []string{}, hadTrailingNewline
	}
	return strings.Split(content, "\n"), hadTrailingNewline
}

func getString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing argument: %s", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument must be string: %s", key)
	}
	return s, nil
}

func getIntArg(args map[string]any, key string, fallback int) int {
	v, ok := args[key]
	if !ok {
		return fallback
	}
	s := fmt.Sprintf("%v", v)
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}

func getBoolArg(args map[string]any, key string, fallback bool) bool {
	v, ok := args[key]
	if !ok {
		return fallback
	}
	b, ok := v.(bool)
	if !ok {
		return fallback
	}
	return b
}

func guardWorkspaceControlPlaneFilename(workspace, targetAbs, agentID string) error {
	base := strings.ToUpper(filepath.Base(targetAbs))
	if !workspaceControlPlaneFilenames[base] {
		return nil
	}
	within, err := isWithinWorkspace(workspace, targetAbs)
	if err != nil || !within {
		return err
	}
	if agentID == "" {
		agentID = "<agent_id>"
	}
	file := filepath.Base(targetAbs)
	return fmt.Errorf("%s in workspace does not control agent behavior. Control-plane files live at .openclawssy/agents/<agent_id>/%s (for this run: .openclawssy/agents/%s/%s). Edit it via dashboard Agent Files", file, file, agentID, file)
}

func isWithinWorkspace(workspace, target string) (bool, error) {
	wsAbs, err := filepath.Abs(workspace)
	if err != nil {
		return false, err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(wsAbs, targetAbs)
	if err != nil {
		return false, err
	}
	if rel == "." {
		return true, nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, nil
	}
	return !filepath.IsAbs(rel), nil
}
