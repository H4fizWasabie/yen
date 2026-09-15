package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/conversation"
	providerpkg "github.com/H4fizWasabie/yen/internal/provider"
	"github.com/H4fizWasabie/yen/internal/runtime"
	"github.com/H4fizWasabie/yen/internal/session"
)

type rpcProvider struct{}

func (rpcProvider) Next(context.Context, []agent.Message, []string) (agent.Response, error) {
	return agent.Response{Text: "rpc reply", StopReason: "stop"}, nil
}

type blockingRPCProvider struct {
	started chan struct{}
	release chan struct{}
}

func (p blockingRPCProvider) Next(ctx context.Context, _ []agent.Message, _ []string) (agent.Response, error) {
	select {
	case p.started <- struct{}{}:
	default:
	}
	select {
	case <-p.release:
		return agent.Response{Text: "rpc reply", StopReason: "stop"}, nil
	case <-ctx.Done():
		return agent.Response{}, ctx.Err()
	}
}

func TestServePromptStateAndMessagesUseJSONLProtocol(t *testing.T) {
	dir := t.TempDir()
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.New(queue, rpcProvider{}, nil)
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	link := conversation.Link{Adapter: "rpc", AdapterKey: "test", ConversationID: "conv-rpc", WorkspaceID: dir}
	input := strings.NewReader(`{"id":"1","type":"prompt","message":"hello"}` + "\n" + `{"id":"2","type":"get_messages"}` + "\n" + `{"id":"3","type":"get_tree"}` + "\n" + `{"id":"4","type":"get_last_assistant_text"}` + "\n")
	var output bytes.Buffer
	server := Server{Runner: runner, Link: link}
	if err := server.Serve(context.Background(), input, &output); err != nil {
		t.Fatal(err)
	}
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	if len(records) < 6 || records[0]["type"] != "response" || records[0]["id"] != "1" {
		t.Fatalf("records=%#v", records)
	}
	if !strings.Contains(output.String(), `"event":"done"`) || !strings.Contains(output.String(), "rpc reply") {
		t.Fatalf("output=%s", output.String())
	}
}

