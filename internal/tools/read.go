package tools

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/unicode/norm"
)

const (
	maxReadLines = 500
	maxReadBytes = 12 * 1024
)

type ReadTool struct {
	cwd string
}

func NewReadTool(cwd string) ReadTool { return ReadTool{cwd: cwd} }

func ResolvePath(rawPath, cwd string) string { return normalizeReadPath(rawPath, cwd) }

func (ReadTool) Name() string { return "read" }

func (t ReadTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	rawPath, ok := args["path"].(string)
	if !ok || rawPath == "" {
		return "", fmt.Errorf("path is required")
	}
	requestedPath := normalizeReadPath(rawPath, t.cwd)
	path := resolveReadPath(rawPath, t.cwd)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	lines := strings.Split(string(data), "\n")
	start, err := readNumber(args, "offset", 1)
	if err != nil {
		return "", err
	}
	if start < 1 {
		start = 1
	}
	start--
	if start >= len(lines) {
		return "", fmt.Errorf("offset %d is beyond end of file (%d lines total)", start+1, len(lines))
	}

	end := len(lines)
	if limit, present, err := optionalReadNumber(args, "limit"); err != nil {
		return "", err
	} else if present {
		if limit < 0 {
			limit = 0
		}
		end = min(start+limit, len(lines))
	}
	selected := lines[start:end]
	if len(selected) > 0 && len([]byte(selected[0])) > maxReadBytes {
		return fmt.Sprintf("[Line %d exceeds %d byte limit. Use a shell command to read it in chunks.]", start+1, maxReadBytes), nil
	}
	output, truncated, truncatedBy := truncate(selected, maxReadLines, maxReadBytes)
	if path != requestedPath {
		output = fmt.Sprintf("[Resolved read path: %q]\n\n%s", path, output)
	}
	if truncated {
		endLine := start + strings.Count(output, "\n") + 1
		if truncatedBy == "lines" {
			output += fmt.Sprintf("\n\n[Showing lines %d-%d of %d. Use offset=%d to continue.]", start+1, endLine, len(lines), endLine+1)
		} else {
			output += fmt.Sprintf("\n\n[Showing lines %d-%d of %d (12 KB limit). Use offset=%d to continue.]", start+1, endLine, len(lines), endLine+1)
		}
	} else if end < len(lines) {
		output += fmt.Sprintf("\n\n[%d more lines in file. Use offset=%d to continue.]", len(lines)-end, end+1)
	}
	return output, nil
}

func resolveReadPath(rawPath, cwd string) string {
	path := normalizeReadPath(rawPath, cwd)
	resolved := normalizeUnicodeSpaces(path)
	candidates := append([]string{path}, readPathCandidates(resolved)...)
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return path
}

func normalizeReadPath(rawPath, cwd string) string {
	path := strings.TrimPrefix(rawPath, "@")
	if strings.HasPrefix(path, "file://") {
		if parsed, err := url.Parse(path); err == nil && parsed.Path != "" {
			path = parsed.Path
		}
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	return filepath.Clean(path)
}

func readPathCandidates(path string) []string {
	curly := strings.ReplaceAll(path, "'", "’")
	spaceAM := strings.ReplaceAll(path, " AM.", "\u202fAM.")
	spaceAM = strings.ReplaceAll(spaceAM, " am.", "\u202fam.")
	spacePM := strings.ReplaceAll(path, " PM.", "\u202fPM.")
	spacePM = strings.ReplaceAll(spacePM, " pm.", "\u202fpm.")
	candidates := []string{
		path,
		spaceAM,
		spacePM,
		norm.NFD.String(path),
		curly,
		norm.NFD.String(curly),
		norm.NFD.String(spaceAM),
		norm.NFD.String(spacePM),
	}
	seen := make(map[string]struct{}, len(candidates))
	unique := candidates[:0]
	for _, candidate := range candidates {
		if _, ok := seen[candidate]; !ok {
			seen[candidate] = struct{}{}
			unique = append(unique, candidate)
		}
	}
	return unique
}

func normalizeUnicodeSpaces(path string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\u00a0', '\u2000', '\u2001', '\u2002', '\u2003', '\u2004', '\u2005', '\u2006', '\u2007', '\u2008', '\u2009', '\u200a', '\u202f', '\u205f', '\u3000':
			return ' '
		default:
			return r
		}
	}, path)
}

func readNumber(args map[string]any, key string, fallback int) (int, error) {
	value, present, err := optionalReadNumber(args, key)
	if err != nil {
		return 0, err
	}
	if !present {
		return fallback, nil
	}
	return value, nil
}

func optionalReadNumber(args map[string]any, key string) (int, bool, error) {
	value, present := args[key]
	if !present {
		return 0, false, nil
	}
	switch number := value.(type) {
	case int:
		return number, true, nil
	case float64:
		return int(number), true, nil
	default:
		return 0, true, fmt.Errorf("%s must be a number", key)
	}
}

func truncate(lines []string, maxLines, maxBytes int) (string, bool, string) {
	if len(lines) <= maxLines && len([]byte(strings.Join(lines, "\n"))) <= maxBytes {
		return strings.Join(lines, "\n"), false, ""
	}
	if len(lines) > 0 && len([]byte(lines[0])) > maxBytes {
		return "", true, "bytes"
	}
	kept := make([]string, 0, min(len(lines), maxLines))
	bytes := 0
	for len(kept) < len(lines) && len(kept) < maxLines {
		line := lines[len(kept)]
		lineBytes := len([]byte(line))
		if len(kept) > 0 {
			lineBytes++
		}
		if bytes+lineBytes > maxBytes {
			break
		}
		kept = append(kept, line)
		bytes += lineBytes
	}
	if len(kept) == maxLines {
		return strings.Join(kept, "\n"), true, "lines"
	}
	return strings.Join(kept, "\n"), true, "bytes"
}
