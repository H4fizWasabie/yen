package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/auth"
	"github.com/H4fizWasabie/yen/internal/conversation"
	"github.com/H4fizWasabie/yen/internal/provider"
	"github.com/H4fizWasabie/yen/internal/runtime"
	"github.com/H4fizWasabie/yen/internal/session"
	"github.com/H4fizWasabie/yen/internal/settings"
)

func TestRunRejectsMissingPrompt(t *testing.T) {
	var stderr bytes.Buffer
	if code := run(nil, &bytes.Buffer{}, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

func TestInteractiveRunRendersScrollbackStatusAndInput(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("YEN_SESSION_FILE", filepath.Join(dir, "session.jsonl"))
	t.Setenv("YEN_DATA_DIR", dir)
	var stdout, stderr bytes.Buffer
	if code := runWithInput([]string{"-i"}, strings.NewReader("/session\n/quit\n"), &stdout, &stderr); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{"\x1b[2J\x1b[H", "Scrollback", "Input", "Session Info", "> "} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q: %q", want, output)
		}
	}
}

func TestRunMigrationIsExplicit(t *testing.T) {
	dir := t.TempDir()
	source, target := filepath.Join(dir, "source"), filepath.Join(dir, "target")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "legacy.md"), []byte("---\nid: legacy\ntype: semantic\nsubject: legacy note\nat: 2026-01-01T00:00:00Z\nedges: []\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-migrate-semantic", source, "-memory-dir", target, "-scope", "owner", "-owner", "owner-1"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "migrated 1 semantic nodes") {
		t.Fatalf("stdout=%q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(source, "legacy.md")); err != nil {
		t.Fatalf("source changed: %v", err)
	}
}

func TestRunPersistsProviderErrorAndReturnsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "provider failed", http.StatusBadGateway)
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "session.jsonl")
	t.Setenv("YEN_SESSION_FILE", path)
	t.Setenv("YEN_CANONICAL_CONVERSATION_ID", "conv-test")
	t.Setenv("YEN_OPENAI_BASE_URL", server.URL)

	var stderr bytes.Buffer
	if code := run([]string{"-p", "hello"}, &bytes.Buffer{}, &stderr); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}

	current, err := session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	messages := current.Messages()
	if len(messages) != 2 || messages[0].Role != "user" || messages[1].Role != "assistant" || messages[1].StopReason != "error" {
		t.Fatalf("messages = %#v", messages)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), `"conversationId":"conv-`) {
		t.Fatalf("session identity missing: err=%v data=%s", err, data)
	}
}