func TestServeRejectsUnknownCommand(t *testing.T) {
	dir := t.TempDir()
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	server := Server{Runner: runtime.New(queue, rpcProvider{}, nil), Link: conversation.Link{ConversationID: "conv-rpc"}}
	var output bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(`{"type":"nope"}`+"\n"), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"success":false`) || !strings.Contains(output.String(), "unsupported rpc command") {
		t.Fatalf("output=%s", output.String())
	}
}

func TestSteerAndFollowUpRouteToActiveTurn(t *testing.T) {
	dir := t.TempDir()
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	provider := blockingRPCProvider{started: make(chan struct{}, 1), release: make(chan struct{})}
	runner := runtime.New(queue, provider, nil)
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	server := Server{Runner: runner, Link: conversation.Link{ConversationID: "active-rpc", WorkspaceID: dir}}
	var output bytes.Buffer
	if err := server.handle(context.Background(), &output, command{Type: "prompt", Message: "start"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("provider did not start")
	}
	if err := server.handle(context.Background(), &output, command{Type: "steer", Message: "change direction"}); err != nil {
		t.Fatal(err)
	}
	if err := server.handle(context.Background(), &output, command{Type: "follow_up", Message: "continue later"}); err != nil {
		t.Fatal(err)
	}
	close(provider.release)
	server.wg.Wait()
	if !strings.Contains(output.String(), `"command":"steer"`) || !strings.Contains(output.String(), `"command":"follow_up"`) {
		t.Fatalf("output=%s", output.String())
	}
}

func TestBranchCommandPersistsActiveLeaf(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conv-rpc.jsonl")
	saved := session.New(path, session.Header{ID: "conv-rpc", ConversationID: "conv-rpc", CWD: dir})
	if _, err := saved.Append(session.Message{Role: "user", Content: "hello"}); err != nil {
		t.Fatal(err)
	}
	entryID, err := saved.Append(session.Message{Role: "assistant", Content: "reply"})
	if err != nil {
		t.Fatal(err)
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.New(queue, rpcProvider{}, nil)
	runner.SessionPath = func(conversation.Turn) string { return path }
	server := Server{Runner: runner, Link: conversation.Link{ConversationID: "conv-rpc", WorkspaceID: dir}}
	var output bytes.Buffer
	if err := server.handle(context.Background(), &output, command{ID: "1", Type: "branch", EntryID: entryID}); err != nil {
		t.Fatal(err)
	}
	opened, err := session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if opened.LeafID() == entryID || !strings.Contains(output.String(), `"success":true`) {
		t.Fatalf("leaf=%q output=%s", opened.LeafID(), output.String())
	}
}

func TestSetSessionNamePersistsAndReturnsName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "named.jsonl")
	created := session.New(path, session.Header{ID: "named", ConversationID: "named", CWD: dir})
	if _, err := created.Append(session.Message{Role: "assistant", Content: "ready"}); err != nil {
		t.Fatal(err)
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.New(queue, rpcProvider{}, nil)
	runner.SessionPath = func(conversation.Turn) string { return path }
	server := Server{Runner: runner, Link: conversation.Link{ConversationID: "named", WorkspaceID: dir}}
	var output bytes.Buffer
	if err := server.handle(context.Background(), &output, command{ID: "1", Type: "set_session_name", Name: "  nightly\ncheck  "}); err != nil {
		t.Fatal(err)
	}
	opened, err := session.Open(path)
	if err != nil || opened.SessionName() != "nightly check" || !strings.Contains(output.String(), `"name":"nightly check"`) {
		t.Fatalf("name=%q output=%s err=%v", opened.SessionName(), output.String(), err)
	}
}

func TestForkCommandCreatesDurableSessionCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source.jsonl")
	saved := session.New(path, session.Header{ID: "source", ConversationID: "source", CWD: dir})
	if _, err := saved.Append(session.Message{Role: "assistant", Content: "ready"}); err != nil {
		t.Fatal(err)
	}
	entryID := saved.LeafID()
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.New(queue, rpcProvider{}, nil)
	runner.SessionPath = func(conversation.Turn) string { return path }
	forkPath := filepath.Join(dir, "fork.jsonl")
	server := Server{Runner: runner, Link: conversation.Link{ConversationID: "source", WorkspaceID: dir}}
	var output bytes.Buffer
	if err := server.handle(context.Background(), &output, command{ID: "1", Type: "fork", EntryID: entryID, Path: forkPath}); err != nil {
		t.Fatal(err)
	}
	forked, err := session.Open(forkPath)
	if err != nil || len(forked.Messages()) != 1 || !strings.Contains(output.String(), `"success":true`) {
		t.Fatalf("messages=%#v output=%s err=%v", forked.Messages(), output.String(), err)
	}
}

func TestImportSessionCommandCopiesExternalSession(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "current.jsonl")
	current := session.New(currentPath, session.Header{ID: "current", ConversationID: "current", CWD: dir})
	if _, err := current.Append(session.Message{Role: "assistant", Content: "current"}); err != nil {
		t.Fatal(err)
	}
	sourceDir := t.TempDir()
	sourcePath := filepath.Join(sourceDir, "external.jsonl")
	external := session.New(sourcePath, session.Header{ID: "external", ConversationID: "external", CWD: sourceDir})
	if _, err := external.Append(session.Message{Role: "assistant", Content: "external"}); err != nil {
		t.Fatal(err)
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.New(queue, rpcProvider{}, nil)
	runner.SessionPath = func(conversation.Turn) string { return currentPath }
	server := Server{Runner: runner, Link: conversation.Link{ConversationID: "current", WorkspaceID: dir}}
	var output bytes.Buffer
	if err := server.handle(context.Background(), &output, command{ID: "1", Type: "import_session", Path: sourcePath}); err != nil {
		t.Fatal(err)
	}
	imported, err := session.Open(filepath.Join(dir, "external.jsonl"))
	if err != nil || len(imported.Messages()) != 1 || imported.Messages()[0].Content != "external" || !strings.Contains(output.String(), `"success":true`) {
		t.Fatalf("messages=%#v output=%s err=%v", imported.Messages(), output.String(), err)
	}
	active, err := runner.OpenSession(server.currentLink())
	if err != nil || active.Path() != filepath.Join(dir, "external.jsonl") || active.Messages()[0].Content != "external" {
		t.Fatalf("active=%#v err=%v", active, err)
	}
}

func TestSessionStatsAndForkMessagesCommands(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stats.jsonl")
	saved := session.New(path, session.Header{ID: "stats", ConversationID: "stats", CWD: dir})
	if _, err := saved.Append(session.Message{Role: "user", Content: "choose fork"}); err != nil {
		t.Fatal(err)
	}
	if _, err := saved.Append(session.Message{Role: "assistant", Content: "done", Usage: &session.Usage{Input: 2, Output: 3, TotalTokens: 5}}); err != nil {
		t.Fatal(err)
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.New(queue, rpcProvider{}, nil)
	runner.SessionPath = func(conversation.Turn) string { return path }
	server := Server{Runner: runner, Link: conversation.Link{ConversationID: "stats", WorkspaceID: dir}}
	var output bytes.Buffer
	if err := server.handle(context.Background(), &output, command{ID: "1", Type: "get_session_stats"}); err != nil {
		t.Fatal(err)
	}
	if err := server.handle(context.Background(), &output, command{ID: "2", Type: "get_fork_messages"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"userMessages":1`) || !strings.Contains(output.String(), "choose fork") || !strings.Contains(output.String(), `"total":5`) {
		t.Fatalf("output=%s", output.String())
	}
}

