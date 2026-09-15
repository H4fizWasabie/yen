package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const toolOutputLimit = 64 * 1024
const fileSearchOutputLimit = 6 * 1024
const grepMaxLineLength = 500

type ListTool struct{ cwd string }
func NewListTool(cwd string) ListTool { return ListTool{cwd: cwd} }
func (ListTool) Name() string { return "ls" }
func (t ListTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if err := ctx.Err(); err != nil { return "", err }
	path := normalizeReadPath(stringArg(args, "path", "."), t.cwd)
	entries, err := os.ReadDir(path)
	if err != nil { return "", fmt.Errorf("cannot read directory: %w", err) }
	limit := intArg(args, "limit", 500)
	if limit < 1 { limit = 1 }
	validEntries := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if _, err := os.Stat(filepath.Join(path, entry.Name())); err == nil { validEntries = append(validEntries, entry) }
	}
	entries = validEntries
	limitReached := len(entries) > limit
	sort.SliceStable(entries, func(i, j int) bool { return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name()) })
	lines := make([]string, 0, min(len(entries), limit))
	for i, entry := range entries {
		if i >= limit { break }
		name := entry.Name()
		if entry.IsDir() { name += "/" }
		lines = append(lines, name)
	}
	if len(lines) == 0 { return "(empty directory)", nil }
	output := truncateFileSearchOutput(strings.Join(lines, "\n"))
	if limitReached { output += fmt.Sprintf("\n\n[%d entries limit reached. Use limit=%d for more]", limit, limit*2) }
	return output, nil
}

type FindTool struct{ cwd string }
func NewFindTool(cwd string) FindTool { return FindTool{cwd: cwd} }
func (FindTool) Name() string { return "find" }
func (t FindTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if err := ctx.Err(); err != nil { return "", err }
	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" { return "", fmt.Errorf("pattern is required") }
	root := normalizeReadPath(stringArg(args, "path", "."), t.cwd)
	limit := intArg(args, "limit", 1000)
	if limit < 1 { limit = 1 }
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil { return err }
		if e := ctx.Err(); e != nil { return e }
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "node_modules") { return filepath.SkipDir }
		if !entry.IsDir() {
			rel, err := filepath.Rel(root, path); if err != nil { return err }
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil { return "", err }
	ignored := gitIgnoredPaths(ctx, root, files)
	var matches []string
	matchCount := 0
	for _, rel := range files {
		if e := ctx.Err(); e != nil { return "", e }
		if _, ok := ignored[rel]; ok { continue }
		matched := matchFindPattern(pattern, rel)
		if matched {
			matchCount++
			if len(matches) < limit { matches = append(matches, rel) }
		}
	}
	if len(matches) == 0 { return "No files found matching pattern", nil }
	sort.Strings(matches)
	output := truncateFileSearchOutput(strings.Join(matches, "\n"))
	if matchCount >= limit { output += fmt.Sprintf("\n\n[%d results limit reached. Use limit=%d for more, or refine pattern]", limit, limit*2) }
	return output, nil
}

func gitIgnoredPaths(ctx context.Context, root string, rels []string) map[string]struct{} {
	if len(rels) == 0 { return nil }
	var input bytes.Buffer
	for _, rel := range rels {
		input.WriteString(filepath.FromSlash(rel))
		input.WriteByte(0)
	}
	var output bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", "-C", root, "check-ignore", "-z", "--stdin")
	cmd.Stdin = &input
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 { return nil }
	}
	ignored := make(map[string]struct{})
	for _, path := range bytes.Split(output.Bytes(), []byte{0}) {
		if len(path) > 0 { ignored[filepath.ToSlash(string(path))] = struct{}{} }
	}
	return ignored
}

