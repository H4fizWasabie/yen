package codingagent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/tools"
)

type convertDocTool struct{ cwd string }

func (convertDocTool) Name() string { return "convert_doc" }

func (t convertDocTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return "", fmt.Errorf("path is required")
	}
	resolved := tools.ResolvePath(path, t.cwd)
	if _, err := os.Stat(resolved); err != nil {
		return "", err
	}
	command := exec.CommandContext(ctx, "markitdown", resolved)
	output, err := command.Output()
	if err != nil {
		var executableError *exec.Error
		if errors.As(err, &executableError) {
			return "", fmt.Errorf("markitdown is not installed; install the markitdown CLI before using convert_doc")
		}
		return "", fmt.Errorf("markitdown failed: %w", err)
	}
	text := strings.TrimSpace(string(output))
	if text == "" {
		return "The document contained no extractable text.", nil
	}
	return text, nil
}

var _ agent.Tool = convertDocTool{}
