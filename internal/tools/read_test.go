package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadToolReadsRelativeText(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("one\ntwo\nthree"), 0o600); err != nil {
		t.Fatal(err)
	}

	content, err := NewReadTool(dir).Execute(context.Background(), map[string]any{"path": "README.md", "offset": float64(2), "limit": float64(1)})
	if err != nil {
		t.Fatal(err)
	}
	if content != "two\n\n[1 more lines in file. Use offset=3 to continue.]" {
		t.Fatalf("content = %q", content)
	}
}