func TestGetEntriesReturnsLeafAndRejectsUnknownSince(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "entries.jsonl")
	saved := session.New(path, session.Header{ID: "entries", ConversationID: "entries", CWD: dir})
	if _, err := saved.Append(session.Message{Role: "user", Content: "hello"}); err != nil {
		t.Fatal(err)
	}
	if _, err := saved.Append(session.Message{Role: "assistant", Content: "world"}); err != nil {
		t.Fatal(err)
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	server := Server{Runner: runtime.New(queue, rpcProvider{}, nil), Link: conversation.Link{ConversationID: "entries", WorkspaceID: dir}}
	var output bytes.Buffer
	if err := server.handle(context.Background(), &output, command{Type: "get_entries"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"leafId"`) {
		t.Fatalf("response=%s", output.String())
	}
	if err := server.handle(context.Background(), &output, command{Type: "get_entries", Since: "missing"}); err == nil || !strings.Contains(err.Error(), "entry not found") {
		t.Fatalf("unknown since error=%v", err)
	}
}

func TestSetModelCommandReplacesConfiguredProvider(t *testing.T) {
	dir := t.TempDir()
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.New(queue, rpcProvider{}, nil)
	server := Server{Runner: runner, Link: conversation.Link{ConversationID: "model", WorkspaceID: dir}}
	var output bytes.Buffer
	if err := server.handle(context.Background(), &output, command{ID: "1", Type: "set_model", Provider: "anthropic", Model: "claude-test"}); err != nil {
		t.Fatal(err)
	}
	if name, model := providerpkg.Describe(runner.Provider); name != "anthropic" || model != "claude-test" || !strings.Contains(output.String(), `"success":true`) {
		t.Fatalf("provider=%q model=%q output=%s", name, model, output.String())
	}
}

func TestThinkingLevelCommandsUpdateState(t *testing.T) {
	dir := t.TempDir()
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.New(queue, providerpkg.NewOpenAICompletions("http://fixture", "key", "model"), nil)
	server := Server{Runner: runner, Link: conversation.Link{ConversationID: "thinking", WorkspaceID: dir}}
	var output bytes.Buffer
	if err := server.handle(context.Background(), &output, command{ID: "1", Type: "set_thinking_level", Level: "high"}); err != nil {
		t.Fatal(err)
	}
	if err := server.handle(context.Background(), &output, command{ID: "2", Type: "cycle_thinking_level"}); err != nil {
		t.Fatal(err)
	}
	if providerpkg.ThinkingLevel(runner.Provider) != "xhigh" || !strings.Contains(output.String(), `"level":"xhigh"`) {
		t.Fatalf("level=%q output=%s", providerpkg.ThinkingLevel(runner.Provider), output.String())
	}
}

func TestRetryControlCommandsUpdateProvider(t *testing.T) {
	dir := t.TempDir()
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.New(queue, providerpkg.NewOpenAICompletions("http://fixture", "key", "model"), nil)
	server := Server{Runner: runner, Link: conversation.Link{ConversationID: "retry", WorkspaceID: dir}}
	var output bytes.Buffer
	enabled := true
	if err := server.handle(context.Background(), &output, command{ID: "1", Type: "set_auto_retry", Enabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	if !providerpkg.RetryEnabled(runner.Provider) || !strings.Contains(output.String(), `"enabled":true`) {
		t.Fatalf("provider=%#v output=%s", runner.Provider, output.String())
	}
}

func TestQueueModeAndCommandDiscovery(t *testing.T) {
	dir := t.TempDir()
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	server := Server{Runner: runtime.New(queue, rpcProvider{}, nil), Link: conversation.Link{ConversationID: "commands"}}
	var output bytes.Buffer
	if err := server.handle(context.Background(), &output, command{Type: "set_steering_mode", Mode: "all"}); err != nil {
		t.Fatal(err)
	}
	if err := server.handle(context.Background(), &output, command{Type: "set_follow_up_mode", Mode: "one-at-a-time"}); err != nil {
		t.Fatal(err)
	}
	if err := server.handle(context.Background(), &output, command{Type: "get_state"}); err != nil {
		t.Fatal(err)
	}
	if err := server.handle(context.Background(), &output, command{Type: "get_commands"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"steeringMode":"all"`) || !strings.Contains(output.String(), `"name":"compact"`) {
		t.Fatalf("output=%s", output.String())
	}
}

func TestBashCommandReturnsOutputAndLogsWorkingNote(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	created := session.New(path, session.Header{ID: "bash", ConversationID: "bash", CWD: dir})
	if _, err := created.Append(session.Message{Role: "user", Content: "run"}); err != nil {
		t.Fatal(err)
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.New(queue, rpcProvider{}, nil)
	runner.SessionPath = func(conversation.Turn) string { return path }
	server := Server{Runner: runner, Link: conversation.Link{ConversationID: "bash", WorkspaceID: dir}}
	var output bytes.Buffer
	if err := server.handle(context.Background(), &output, command{Type: "bash", Command: "printf hello"}); err != nil {
		t.Fatal(err)
	}
	opened, err := session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	messages := opened.Messages()
	if !strings.Contains(output.String(), `"output":"hello"`) || !strings.Contains(opened.WorkingNote(), "ran: printf hello") {
		t.Fatalf("output=%s note=%q", output.String(), opened.WorkingNote())
	}
	if len(messages) != 1 || messages[0].Role != "bashExecution" || messages[0].Command != "printf hello" || messages[0].Output != "hello" {
		t.Fatalf("messages=%#v", messages)
	}
}

func TestSessionLifecycleCommandsCreateCloneAndExport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	created := session.New(path, session.Header{ID: "root", ConversationID: "root", CWD: dir})
	if _, err := created.Append(session.Message{Role: "user", Content: "<hello>"}); err != nil {
		t.Fatal(err)
	}
	if _, err := created.Append(session.Message{Role: "assistant", Content: "world"}); err != nil {
		t.Fatal(err)
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.New(queue, rpcProvider{}, nil)
	runner.SessionPath = func(turn conversation.Turn) string {
		if turn.ConversationID == "root" {
			return path
		}
		return filepath.Join(dir, turn.ConversationID+".jsonl")
	}
	server := Server{Runner: runner, Link: conversation.Link{ConversationID: "root", WorkspaceID: dir}}
	var output bytes.Buffer
	if err := server.handle(context.Background(), &output, command{Type: "clone"}); err != nil {
		t.Fatal(err)
	}
	if err := server.handle(context.Background(), &output, command{Type: "export_html", OutputPath: filepath.Join(dir, "session.html")}); err != nil {
		t.Fatal(err)
	}
	if err := server.handle(context.Background(), &output, command{Type: "new_session"}); err != nil {
		t.Fatal(err)
	}
	if err := server.handle(context.Background(), &output, command{Type: "get_state"}); err != nil {
		t.Fatal(err)
	}
	exported, err := os.ReadFile(filepath.Join(dir, "session.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(exported), "&lt;hello&gt;") || !strings.Contains(output.String(), `"success":true`) {
		t.Fatalf("export/output=%s html=%s", output.String(), exported)
	}
}