func TestRunEndToEndToolTurnUsesSharedRunnerAndMemory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("fixture README\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldCWD)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if requests.Add(1) == 1 {
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"read-1","function":{"name":"read","arguments":"{\"path\":"}}]}}]}`)
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"README.md\"}"}}]},"finish_reason":"tool_calls"}]}`)
		} else {
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"fixture read"},"finish_reason":"stop"}]}`)
		}
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()
	dataDir := filepath.Join(dir, "data")
	path := filepath.Join(dataDir, "sessions", "session.jsonl")
	t.Setenv("YEN_SESSION_FILE", path)
	t.Setenv("YEN_DATA_DIR", dataDir)
	t.Setenv("YEN_CANONICAL_CONVERSATION_ID", "conv-test")
	t.Setenv("YEN_OPENAI_BASE_URL", server.URL)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-p", "read README"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if stdout.String() != "fixture read\n" {
		t.Fatalf("stdout=%q", stdout.String())
	}
	stored, err := session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Messages()) != 4 {
		t.Fatalf("messages=%#v", stored.Messages())
	}
	if data, err := os.ReadFile(filepath.Join(dataDir, "consolidation-checkpoints.json")); err != nil || !strings.Contains(string(data), "conv-") {
		t.Fatalf("checkpoint err=%v data=%s", err, data)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "conversation-queue.jsonl")); err != nil {
		t.Fatalf("shared queue missing: %v", err)
	}
}

func TestInteractiveSessionCommands(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	current := session.New(path, session.Header{ID: "session-1", ConversationID: "conv-1"})
	runner := &runtime.Runner{}
	link := conversation.Link{ConversationID: "conv-1"}
	var output bytes.Buffer
	activePath := current.Path()
	handled, err := handleInteractiveCommand("/name evening", current, runner, link, &activePath, &output)
	if err != nil || !handled || current.SessionName() != "evening" {
		t.Fatalf("name command handled=%v err=%v name=%q", handled, err, current.SessionName())
	}
	handled, err = handleInteractiveCommand("/session", current, runner, link, &activePath, &output)
	if err != nil || !handled || !strings.Contains(output.String(), "session-1") {
		t.Fatalf("session command handled=%v err=%v output=%q", handled, err, output.String())
	}
	handled, err = handleInteractiveCommand("/working-note", current, runner, link, &activePath, &output)
	if err != nil || !handled || !strings.Contains(output.String(), "Working Note is empty") {
		t.Fatalf("working note command handled=%v err=%v output=%q", handled, err, output.String())
	}
	if _, err := current.StoreArtifact("fixture", "result.txt", []byte("artifact")); err != nil {
		t.Fatal(err)
	}
	handled, err = handleInteractiveCommand("/tree", current, runner, link, &activePath, &output)
	if err != nil || !handled || !strings.Contains(output.String(), "session-1") {
		t.Fatalf("tree command handled=%v err=%v output=%q", handled, err, output.String())
	}
	handled, err = handleInteractiveCommand("/artifacts", current, runner, link, &activePath, &output)
	if err != nil || !handled || !strings.Contains(output.String(), "fixture") {
		t.Fatalf("artifacts command handled=%v err=%v output=%q", handled, err, output.String())
	}
}

func TestInteractiveCopyCopiesLastAssistantMessage(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(dir, "clipboard.txt")
	program := filepath.Join(bin, "xclip")
	if err := os.WriteFile(program, []byte("#!/bin/sh\ncat > \"$YEN_CLIPBOARD_CAPTURE\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("YEN_CLIPBOARD_CAPTURE", capture)
	current := session.New(filepath.Join(dir, "session.jsonl"), session.Header{ID: "session-1"})
	if _, err := current.Append(session.Message{Role: "assistant", Content: []session.ContentPart{{Type: "thinking", Text: "hidden"}, {Type: "text", Text: " copied answer "}}}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	activePath := current.Path()
	handled, err := handleInteractiveCommand("/copy", current, &runtime.Runner{}, conversation.Link{}, &activePath, &output)
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v output=%q", handled, err, output.String())
	}
	data, err := os.ReadFile(capture)
	if err != nil || string(data) != "copied answer" {
		t.Fatalf("clipboard=%q err=%v output=%q", data, err, output.String())
	}
}

func TestInteractiveBashCommandsPersistOutputAndExclusion(t *testing.T) {
	dir := t.TempDir()
	current := session.New(filepath.Join(dir, "session.jsonl"), session.Header{ID: "session-1", CWD: dir})
	var output bytes.Buffer
	activePath := current.Path()
	handled, err := handleInteractiveCommand("!!printf hidden", current, &runtime.Runner{}, conversation.Link{}, &activePath, &output)
	if err != nil || !handled || !strings.Contains(output.String(), "hidden") {
		t.Fatalf("handled=%v err=%v output=%q", handled, err, output.String())
	}
	messages := current.Messages()
	if len(messages) != 1 || messages[0].Role != "bashExecution" || !messages[0].ExcludeFromContext || messages[0].Command != "printf hidden" {
		t.Fatalf("messages=%#v", messages)
	}
}

func TestInteractiveBashCommandPersistsExitMetadata(t *testing.T) {
	dir := t.TempDir()
	current := session.New(filepath.Join(dir, "session.jsonl"), session.Header{ID: "session-1", CWD: dir})
	var output bytes.Buffer
	activePath := current.Path()
	handled, err := handleInteractiveCommand("!printf failed >&2; exit 7", current, &runtime.Runner{}, conversation.Link{}, &activePath, &output)
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v output=%q", handled, err, output.String())
	}
	messages := current.Messages()
	if len(messages) != 1 || messages[0].ExitCode == nil || *messages[0].ExitCode != 7 || messages[0].Output != "failed" {
		t.Fatalf("messages=%#v", messages)
	}
}

func TestInteractiveSessionExportAndImport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	current := session.New(path, session.Header{ID: "session-1", CWD: dir})
	if _, err := current.Append(session.Message{Role: "user", Content: "before"}); err != nil {
		t.Fatal(err)
	}
	if _, err := current.Append(session.Message{Role: "assistant", Content: "reply"}); err != nil {
		t.Fatal(err)
	}
	exportPath := filepath.Join(dir, "copy.jsonl")
	var output bytes.Buffer
	activePath := current.Path()
	handled, err := handleInteractiveCommand("/export "+exportPath, current, &runtime.Runner{}, conversation.Link{}, &activePath, &output)
	if err != nil || !handled {
		t.Fatalf("export handled=%v err=%v", handled, err)
	}
	importSource := filepath.Join(dir, "incoming.jsonl")
	incoming := session.New(importSource, session.Header{ID: "incoming", CWD: dir})
	if _, err := incoming.Append(session.Message{Role: "user", Content: "after"}); err != nil {
		t.Fatal(err)
	}
	if _, err := incoming.Append(session.Message{Role: "assistant", Content: "reply"}); err != nil {
		t.Fatal(err)
	}
	handled, err = handleInteractiveCommand("/import "+importSource, current, &runtime.Runner{}, conversation.Link{}, &activePath, &output)
	if err != nil || !handled || len(current.Messages()) != 2 || current.Messages()[0].Content != "after" {
		t.Fatalf("import handled=%v err=%v messages=%#v", handled, err, current.Messages())
	}
}

func TestInteractiveCloneCreatesFork(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	current := session.New(path, session.Header{ID: "session-1", CWD: dir})
	if _, err := current.Append(session.Message{Role: "user", Content: "before"}); err != nil {
		t.Fatal(err)
	}
	if _, err := current.Append(session.Message{Role: "assistant", Content: "reply"}); err != nil {
		t.Fatal(err)
	}
	clonePath := filepath.Join(dir, "clone.jsonl")
	var output bytes.Buffer
	activePath := current.Path()
	handled, err := handleInteractiveCommand("/clone "+clonePath, current, &runtime.Runner{}, conversation.Link{}, &activePath, &output)
	if err != nil || !handled || !strings.Contains(output.String(), clonePath) {
		t.Fatalf("handled=%v err=%v output=%q", handled, err, output.String())
	}
	cloned, err := session.Open(clonePath)
	if err != nil || len(cloned.Messages()) != 2 {
		t.Fatalf("clone=%#v err=%v", cloned, err)
	}
	if activePath != clonePath || current.Path() != clonePath {
		t.Fatalf("active path=%q current path=%q", activePath, current.Path())
	}
}

func TestInteractiveForkAndResumeSwitchActiveSession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	current := session.New(path, session.Header{ID: "session-1", CWD: dir})
	root, err := current.Append(session.Message{Role: "user", Content: "root"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := current.Append(session.Message{Role: "assistant", Content: "reply"}); err != nil {
		t.Fatal(err)
	}
	forkPath := filepath.Join(dir, "fork.jsonl")
	var output bytes.Buffer
	activePath := current.Path()
	handled, err := handleInteractiveCommand("/fork "+root+" "+forkPath, current, &runtime.Runner{}, conversation.Link{}, &activePath, &output)
	if err != nil || !handled || activePath != forkPath || len(current.Messages()) != 1 {
		t.Fatalf("fork handled=%v err=%v path=%q messages=%#v", handled, err, activePath, current.Messages())
	}
	handled, err = handleInteractiveCommand("/resume "+path, current, &runtime.Runner{}, conversation.Link{}, &activePath, &output)
	if err != nil || !handled || activePath != path || len(current.Messages()) != 2 {
		t.Fatalf("resume handled=%v err=%v path=%q messages=%#v", handled, err, activePath, current.Messages())
	}
}

func TestInteractiveNewSessionSwitchesActivePath(t *testing.T) {
	dir := t.TempDir()
	current := session.New(filepath.Join(dir, "session.jsonl"), session.Header{ID: "old", ConversationID: "conv-1", CWD: dir})
	if _, err := current.Append(session.Message{Role: "user", Content: "old"}); err != nil {
		t.Fatal(err)
	}
	if _, err := current.Append(session.Message{Role: "assistant", Content: "reply"}); err != nil {
		t.Fatal(err)
	}
	activePath := current.Path()
	var output bytes.Buffer
	handled, err := handleInteractiveCommand("/new", current, &runtime.Runner{}, conversation.Link{}, &activePath, &output)
	if err != nil || !handled || current.Path() == filepath.Join(dir, "session.jsonl") || len(current.Messages()) != 0 || activePath != current.Path() {
		t.Fatalf("handled=%v err=%v path=%q active=%q messages=%#v", handled, err, current.Path(), activePath, current.Messages())
	}
}

func TestInteractiveProviderAndTrustCommands(t *testing.T) {
	dir := t.TempDir()
	current := session.New(filepath.Join(dir, "session.jsonl"), session.Header{ID: "session-1", ConversationID: "conv-1", CWD: dir})
	runner := &runtime.Runner{Provider: provider.NewOpenAICompletions("http://fixture", "key", "old-model")}
	link := conversation.Link{ConversationID: "conv-1", WorkspaceID: dir}
	var output bytes.Buffer
	activePath := current.Path()
	handled, err := handleInteractiveCommand("/model new-model", current, runner, link, &activePath, &output)
	if err != nil || !handled || providerModel(runner.Provider) != "new-model" {
		t.Fatalf("model handled=%v err=%v output=%q", handled, err, output.String())
	}
	handled, err = handleInteractiveCommand("/thinking high", current, runner, link, &activePath, &output)
	if err != nil || !handled || provider.ThinkingLevel(runner.Provider) != "high" {
		t.Fatalf("thinking handled=%v err=%v output=%q", handled, err, output.String())
	}
	handled, err = handleInteractiveCommand("/retry on", current, runner, link, &activePath, &output)
	if err != nil || !handled || !provider.RetryEnabled(runner.Provider) {
		t.Fatalf("retry handled=%v err=%v output=%q", handled, err, output.String())
	}
	t.Setenv("YEN_TRUST_PROJECT", "")
	t.Setenv("YEN_TRUST_FILE", filepath.Join(dir, "trusted-projects.json"))
	handled, err = handleInteractiveCommand("/trust", current, runner, link, &activePath, &output)
	if err != nil || !handled || !settings.IsTrusted(dir) {
		t.Fatalf("trust handled=%v err=%v output=%q", handled, err, output.String())
	}
}

func TestInteractiveScopedModelsCommandListsProviderCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/models" {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"model-a"},{"id":"model-b"}]}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	current := session.New(filepath.Join(dir, "session.jsonl"), session.Header{ID: "session-1"})
	runner := &runtime.Runner{Provider: provider.NewOpenAICompletions(server.URL, "key", "model-a")}
	var output bytes.Buffer
	activePath := current.Path()
	handled, err := handleInteractiveCommand("/scoped-models", current, runner, conversation.Link{}, &activePath, &output)
	if err != nil || !handled || !strings.Contains(output.String(), `"model-b"`) {
		t.Fatalf("handled=%v err=%v output=%q", handled, err, output.String())
	}
}

func TestInteractiveLogoutRemovesStoredProviderCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if _, err := auth.Open(path).Modify("openrouter", func(*auth.Credential) (*auth.Credential, error) {
		return &auth.Credential{Type: "api_key", Key: "secret"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Open(path).Modify("anthropic", func(*auth.Credential) (*auth.Credential, error) {
		return &auth.Credential{Type: "oauth", Access: "access"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YEN_AUTH_FILE", path)

	current := session.New(filepath.Join(t.TempDir(), "session.jsonl"), session.Header{ID: "session-1"})
	var output bytes.Buffer
	activePath := current.Path()
	handled, err := handleInteractiveCommand("/logout", current, &runtime.Runner{}, conversation.Link{}, &activePath, &output)
	if err != nil || !handled || !strings.HasPrefix(output.String(), `[{"providerId":"anthropic"`) {
		t.Fatalf("list handled=%v err=%v output=%q", handled, err, output.String())
	}
	output.Reset()
	handled, err = handleInteractiveCommand("/logout openrouter", current, &runtime.Runner{}, conversation.Link{}, &activePath, &output)
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v output=%q", handled, err, output.String())
	}
	if _, ok, err := auth.Open(path).Read("openrouter"); err != nil || ok {
		t.Fatalf("credential still present: ok=%v err=%v", ok, err)
	}
}

func TestInteractiveAnthropicLoginRequiresAuthFile(t *testing.T) {
	t.Setenv("YEN_AUTH_FILE", "")
	current := session.New(filepath.Join(t.TempDir(), "session.jsonl"), session.Header{ID: "session-1"})
	var output bytes.Buffer
	activePath := current.Path()
	handled, err := handleInteractiveCommand("/login anthropic", current, &runtime.Runner{}, conversation.Link{}, &activePath, &output)
	if !handled || err == nil || !strings.Contains(err.Error(), "YEN_AUTH_FILE is required") {
		t.Fatalf("handled=%v err=%v output=%q", handled, err, output.String())
	}
}

func TestInteractiveCodexBrowserLoginRequiresAuthFile(t *testing.T) {
	t.Setenv("YEN_AUTH_FILE", "")
	current := session.New(filepath.Join(t.TempDir(), "session.jsonl"), session.Header{ID: "session-1"})
	var output bytes.Buffer
	activePath := current.Path()
	handled, err := handleInteractiveCommand("/login openai-codex browser", current, &runtime.Runner{}, conversation.Link{}, &activePath, &output)
	if !handled || err == nil || !strings.Contains(err.Error(), "YEN_AUTH_FILE is required") {
		t.Fatalf("handled=%v err=%v output=%q", handled, err, output.String())
	}
}

func TestInteractiveBedrockLoginStoresSelectedCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	t.Setenv("YEN_AUTH_FILE", path)
	current := session.New(filepath.Join(t.TempDir(), "session.jsonl"), session.Header{ID: "session-1"})
	var output bytes.Buffer
	activePath := current.Path()
	handled, err := handleInteractiveCommand("/login amazon-bedrock aws-profile stored-profile", current, &runtime.Runner{}, conversation.Link{}, &activePath, &output)
	if err != nil || !handled || !strings.Contains(output.String(), "Logged in: amazon-bedrock") {
		t.Fatalf("handled=%v err=%v output=%q", handled, err, output.String())
	}
	credential, ok, err := auth.Open(path).Read("amazon-bedrock")
	if err != nil || !ok || credential.Type != "api_key" || credential.Key != "" || credential.Env["AWS_PROFILE"] != "stored-profile" {
		t.Fatalf("credential=%#v ok=%v err=%v", credential, ok, err)
	}
}

func TestInteractiveVertexLoginStoresSelectedCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	t.Setenv("YEN_AUTH_FILE", path)
	current := session.New(filepath.Join(t.TempDir(), "session.jsonl"), session.Header{ID: "session-1"})
	var output bytes.Buffer
	activePath := current.Path()
	handled, err := handleInteractiveCommand("/login google-vertex service-account project-1 us-central1 /tmp/service-account.json", current, &runtime.Runner{}, conversation.Link{}, &activePath, &output)
	if err != nil || !handled || !strings.Contains(output.String(), "Logged in: google-vertex") {
		t.Fatalf("handled=%v err=%v output=%q", handled, err, output.String())
	}
	credential, ok, err := auth.Open(path).Read("google-vertex")
	if err != nil || !ok || credential.Type != "api_key" || credential.Key != "" || credential.Env["GOOGLE_CLOUD_PROJECT"] != "project-1" || credential.Env["GOOGLE_CLOUD_LOCATION"] != "us-central1" || credential.Env["GOOGLE_APPLICATION_CREDENTIALS"] != "/tmp/service-account.json" {
		t.Fatalf("credential=%#v ok=%v err=%v", credential, ok, err)
	}
}

func TestInteractiveSettingsAndReloadCommands(t *testing.T) {
	dir := t.TempDir()
	current := session.New(filepath.Join(dir, "session.jsonl"), session.Header{ID: "session-1", CWD: dir})
	if _, err := current.Append(session.Message{Role: "user", Content: "hello"}); err != nil {
		t.Fatal(err)
	}
	if _, err := current.Append(session.Message{Role: "assistant", Content: "reply"}); err != nil {
		t.Fatal(err)
	}
	activePath := current.Path()
	var output bytes.Buffer
	handled, err := handleInteractiveCommand("/settings", current, &runtime.Runner{}, conversation.Link{}, &activePath, &output)
	if err != nil || !handled || !strings.Contains(output.String(), "{") {
		t.Fatalf("settings handled=%v err=%v output=%q", handled, err, output.String())
	}
	output.Reset()
	handled, err = handleInteractiveCommand("/reload", current, &runtime.Runner{}, conversation.Link{}, &activePath, &output)
	if err != nil || !handled || !strings.Contains(output.String(), "Session reloaded") || len(current.Messages()) != 2 {
		t.Fatalf("reload handled=%v err=%v output=%q messages=%#v", handled, err, output.String(), current.Messages())
	}
}

func providerModel(value agent.Provider) string {
	_, model := provider.Describe(value)
	return model
}
