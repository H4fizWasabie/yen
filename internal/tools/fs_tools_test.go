package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
	if err != nil || !strings.Contains(grep, "a.txt:2: world") {
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

func TestListToolSortsBeforeApplyingLimit(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"Z.txt", "a.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := NewListTool(dir).Execute(context.Background(), map[string]any{"limit": 1})
	if err != nil || !strings.HasPrefix(got, "a.txt\n") {
		t.Fatalf("ls=%q err=%v", got, err)
	}
}

func TestListToolSkipsBrokenSymlinks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "good.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "missing.txt"), filepath.Join(dir, "broken.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	got, err := NewListTool(dir).Execute(context.Background(), map[string]any{})
	if err != nil || !strings.Contains(got, "good.txt") || strings.Contains(got, "broken.txt") {
		t.Fatalf("ls=%q err=%v", got, err)
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

func TestFindToolRespectsGitignore(t *testing.T) {
	dir := t.TempDir()
	if err := exec.Command("git", "-C", dir, "init", "-q").Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored/\n*.secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"visible.txt", "ignored/hidden.txt", "private.secret"} {
		fullPath := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte("needle\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := NewFindTool(dir).Execute(context.Background(), map[string]any{"pattern": "**/*"})
	if err != nil || got != ".gitignore\nvisible.txt" {
		t.Fatalf("find=%q err=%v", got, err)
	}

	got, err = NewGrepTool(dir).Execute(context.Background(), map[string]any{"pattern": "needle"})
	if err != nil || !strings.Contains(got, "visible.txt:1: needle") || strings.Contains(got, "hidden.txt") || strings.Contains(got, "private.secret") {
		t.Fatalf("grep=%q err=%v", got, err)
	}
}

func TestGrepToolMatchesRecursiveGlobstarPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "internal", "nested", "main.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package nested\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := NewGrepTool(dir).Execute(context.Background(), map[string]any{
		"pattern": "package", "glob": "internal/**/*.go",
	})
	if err != nil || !strings.Contains(got, "internal/nested/main.go:1: package nested") {
		t.Fatalf("grep=%q err=%v", got, err)
	}
}

func TestGrepToolSkipsBinaryFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "binary.dat"), []byte("needle\x00binary\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := NewGrepTool(dir).Execute(context.Background(), map[string]any{"pattern": "needle"})
	if err != nil || got != "No matches found" {
		t.Fatalf("grep=%q err=%v", got, err)
	}
}

func TestGrepToolRemovesCarriageReturns(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cr.txt"), []byte("needle\rtext\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := NewGrepTool(dir).Execute(context.Background(), map[string]any{"pattern": "needle"})
	if err != nil || got != "cr.txt:1: needletext" {
		t.Fatalf("grep=%q err=%v", got, err)
	}
}

func TestGrepToolTruncatesLongMatchingLines(t *testing.T) {
	dir := t.TempDir()
	line := "match " + strings.Repeat("x", 600)
	if err := os.WriteFile(filepath.Join(dir, "long.txt"), []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := NewGrepTool(dir).Execute(context.Background(), map[string]any{"pattern": "match"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "... [truncated]") || strings.Contains(got, strings.Repeat("x", 600)) {
		t.Fatalf("grep=%q", got)
	}
}

func TestGrepToolUsesOracleMatchSpacing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "match.txt"), []byte("needle\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := NewGrepTool(dir).Execute(context.Background(), map[string]any{"pattern": "needle"})
	if err != nil || !strings.Contains(got, "match.txt:1: needle") {
		t.Fatalf("grep=%q err=%v", got, err)
	}
}

func TestGrepToolFormatsContextLinesLikeOracle(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "context.txt"), []byte("before\nneedle\nafter\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := NewGrepTool(dir).Execute(context.Background(), map[string]any{"pattern": "needle", "context": 1})
	if err != nil || !strings.Contains(got, "context.txt-1- before") || !strings.Contains(got, "context.txt:2: needle") || !strings.Contains(got, "context.txt-3- after") {
		t.Fatalf("grep=%q err=%v", got, err)
	}
}

func TestGrepToolLimitCountsMatchesNotContextLines(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "context.txt"), []byte("before\nneedle\nafter\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := NewGrepTool(dir).Execute(context.Background(), map[string]any{"pattern": "needle", "context": 1, "limit": 1})
	if err != nil || !strings.Contains(got, "context.txt-1- before") || !strings.Contains(got, "context.txt:2: needle") || !strings.Contains(got, "context.txt-3- after") {
		t.Fatalf("grep=%q err=%v", got, err)
	}
}

func TestGrepToolReportsMatchLimit(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("match\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := NewGrepTool(dir).Execute(context.Background(), map[string]any{"pattern": "match", "limit": 1})
	if err != nil || !strings.Contains(got, "1 matches limit reached. Use limit=2 for more, or refine pattern") {
		t.Fatalf("grep=%q err=%v", got, err)
	}
}

func TestFileSearchToolsBoundOutput(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 200; i++ {
		name := fmt.Sprintf("entry-%03d-%s.txt", i, strings.Repeat("x", 40))
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Repeat("match ", 15)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	ls, err := NewListTool(dir).Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	find, err := NewFindTool(dir).Execute(context.Background(), map[string]any{"pattern": "*.txt"})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range []string{ls, find} {
		if !strings.Contains(result, "[Output truncated]") {
			t.Fatalf("result was not truncated: %q", result)
		}
	}
	grep, err := NewGrepTool(dir).Execute(context.Background(), map[string]any{"pattern": "match"})
	if err != nil || strings.Contains(grep, "[Output truncated]") {
		t.Fatalf("grep=%q err=%v", grep, err)
	}
}

func TestFileSearchToolsReportResultLimit(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	ls, err := NewListTool(dir).Execute(context.Background(), map[string]any{"limit": 1})
	if err != nil || !strings.Contains(ls, "1 entries limit reached") {
		t.Fatalf("ls=%q err=%v", ls, err)
	}
	find, err := NewFindTool(dir).Execute(context.Background(), map[string]any{"pattern": "*.txt", "limit": 1})
	if err != nil || !strings.Contains(find, "1 results limit reached. Use limit=2 for more, or refine pattern") {
		t.Fatalf("find=%q err=%v", find, err)
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
