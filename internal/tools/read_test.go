package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
	"golang.org/x/text/unicode/norm"
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

func TestReadToolStripsAtPathPrefix(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	content, err := NewReadTool(dir).Execute(context.Background(), map[string]any{"path": "@README.md"})
	if err != nil {
		t.Fatal(err)
	}
	if content != "hello" {
		t.Fatalf("content = %q", content)
	}
}

func TestReadToolReportsFirstLineOverByteLimit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "large.txt"), []byte(strings.Repeat("x", 12*1024+1)), 0o600); err != nil {
		t.Fatal(err)
	}

	content, err := NewReadTool(dir).Execute(context.Background(), map[string]any{"path": "large.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "Line 1 exceeds 12288 byte limit") {
		t.Fatalf("content = %q", content)
	}
}

func TestReadToolReportsLineTruncationMetadata(t *testing.T) {
	dir := t.TempDir()
	lines := make([]string, maxReadLines+1)
	for i := range lines {
		lines[i] = "line"
	}
	if err := os.WriteFile(filepath.Join(dir, "many.txt"), []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	content, err := NewReadTool(dir).Execute(context.Background(), map[string]any{"path": "many.txt"})
	if err != nil || !strings.Contains(content, "Showing lines 1-500 of 501") {
		t.Fatalf("content=%q err=%v", content, err)
	}
}

func TestReadToolAllowsRelativeTraversalLikePinnedPathResolver(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside.txt"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	content, err := NewReadTool(child).Execute(context.Background(), map[string]any{"path": "../outside.txt"})
	if err != nil || content != "outside" {
		t.Fatalf("content=%q err=%v", content, err)
	}
}

func TestReadToolFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "target.txt"), []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.txt", filepath.Join(dir, "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	content, err := NewReadTool(dir).Execute(context.Background(), map[string]any{"path": "link.txt"})
	if err != nil || content != "target" {
		t.Fatalf("content=%q err=%v", content, err)
	}
}

func TestReadToolReadsUnicodeFilename(t *testing.T) {
	dir := t.TempDir()
	name := "résumé-日本語.txt"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("unicode"), 0o600); err != nil {
		t.Fatal(err)
	}

	content, err := NewReadTool(dir).Execute(context.Background(), map[string]any{"path": name})
	if err != nil || content != "unicode" {
		t.Fatalf("content=%q err=%v", content, err)
	}
}

func TestReadToolResolvesNormalizedUnicodeFilename(t *testing.T) {
	dir := t.TempDir()
	name := norm.NFD.String("café.txt")
	if err := os.WriteFile(filepath.Join(dir, name), []byte("decomposed"), 0o600); err != nil {
		t.Fatal(err)
	}

	content, err := NewReadTool(dir).Execute(context.Background(), map[string]any{"path": "café.txt"})
	if err != nil || !strings.Contains(content, "decomposed") || !strings.Contains(content, "Resolved read path") {
		t.Fatalf("content=%q err=%v", content, err)
	}
}

func TestReadToolResolvesUnicodeSpaceFilename(t *testing.T) {
	dir := t.TempDir()
	name := "Capture\u202fAM.txt"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("space variant"), 0o600); err != nil {
		t.Fatal(err)
	}
	content, err := NewReadTool(dir).Execute(context.Background(), map[string]any{"path": "Capture AM.txt"})
	if err != nil || !strings.Contains(content, "space variant") {
		t.Fatalf("content=%q err=%v", content, err)
	}
}

func TestReadToolAcceptsFileURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("file url"), 0o600); err != nil {
		t.Fatal(err)
	}
	content, err := NewReadTool(dir).Execute(context.Background(), map[string]any{"path": "file://" + path})
	if err != nil || content != "file url" {
		t.Fatalf("content=%q err=%v", content, err)
	}
}

func TestReadToolReturnsImagesThroughRichResults(t *testing.T) {
	dir := t.TempDir()
	data := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	path := filepath.Join(dir, "image.png")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	var rich agent.RichTool = NewReadTool(dir)
	result, err := rich.ExecuteRich(context.Background(), map[string]any{"path": "image.png"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Text, "Read image file [image/png]") || len(result.Images) != 1 || !strings.HasPrefix(result.Images[0], "data:image/png;base64,") {
		t.Fatalf("result=%#v", result)
	}
}

func TestReadToolRejectsAlreadyAbortedContext(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := NewReadTool(dir).Execute(ctx, map[string]any{"path": "file.txt"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context.Canceled", err)
	}
}
