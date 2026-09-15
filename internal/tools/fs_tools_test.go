package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilesystemTools(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\nworld\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	ls, err := NewListTool(dir).Execute(ctx, map[string]any{})
	if err != nil || !strings.Contains(ls, "a.txt") || !strings.Contains(ls, "sub/") {
		t.Fatalf("ls=%q err=%v", ls, err)
	}
	find, err := NewFindTool(dir).Execute(ctx, map[string]any{"pattern": "*.txt"})
	if err != nil || find != "a.txt" {
		t.Fatalf("find=%q err=%v", find, err)
	}
	grep, err := NewGrepTool(dir).Execute(ctx, map[string]any{"pattern": "world"})
	if err != nil || !strings.Contains(grep, "a.txt:2:world") {
		t.Fatalf("grep=%q err=%v", grep, err)
	}
	if _, err := NewWriteTool(dir).Execute(ctx, map[string]any{"path": "sub/new.txt", "content": "old"}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewEditTool(dir).Execute(ctx, map[string]any{"path": "sub/new.txt", "edits": []any{map[string]any{"oldText": "old", "newText": "new"}}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "sub/new.txt"))
	if err != nil || string(data) != "new" {
		t.Fatalf("edited=%q err=%v", data, err)
	}
}

func TestFindToolSupportsRecursiveGlobstar(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "internal", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "internal", "nested", "main.go"), []byte("package nested"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("readme"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := NewFindTool(dir).Execute(context.Background(), map[string]any{"pattern": "**/*.go"})
	if err != nil || got != "internal/nested/main.go" {
		t.Fatalf("find=%q err=%v", got, err)
	}
}

func TestEditRequiresUniqueOldText(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("x\nx\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewEditTool(dir).Execute(context.Background(), map[string]any{
		"path": "x.txt", "oldText": "x", "newText": "y",
	})
	if err == nil || !strings.Contains(err.Error(), "unique") {
		t.Fatalf("err=%v", err)
	}
}