func matchFindPattern(pattern, rel string) bool {
	pattern = filepath.ToSlash(pattern)
	if !strings.Contains(pattern, "/") {
		matched, _ := filepath.Match(pattern, filepath.Base(rel))
		return matched
	}
	return matchGlobstar(strings.Split(strings.Trim(pattern, "/"), "/"), strings.Split(strings.Trim(rel, "/"), "/"), 0, 0)
}

func matchGlobstar(pattern, path []string, patternIndex, pathIndex int) bool {
	if patternIndex == len(pattern) {
		return pathIndex == len(path)
	}
	if pattern[patternIndex] == "**" {
		return matchGlobstar(pattern, path, patternIndex+1, pathIndex) ||
			(pathIndex < len(path) && matchGlobstar(pattern, path, patternIndex, pathIndex+1))
	}
	if pathIndex == len(path) {
		return false
	}
	matched, _ := filepath.Match(pattern[patternIndex], path[pathIndex])
	return matched && matchGlobstar(pattern, path, patternIndex+1, pathIndex+1)
}

type GrepTool struct{ cwd string }
func NewGrepTool(cwd string) GrepTool { return GrepTool{cwd: cwd} }
func (GrepTool) Name() string { return "grep" }
func (t GrepTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if err := ctx.Err(); err != nil { return "", err }
	pattern, ok := args["pattern"].(string); if !ok || pattern == "" { return "", fmt.Errorf("pattern is required") }
	literal, _ := args["literal"].(bool); ignoreCase, _ := args["ignoreCase"].(bool)
	if literal { pattern = regexp.QuoteMeta(pattern) }
	if ignoreCase { pattern = "(?i)" + pattern }
	re, err := regexp.Compile(pattern); if err != nil { return "", err }
	root := normalizeReadPath(stringArg(args, "path", "."), t.cwd)
	glob := stringArg(args, "glob", "")
	limit := intArg(args, "limit", 100); if limit < 1 { limit = 1 }
	contextLines := intArg(args, "context", 0); if contextLines < 0 { contextLines = 0 }
	var files []string
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil { return walkErr }
		if e := ctx.Err(); e != nil { return e }
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "node_modules") { return filepath.SkipDir }
		if !entry.IsDir() {
			rel, err := filepath.Rel(root, path); if err != nil { return err }
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil { return "", err }
	ignored := gitIgnoredPaths(ctx, root, files)
	var out []string
	matchCount := 0
	matchLimitReached := false
filesLoop:
	for _, rel := range files {
		if e := ctx.Err(); e != nil { return "", e }
		if _, ok := ignored[rel]; ok { continue }
		if glob != "" && !matchFindPattern(glob, rel) { continue }
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))); if err != nil { continue }
		if bytes.IndexByte(data, 0) >= 0 { continue }
		lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", ""), "\n")
		for i, line := range lines {
			if !re.MatchString(line) { continue }
			matchCount++
			start, end := max(0, i-contextLines), min(len(lines), i+contextLines+1)
			for j := start; j < end; j++ {
				separator := ":"
				if j != i { separator = "-" }
				out = append(out, fmt.Sprintf("%s%s%d%s %s", rel, separator, j+1, separator, truncateGrepLine(lines[j])))
			}
			if matchCount >= limit { matchLimitReached = true; break filesLoop }
		}
	}
	if len(out) == 0 { return "No matches found", nil }
	result := truncateToolOutput(strings.Join(out, "\n"))
	if matchLimitReached { result += fmt.Sprintf("\n\n[%d matches limit reached. Use limit=%d for more, or refine pattern]", limit, limit*2) }
	return result, nil
}

func truncateGrepLine(line string) string {
	runes := []rune(line)
	if len(runes) <= grepMaxLineLength { return line }
	return string(runes[:grepMaxLineLength]) + "... [truncated]"
}

