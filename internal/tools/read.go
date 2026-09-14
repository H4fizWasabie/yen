package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxReadLines = 500
	maxReadBytes = 12 * 1024
)

type ReadTool struct {
	cwd string
}

func NewReadTool(cwd string) ReadTool { return ReadTool{cwd: cwd} }

func (ReadTool) Name() string { return "read" }

func (t ReadTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	rawPath, ok := args["path"].(string)
	if !ok || rawPath == "" {
		return "", fmt.Errorf("path is required")
	}
	path := rawPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(t.cwd, path)
	}
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
	output, truncated := truncate(selected, maxReadLines, maxReadBytes)
	if truncated {
		endLine := start + strings.Count(output, "\n") + 1
		output += fmt.Sprintf("\n\n[Showing lines %d-%d of %d. Use offset=%d to continue.]", start+1, endLine, len(lines), endLine+1)
	} else if end < len(lines) {
		output += fmt.Sprintf("\n\n[%d more lines in file. Use offset=%d to continue.]", len(lines)-end, end+1)
	}
	return output, nil
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

func truncate(lines []string, maxLines, maxBytes int) (string, bool) {
	if len(lines) <= maxLines && len([]byte(strings.Join(lines, "\n"))) <= maxBytes {
		return strings.Join(lines, "\n"), false
	}
	if len(lines) > 0 && len([]byte(lines[0])) > maxBytes {
		return "", true
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
	return strings.Join(kept, "\n"), true
}
