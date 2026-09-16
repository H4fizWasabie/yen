package codingagent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/tools"
)

type convertDocTool struct{ cwd string }

const convertDocMaxOutput = 2_000_000

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
	stdout, err := command.StdoutPipe()
	if err != nil {
		return "", err
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		var executableError *exec.Error
		if errors.As(err, &executableError) {
			return "", fmt.Errorf("markitdown is not installed; install the markitdown CLI before using convert_doc")
		}
		return "", fmt.Errorf("markitdown failed: %w", err)
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, convertDocMaxOutput+1))
	if len(output) > convertDocMaxOutput {
		_ = command.Process.Kill()
		_ = command.Wait()
		return "", markitdownError(fmt.Errorf("output exceeds %d bytes", convertDocMaxOutput), stderr.String())
	}
	if readErr != nil {
		_ = command.Wait()
		return "", markitdownError(readErr, stderr.String())
	}
	if err := command.Wait(); err != nil {
		return "", markitdownError(err, stderr.String())
	}
	text := strings.TrimSpace(string(output))
	if text == "" {
		return "The document contained no extractable text.", nil
	}
	return text, nil
}

func markitdownError(err error, stderr string) error {
	if message := strings.TrimSpace(stderr); message != "" {
		return fmt.Errorf("markitdown failed: %s: %w", message, err)
	}
	return fmt.Errorf("markitdown failed: %w", err)
}

var _ agent.Tool = convertDocTool{}