type WriteTool struct{ cwd string }
func NewWriteTool(cwd string) WriteTool { return WriteTool{cwd: cwd} }
func (WriteTool) Name() string { return "write" }
func (t WriteTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if err := ctx.Err(); err != nil { return "", err }
	path, ok := args["path"].(string); if !ok || path == "" { return "", fmt.Errorf("path is required") }
	content, ok := args["content"].(string); if !ok { return "", fmt.Errorf("content is required") }
	abs := normalizeReadPath(path, t.cwd)
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil { return "", err }
	if err := os.WriteFile(abs, []byte(content), 0o600); err != nil { return "", err }
	return fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), path), nil
}

type EditTool struct{ cwd string }
func NewEditTool(cwd string) EditTool { return EditTool{cwd: cwd} }
func (EditTool) Name() string { return "edit" }
func (t EditTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if err := ctx.Err(); err != nil { return "", err }
	path, ok := args["path"].(string); if !ok || path == "" { return "", fmt.Errorf("path is required") }
	data, err := os.ReadFile(normalizeReadPath(path, t.cwd)); if err != nil { return "", fmt.Errorf("could not edit file: %s: %w", path, err) }
	original := string(data)
	edits, err := editArgs(args); if err != nil { return "", err }
	result := original
	positions := make([][2]int, 0, len(edits))
	for _, edit := range edits {
		count := strings.Count(original, edit.oldText)
		if count != 1 { return "", fmt.Errorf("oldText must match a unique region in %s (found %d matches)", path, count) }
		idx := strings.Index(original, edit.oldText)
		for _, p := range positions { if idx < p[1] && idx+len(edit.oldText) > p[0] { return "", fmt.Errorf("edits overlap in %s", path) } }
		positions = append(positions, [2]int{idx, idx + len(edit.oldText)})
	}
	sort.Slice(positions, func(i, j int) bool { return positions[i][0] > positions[j][0] })
	for i, p := range positions { edit := edits[0]; for _, candidate := range edits { if strings.Index(original, candidate.oldText) == p[0] { edit = candidate; break } }; result = result[:p[0]] + edit.newText + result[p[1]:]; _ = i }
	if err := os.WriteFile(normalizeReadPath(path, t.cwd), []byte(result), 0o600); err != nil { return "", err }
	return fmt.Sprintf("Successfully replaced %d block(s) in %s.", len(edits), path), nil
}

type textEdit struct{ oldText, newText string }
func editArgs(args map[string]any) ([]textEdit, error) {
	if raw, ok := args["edits"].([]any); ok {
		if len(raw) == 0 { return nil, fmt.Errorf("edits must contain at least one replacement") }
		out := make([]textEdit, 0, len(raw)); for _, item := range raw { m, ok := item.(map[string]any); if !ok { return nil, fmt.Errorf("invalid edit") }; old, ok1 := m["oldText"].(string); newText, ok2 := m["newText"].(string); if !ok1 || !ok2 { return nil, fmt.Errorf("invalid edit") }; out = append(out, textEdit{old, newText}) }; return out, nil
	}
	old, ok1 := args["oldText"].(string); newText, ok2 := args["newText"].(string); if !ok1 || !ok2 { return nil, fmt.Errorf("edits must contain at least one replacement") }; return []textEdit{{old, newText}}, nil
}

func stringArg(args map[string]any, key, fallback string) string { if value, ok := args[key].(string); ok && value != "" { return value }; return fallback }
func intArg(args map[string]any, key string, fallback int) int { switch value := args[key].(type) { case int: return value; case float64: return int(value); default: return fallback } }
func truncateToolOutput(value string) string {
	return truncateOutput(value, toolOutputLimit)
}

func truncateFileSearchOutput(value string) string {
	return truncateOutput(value, fileSearchOutputLimit)
}

func truncateOutput(value string, limit int) string {
	if len(value) <= limit { return value }
	cut := value[:limit]
	if end := strings.LastIndexByte(cut, '\n'); end >= 0 { cut = cut[:end] }
	for !utf8.ValidString(cut) { cut = cut[:len(cut)-1] }
	return cut + "\n\n[Output truncated]"
}
