package codingagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
)

func TestHTTPSidecarLoadsAndExecutesUntrustedTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/tools":
			_, _ = w.Write([]byte(`{"tools":[{"name":"external_echo","description":"echo","inputSchema":{"type":"object"}}]}`))
		case "/execute":
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request["tool"] != "external_echo" {
				t.Fatalf("request=%#v err=%v", request, err)
			}
			_, _ = w.Write([]byte(`{"result":{"ok":true}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("YEN_HTTP_SIDECAR_URL", server.URL)
	tools := loadExternalTools()
	if len(tools) != 1 || tools[0].Name() != "external_echo" {
		t.Fatalf("tools=%#v", tools)
	}
	result, err := tools[0].Execute(context.Background(), map[string]any{"value": "ok"})
	if err != nil || !strings.Contains(result, "UNTRUSTED EXTERNAL CONTENT") || !strings.Contains(result, `"ok":true`) {
		t.Fatalf("result=%q err=%v", result, err)
	}
}

func TestMCPHTTPLoadsAndCallsTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		if request["method"] == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		result := any(map[string]any{})
		switch request["method"] {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-06-18"}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "mcp_echo", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request["id"], "result": result})
	}))
	defer server.Close()
	tools := loadMCPHTTP(server.URL)
	if len(tools) != 1 || tools[0].Name() != "mcp_echo" {
		t.Fatalf("tools=%#v", tools)
	}
	result, err := tools[0].Execute(context.Background(), map[string]any{})
	if err != nil || !strings.Contains(result, "UNTRUSTED EXTERNAL CONTENT") {
		t.Fatalf("result=%q err=%v", result, err)
	}
}

func TestDeferredExternalToolsSearchAndCall(t *testing.T) {
	tool := externalTool{name: "deferred_echo", execute: func(context.Context, map[string]any) (string, error) {
		return "called", nil
	}}
	deferred := deferExternalTools([]agent.Tool{tool})
	search, err := deferred[0].Execute(context.Background(), map[string]any{"query": "echo"})
	if err != nil || search != "deferred_echo" {
		t.Fatalf("search=%q err=%v", search, err)
	}
	result, err := deferred[1].Execute(context.Background(), map[string]any{"name": "deferred_echo", "args": map[string]any{}})
	if err != nil || result != "called" {
		t.Fatalf("result=%q err=%v", result, err)
	}
}

func TestMCPStdioLoadsAndCallsTools(t *testing.T) {
	script := `while IFS= read -r line; do case "$line" in *initialize*) echo '{"jsonrpc":"2.0","id":1,"result":{}}' ;; *tools/list*) echo '{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"stdio_echo","inputSchema":{"type":"object"}}]}}' ;; *tools/call*) echo '{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"ok"}]}}' ;; esac; done`
	tools := loadMCPStdio("sh", []string{"-c", script})
	if len(tools) != 1 || tools[0].Name() != "stdio_echo" {
		t.Fatalf("tools=%#v", tools)
	}
	result, err := tools[0].Execute(context.Background(), map[string]any{})
	if err != nil || !strings.Contains(result, "UNTRUSTED EXTERNAL CONTENT") {
		t.Fatalf("result=%q err=%v", result, err)
	}
}

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
