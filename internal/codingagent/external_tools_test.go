package codingagent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebSearchFormatsTavilyResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Fatalf("authorization=%q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"answer":"summary","results":[{"title":"Title","url":"https://example.test","content":"snippet"}]}`))
	}))
	defer server.Close()
	t.Setenv("TAVILY_API_KEY", "key")
	tool := webSearchTool{client: server.Client(), endpoint: server.URL}
	result, err := tool.Execute(context.Background(), map[string]any{"query": "yen"})
	if err != nil || !strings.Contains(result, "summary") || !strings.Contains(result, "https://example.test") {
		t.Fatalf("result=%q err=%v", result, err)
	}
}

func TestConvertDocReportsMissingMarkitdown(t *testing.T) {
	if _, err := exec.LookPath("markitdown"); err == nil {
		t.Skip("markitdown is installed")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "document.docx"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	tool := convertDocTool{cwd: dir}
	_, err := tool.Execute(context.Background(), map[string]any{"path": "document.docx"})
	if err == nil || !strings.Contains(err.Error(), "markitdown") {
		t.Fatalf("err=%v", err)
	}
}
