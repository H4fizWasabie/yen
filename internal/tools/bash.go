package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

const (
	bashSpillBytes = 6 * 1024
	bashTailBytes  = 12 * 1024
)

type BashOptions struct {
	ShellPath     string
	CommandPrefix string
}

type BashTool struct {
	cwd, name     string
	shellPath     string
	commandPrefix string
}

func NewBashTool(cwd string) BashTool       { return BashTool{cwd: cwd, name: "bash"} }
func NewPowerShellTool(cwd string) BashTool { return BashTool{cwd: cwd, name: "powershell"} }
func NewBashToolWithOptions(cwd string, options BashOptions) BashTool {
	return BashTool{cwd: cwd, name: "bash", shellPath: options.ShellPath, commandPrefix: options.CommandPrefix}
}
func (t BashTool) Name() string { return t.name }

type BashResult struct {
	Output         string
	ExitCode       *int
	Cancelled      bool
	Truncated      bool
	FullOutputPath string
}

func (t BashTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	result, err := t.ExecuteResult(ctx, args)
	return result.Output, err
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
	shell, flag := t.shellPath, "-lc"
	if shell == "" {
		shell = "bash"
	}
	if t.name == "powershell" {
		shell, flag = "powershell", "-Command"
	}
	if t.commandPrefix != "" {
		command = t.commandPrefix + "\n" + command
	}
	capture := newBashCapture()
	cmd := exec.CommandContext(ctx, shell, flag, command)
	cmd.Dir = t.cwd
	cmd.Stdout, cmd.Stderr = capture, capture
	err := cmd.Run()
	result := capture.result()
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
			return result, fmt.Errorf("command exited with code %d: %s", code, result.Output)
		}
		return result, err
	}
	code := 0
	result.ExitCode = &code
	return result, nil
}

type bashCapture struct {
	mu     sync.Mutex
	prefix []byte
	tail   []byte
	total  int
	spill  *os.File
}

func newBashCapture() *bashCapture { return &bashCapture{} }

func (c *bashCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	previous := c.total
	c.total += len(p)
	if len(c.prefix) < bashSpillBytes {
		end := bashSpillBytes - len(c.prefix)
		if end > len(p) {
			end = len(p)
		}
		c.prefix = append(c.prefix, p[:end]...)
	}
	if c.spill == nil && c.total > bashSpillBytes {
		c.spill, _ = os.CreateTemp("", "yen-bash-*.log")
		if c.spill != nil {
			_ = c.spill.Chmod(0o600)
			_, _ = c.spill.Write(c.prefix)
		}
	}
	if c.spill != nil {
		start := 0
		if previous < bashSpillBytes {
			start = bashSpillBytes - previous
			if start > len(p) {
				start = len(p)
			}
		}
		_, _ = c.spill.Write(p[start:])
	}
	c.tail = append(c.tail, p...)
	if len(c.tail) > bashTailBytes {
		c.tail = append([]byte(nil), c.tail[len(c.tail)-bashTailBytes:]...)
	}
	return len(p), nil
}

func (c *bashCapture) result() BashResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	path := ""
	if c.spill != nil {
		path = c.spill.Name()
		_ = c.spill.Close()
	}
	return BashResult{Output: string(c.tail), Truncated: c.total > bashTailBytes, FullOutputPath: path}
}
