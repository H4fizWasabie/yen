package tools

import (
	"context"
	"strings"
	"testing"
)

func TestBashToolRunsInWorkspace(t *testing.T) {
	output, err := NewBashTool(t.TempDir()).Execute(context.Background(), map[string]any{"command": "printf hello"})
	if err != nil || output != "hello" {
		t.Fatalf("output=%q err=%v", output, err)
	}
}

func TestBashToolRejectsInvalidTimeout(t *testing.T) {
	_, err := NewBashTool(t.TempDir()).Execute(context.Background(), map[string]any{"command": "true", "timeout": 0.0})
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("err=%v", err)
	}
}

func TestBashToolStopsTimedOutCommandTree(t *testing.T) {
	_, err := NewBashTool(t.TempDir()).Execute(context.Background(), map[string]any{"command": "sleep 10", "timeout": 0.01})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err=%v", err)
	}
}

func TestBashToolAppliesConfiguredShellPathAndPrefix(t *testing.T) {
	tool := NewBashToolWithOptions(t.TempDir(), BashOptions{
		ShellPath:     "/bin/bash",
		CommandPrefix: "export YEN_BASH_PREFIX=applied",
	})
	output, err := tool.Execute(context.Background(), map[string]any{"command": `printf "$YEN_BASH_PREFIX"`})
	if err != nil || output != "applied" {
		t.Fatalf("output=%q err=%v", output, err)
	}
}

func TestResolveShellUsesConfiguredPortableFlags(t *testing.T) {
	if shell, flag := resolveShell("bash", "/custom/bash"); shell != "/custom/bash" || flag != "-lc" {
		t.Fatalf("bash=%q %q", shell, flag)
	}
	if shell, flag := resolveShell("powershell", "/custom/pwsh"); shell != "/custom/pwsh" || flag != "-Command" {
		t.Fatalf("powershell=%q %q", shell, flag)
	}
}
