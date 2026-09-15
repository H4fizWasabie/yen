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
