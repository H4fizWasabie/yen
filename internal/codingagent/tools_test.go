package codingagent

import "testing"

func TestNewToolsMatchesCodingToolCore(t *testing.T) {
	got := NewTools(t.TempDir())
	want := []string{"read", "bash", "powershell", "edit", "write", "grep", "find", "ls"}
	if len(got) != len(want) {
		t.Fatalf("tool count=%d, want %d", len(got), len(want))
	}
	for i, tool := range got {
		if tool.Name() != want[i] {
			t.Errorf("tool %d=%q, want %q", i, tool.Name(), want[i])
		}
	}
}
