package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type BashTool struct{ cwd, name string }
func NewBashTool(cwd string) BashTool { return BashTool{cwd: cwd, name: "bash"} }
func NewPowerShellTool(cwd string) BashTool { return BashTool{cwd: cwd, name: "powershell"} }
func (t BashTool) Name() string { return t.name }
func (t BashTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	command, ok := args["command"].(string); if !ok || command == "" { return "", fmt.Errorf("command is required") }
	if raw, present := args["timeout"]; present { seconds, ok := raw.(float64); if !ok { if integer, okInt := raw.(int); okInt { seconds = float64(integer); ok = true } }; if !ok || seconds <= 0 { return "", fmt.Errorf("invalid timeout: must be a finite number of seconds") }; var cancel context.CancelFunc; ctx, cancel = context.WithTimeout(ctx, time.Duration(seconds*float64(time.Second))); defer cancel() }
	shell, flag := "bash", "-lc"; if t.name == "powershell" { shell, flag = "powershell", "-Command" }
	cmd := exec.CommandContext(ctx, shell, flag, command); cmd.Dir = t.cwd
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil { if ctx.Err() == context.DeadlineExceeded { return "", fmt.Errorf("command timed out") }; return "", fmt.Errorf("command aborted") }
	if err != nil { return strings.TrimSpace(string(output)), fmt.Errorf("command exited with code %d: %s", cmd.ProcessState.ExitCode(), strings.TrimSpace(string(output))) }
	return truncateToolOutput(strings.TrimSuffix(string(output), "\n")), nil
}
