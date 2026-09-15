package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type BashTool struct{ cwd, name string }

func NewBashTool(cwd string) BashTool       { return BashTool{cwd: cwd, name: "bash"} }
func NewPowerShellTool(cwd string) BashTool { return BashTool{cwd: cwd, name: "powershell"} }
func (t BashTool) Name() string             { return t.name }
func (t BashTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	result, err := t.ExecuteResult(ctx, args)
	return result.Output, err
}

type BashResult struct {
	Output    string
	ExitCode  *int
	Cancelled bool
	Truncated bool
}

func (t BashTool) ExecuteResult(ctx context.Context, args map[string]any) (BashResult, error) {
	command, ok := args["command"].(string)
	if !ok || command == "" {
		return BashResult{}, fmt.Errorf("command is required")
	}
	if raw, present := args["timeout"]; present {
		seconds, ok := raw.(float64)
		if !ok {
			if integer, okInt := raw.(int); okInt {
				seconds = float64(integer)
				ok = true
			}
		}
		if !ok || seconds <= 0 {
			return BashResult{}, fmt.Errorf("invalid timeout: must be a finite number of seconds")
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(seconds*float64(time.Second)))
		defer cancel()
	}
	shell, flag := "bash", "-lc"
	if t.name == "powershell" {
		shell, flag = "powershell", "-Command"
	}
	cmd := exec.CommandContext(ctx, shell, flag, command)
	cmd.Dir = t.cwd
	output, err := cmd.CombinedOutput()
	raw := strings.TrimSuffix(string(output), "\n")
	text := truncateToolOutput(raw)
	result := BashResult{Output: text, Truncated: text != raw}
	if ctx.Err() != nil {
		result.Cancelled = true
		if ctx.Err() == context.DeadlineExceeded {
			return result, fmt.Errorf("command timed out")
		}
		return result, fmt.Errorf("command aborted")
	}
	if err != nil {
		if cmd.ProcessState != nil {
			code := cmd.ProcessState.ExitCode()
			result.ExitCode = &code
			return result, fmt.Errorf("command exited with code %d: %s", code, strings.TrimSpace(string(output)))
		}
		return result, err
	}
	code := 0
	result.ExitCode = &code
	return result, nil
}
