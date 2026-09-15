package codingagent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConvertDocCapsMarkitdownOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "brief.docx")
	if err := os.WriteFile(input, []byte("document"), 0o600); err != nil {
		t.Fatal(err)
	}
	markitdown := filepath.Join(dir, "markitdown")
	if err := os.WriteFile(markitdown, []byte("#!/bin/sh\nhead -c 2000001 /dev/zero\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatal(err)
	}
	defer os.Setenv("PATH", oldPath)

	_, err := (convertDocTool{cwd: dir}).Execute(context.Background(), map[string]any{"path": input})
	if err == nil || !strings.Contains(err.Error(), "output exceeds 2000000 bytes") {
		t.Fatalf("error=%v, want bounded-output error", err)
	}
}
