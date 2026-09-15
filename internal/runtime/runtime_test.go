package runtime

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/conversation"
	"github.com/H4fizWasabie/yen/internal/memory"
	"github.com/H4fizWasabie/yen/internal/session"
)

type provider struct{}

func (provider) Next(_ context.Context, _ []agent.Message, _ []string) (agent.Response, error) {
	return agent.Response{Text: "done", StopReason: "stop", Provider: "test-provider", Model: "test-model"}, nil
}

type autoConsolidationProvider struct{ calls int }

func (p *autoConsolidationProvider) Next(_ context.Context, _ []agent.Message, _ []string) (agent.Response, error) {
	p.calls++
	if p.calls == 1 {
		return agent.Response{Text: "done", StopReason: "stop"}, nil
	}
	return agent.Response{Text: `{"facts":[{"id":"f1","subject":"User prefers concise replies"}],"episode":{"summary":"Recorded a preference.","startedAt":"2026-01-01T00:00:00Z","endedAt":"2026-01-01T00:00:01Z"}}`, StopReason: "stop"}, nil
}

type contextCaptureProvider struct {
	messages []agent.Message
}

func (p *contextCaptureProvider) Next(_ context.Context, messages []agent.Message, _ []string) (agent.Response, error) {
	p.messages = append([]agent.Message(nil), messages...)
	return agent.Response{Text: "continued", StopReason: "stop"}, nil
}

type summaryProvider struct {
	response agent.Response
	seen     []agent.Message
}

func (p *summaryProvider) Next(_ context.Context, messages []agent.Message, _ []string) (agent.Response, error) {
	p.seen = append([]agent.Message(nil), messages...)
	return p.response, nil
}

type autoCompactionProvider struct {
	calls int
	seen  [][]agent.Message
}

func (p *autoCompactionProvider) Next(_ context.Context, messages []agent.Message, _ []string) (agent.Response, error) {
	p.calls++
	p.seen = append(p.seen, append([]agent.Message(nil), messages...))
	if p.calls == 1 {
		return agent.Response{Text: "automatic summary", StopReason: "stop", Usage: agent.Usage{Input: 8, Output: 3, TotalTokens: 11}}, nil
	}
	return agent.Response{Text: "continued", StopReason: "stop"}, nil
}

type overflowRecoveryProvider struct{ calls int }

func (p *overflowRecoveryProvider) Next(context.Context, []agent.Message, []string) (agent.Response, error) {
	p.calls++
	switch p.calls {
	case 1:
		return agent.Response{}, errors.New("400 input exceeds the model's maximum context length of 128 tokens")
	case 2:
		return agent.Response{Text: "overflow summary", StopReason: "stop"}, nil
	default:
		return agent.Response{Text: "recovered", StopReason: "stop"}, nil
	}
}

type lengthRecoveryProvider struct{ calls int }

func (p *lengthRecoveryProvider) Next(_ context.Context, _ []agent.Message, _ []string) (agent.Response, error) {
	p.calls++
	switch p.calls {
	case 1:
		return agent.Response{Text: "truncated", StopReason: "length"}, nil
	case 2:
		return agent.Response{Text: `{"episode":{"summary":"compacted","startedAt":"2026-01-01T00:00:00Z","endedAt":"2026-01-01T00:00:01Z"}}`, StopReason: "stop"}, nil
	default:
		return agent.Response{Text: "recovered", StopReason: "stop"}, nil
	}
}

type slowProvider struct {
	mu      sync.Mutex
	seen    []string
	started chan struct{}
	release chan struct{}
	first   bool
}

type cancelableProvider struct{ started chan struct{} }

type steeringProvider struct {
	started chan struct{}
	release chan struct{}
	mu      sync.Mutex
	seen    [][]agent.Message
	calls   int
}

func (p *steeringProvider) Next(_ context.Context, messages []agent.Message, _ []string) (agent.Response, error) {
	p.mu.Lock()
	p.seen = append(p.seen, append([]agent.Message(nil), messages...))
	p.calls++
	call := p.calls
	p.mu.Unlock()
	if call == 1 {
		close(p.started)
		<-p.release
		return agent.Response{Text: "first", StopReason: "stop"}, nil
	}
	return agent.Response{Text: "steered", StopReason: "stop"}, nil
}

func (p cancelableProvider) Next(ctx context.Context, _ []agent.Message, _ []string) (agent.Response, error) {
	close(p.started)
	<-ctx.Done()
	return agent.Response{}, ctx.Err()
}

type memoryToolProvider struct{ calls int }

func (p *memoryToolProvider) Next(_ context.Context, messages []agent.Message, tools []string) (agent.Response, error) {
	p.calls++
	if p.calls == 1 {
		return agent.Response{ToolCalls: []agent.ToolCall{{ID: "note-1", Name: "save_note", Args: map[string]any{"note": "prefers concise replies"}}}, StopReason: "toolUse"}, nil
	}
	for _, message := range messages {
		if message.Role == "tool" && !strings.Contains(message.Content, "Durable note saved") {
			return agent.Response{}, errors.New("memory tool result missing")
		}
	}
	for _, name := range tools {
		if name == "remember" {
			return agent.Response{Text: "memory saved", StopReason: "stop"}, nil
		}
	}
	return agent.Response{}, errors.New("memory tools were not exposed")
}

func (p *slowProvider) Next(_ context.Context, messages []agent.Message, _ []string) (agent.Response, error) {
	p.mu.Lock()
	last := ""
	for _, message := range messages {
		if message.Role == "user" {
			last = message.Content
		}
	}
	p.seen = append(p.seen, last)
	first := p.first
	p.first = false
	p.mu.Unlock()
	if first {
		close(p.started)
		<-p.release
	}
	return agent.Response{Text: "ok", StopReason: "stop"}, nil
}

func TestRunnerUsesCanonicalQueueAndResumesSession(t *testing.T) {
	dir := t.TempDir()
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runner := New(queue, provider{}, nil)
	runner.Memory, err = memory.OpenEngine(filepath.Join(dir, "memory"))
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Memory.Close()
	runner.Checkpoints, err = memory.OpenCheckpoints(filepath.Join(dir, "checkpoints.json"))
	if err != nil {
		t.Fatal(err)
	}
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	link := conversation.Link{Adapter: "cli", AdapterKey: "cwd", ConversationID: "conv-1", WorkspaceID: dir}
	turn, err := runner.Submit(link, "hello")
	if err != nil {
		t.Fatal(err)
	}
	gotTurn, result, err := runner.RunNext(context.Background(), link.ConversationID)
	if err != nil || gotTurn.ID != turn.ID || result.FinalText != "done" {
		t.Fatalf("turn=%#v result=%#v err=%v", gotTurn, result, err)
	}
	if pending := queue.Pending(link.ConversationID); len(pending) != 0 {
		t.Fatalf("pending=%#v", pending)
	}
	stored, err := session.Open(filepath.Join(dir, "conv-1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Messages()) != 2 {
		t.Fatalf("messages=%#v", stored.Messages())
	}
	if stored.Messages()[1].Provider != "test-provider" || stored.Messages()[1].Model != "test-model" {
		t.Fatalf("assistant metadata=%#v", stored.Messages()[1])
	}
	if runner.Checkpoints.Get(link.ConversationID).LastEntryID != turn.ID {
		t.Fatalf("checkpoint=%#v", runner.Checkpoints.Get(link.ConversationID))
	}
	if episodes, err := runner.Memory.Episodic.Recent(link.ConversationID, 8); err != nil || len(episodes) != 1 {
		t.Fatalf("episodes=%#v err=%v", episodes, err)
	}
	turn, err = runner.Submit(link, "again")
	if err != nil {
		t.Fatal(err)
	}
	_, result, err = runner.RunNext(context.Background(), link.ConversationID)
	if err != nil || result.FinalText != "done" || turn.ID == "" {
		t.Fatalf("resume result=%#v err=%v", result, err)
	}
	stored, err = session.Open(filepath.Join(dir, "conv-1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Messages()) != 4 {
		t.Fatalf("resumed messages=%#v", stored.Messages())
	}
}

func TestRunnerOptInConsolidationUsesSeparateCheckpoint(t *testing.T) {
	dir := t.TempDir()
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	provider := &autoConsolidationProvider{}
	runner := New(queue, provider, nil)
	runner.AutoConsolidate = true
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	runner.Memory, err = memory.OpenEngine(filepath.Join(dir, "memory"))
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Memory.Close()
	runner.Checkpoints, err = memory.OpenCheckpoints(filepath.Join(dir, "runtime-checkpoints.json"))
	if err != nil {
		t.Fatal(err)
	}
	link := conversation.Link{Adapter: "telegram", AdapterKey: "chat-1", ConversationID: "conv-auto", WorkspaceID: dir}
	turn, err := runner.Submit(link, "Thanks, keep replies concise")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := runner.RunSubmitted(context.Background(), turn); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls=%d, want normal turn plus consolidation", provider.calls)
	}
	hits, err := runner.Memory.Remember("concise replies", memory.Context{WorkspaceID: dir, ConversationID: link.ConversationID})
	if err != nil || len(hits) != 1 {
		t.Fatalf("memory hits=%#v err=%v", hits, err)
	}
	if got := runner.Memory.ConsolidationCheckpoints.Get(link.ConversationID).LastEntryID; got != turn.ID {
		t.Fatalf("consolidation checkpoint=%q", got)
	}
	if got := runner.Checkpoints.Get(link.ConversationID).LastEntryID; got != turn.ID {
		t.Fatalf("runtime checkpoint=%q", got)
	}
}

func TestRunnerUsesCompactionAwareContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conv-compact.jsonl")
	saved := session.New(path, session.Header{ID: "conv-compact", ConversationID: "conv-compact", CWD: dir, Channel: "cli", ChannelSessionID: dir})
	if _, err := saved.Append(session.Message{Role: "user", Content: "old"}); err != nil {
		t.Fatal(err)
	}
	if _, err := saved.Append(session.Message{Role: "assistant", Content: "old reply"}); err != nil {
		t.Fatal(err)
	}
	keptID, err := saved.Append(session.Message{Role: "user", Content: "keep"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := saved.Append(session.Message{Role: "assistant", Content: "keep reply"}); err != nil {
		t.Fatal(err)
	}
	if _, err := saved.AppendCompaction("old summary", keptID, 42, nil); err != nil {
		t.Fatal(err)
	}

	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	provider := &contextCaptureProvider{}
	runner := New(queue, provider, nil)
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	link := conversation.Link{Adapter: "cli", AdapterKey: dir, ConversationID: "conv-compact", WorkspaceID: dir}
	if _, err := runner.Submit(link, "new prompt"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runner.RunNext(context.Background(), link.ConversationID); err != nil {
		t.Fatal(err)
	}
	if len(provider.messages) != 4 || provider.messages[0].Content == "old" || provider.messages[1].Content != "keep" || provider.messages[3].Content != "new prompt" {
		t.Fatalf("provider context=%#v", provider.messages)
	}
}

func TestAutoCompactKeepRecentTokensFromEnv(t *testing.T) {
	t.Setenv("THEOSES_AUTO_COMPACT_KEEP_RECENT_TOKENS", "2048")
	if got := AutoCompactKeepRecentTokensFromEnv(); got != 2048 {
		t.Fatalf("tokens=%d", got)
	}
	t.Setenv("THEOSES_AUTO_COMPACT_KEEP_RECENT_TOKENS", "0")
	if got := AutoCompactKeepRecentTokensFromEnv(); got != 0 {
		t.Fatalf("disabled tokens=%d", got)
	}
}

func TestAutoCompactContextSettingsFromEnv(t *testing.T) {
	t.Setenv("THEOSES_AUTO_COMPACT_CONTEXT_WINDOW", "8192")
	if got := AutoCompactContextWindowFromEnv(); got != 8192 {
		t.Fatalf("context window=%d", got)
	}
	t.Setenv("THEOSES_AUTO_COMPACT_RESERVE_TOKENS", "4096")
	if got := AutoCompactReserveTokensFromEnv(); got != 4096 {
		t.Fatalf("reserve tokens=%d", got)
	}
	t.Setenv("THEOSES_AUTO_COMPACT_CONTEXT_WINDOW", "0")
	if got := AutoCompactContextWindowFromEnv(); got != 0 {
		t.Fatalf("disabled context window=%d", got)
	}
	t.Setenv("THEOSES_AUTO_COMPACT_RESERVE_TOKENS", "0")
	if got := AutoCompactReserveTokensFromEnv(); got != 16384 {
		t.Fatalf("default reserve tokens=%d", got)
	}
}

func TestAutoCompactMaxHistoryTurnsFromEnv(t *testing.T) {
	t.Setenv("THEOSES_AUTO_COMPACT_MAX_HISTORY_TURNS", "3")
	if got := AutoCompactMaxHistoryTurnsFromEnv(); got != 3 {
		t.Fatalf("max history turns=%d", got)
	}
	t.Setenv("THEOSES_AUTO_COMPACT_MAX_HISTORY_TURNS", "0")
	if got := AutoCompactMaxHistoryTurnsFromEnv(); got != 0 {
		t.Fatalf("disabled max history turns=%d", got)
	}
}

func TestAutoCompactDisabledFromEnv(t *testing.T) {
	t.Setenv("THEOSES_AUTO_COMPACT_ENABLED", "false")
	if !AutoCompactDisabledFromEnv() {
		t.Fatal("expected automatic compaction to be disabled")
	}
	t.Setenv("THEOSES_AUTO_COMPACT_ENABLED", "true")
	if AutoCompactDisabledFromEnv() {
		t.Fatal("expected automatic compaction to be enabled")
	}
}

func TestRunnerCompactsSessionWithProviderSummary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conv-compact.jsonl")
	saved := session.New(path, session.Header{ID: "conv-compact", ConversationID: "conv-compact", CWD: dir, Channel: "cli", ChannelSessionID: dir})
	for _, content := range []string{"one", "one reply", "two", "two reply", "three", "three reply"} {
		role := "user"
		if strings.HasSuffix(content, "reply") {
			role = "assistant"
		}
		if _, err := saved.Append(session.Message{Role: role, Content: content}); err != nil {
			t.Fatal(err)
		}
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	provider := &summaryProvider{response: agent.Response{Text: "structured summary", StopReason: "stop", Usage: agent.Usage{Input: 12, Output: 4, TotalTokens: 16}}}
	runner := New(queue, provider, nil)
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	if err := runner.Compact(context.Background(), "conv-compact", 2); err != nil {
		t.Fatal(err)
	}
	if len(provider.seen) != 1 || !strings.Contains(provider.seen[0].Content, "one reply") {
		t.Fatalf("summary prompt=%#v", provider.seen)
	}
	reopened, err := session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	contextMessages := reopened.ContextMessages()
	if len(contextMessages) != 5 || !strings.Contains(contextMessages[0].Content.(string), "structured summary") || contextMessages[1].Content != "two" {
		t.Fatalf("context=%#v", contextMessages)
	}
}

func TestRunnerCompactionDoesNotPersistInvalidSummary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conv-invalid.jsonl")
	saved := session.New(path, session.Header{ID: "conv-invalid", ConversationID: "conv-invalid", CWD: dir, Channel: "cli"})
	for _, content := range []string{"one", "one reply", "two", "two reply"} {
		role := "user"
		if strings.HasSuffix(content, "reply") {
			role = "assistant"
		}
		if _, err := saved.Append(session.Message{Role: role, Content: content}); err != nil {
			t.Fatal(err)
		}
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runner := New(queue, &summaryProvider{response: agent.Response{ToolCalls: []agent.ToolCall{{ID: "bad", Name: "read"}}, StopReason: "toolUse"}}, nil)
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	if err := runner.Compact(context.Background(), "conv-invalid", 1); err == nil {
		t.Fatal("expected invalid summary error")
	}
	reopened, err := session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.ContextMessages()) != 4 {
		t.Fatalf("context changed after failed compaction: %#v", reopened.ContextMessages())
	}
}

func TestRunnerAutoCompactsBeforePrompt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conv-auto.jsonl")
	saved := session.New(path, session.Header{ID: "conv-auto", ConversationID: "conv-auto", CWD: dir, Channel: "cli"})
	for _, content := range []string{"one", "one reply", "two", "two reply", "three", "three reply"} {
		role := "user"
		if strings.HasSuffix(content, "reply") {
			role = "assistant"
		}
		if _, err := saved.Append(session.Message{Role: role, Content: content}); err != nil {
			t.Fatal(err)
		}
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	provider := &autoCompactionProvider{}
	runner := New(queue, provider, nil)
	runner.AutoCompactMaxHistoryTurns = 2
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	link := conversation.Link{Adapter: "cli", AdapterKey: dir, ConversationID: "conv-auto", WorkspaceID: dir}
	if _, err := runner.Submit(link, "four"); err != nil {
		t.Fatal(err)
	}
	if _, result, err := runner.RunNext(context.Background(), link.ConversationID); err != nil || result.FinalText != "continued" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if provider.calls != 2 || len(provider.seen[1]) == 0 || provider.seen[1][0].Content == "one" {
		t.Fatalf("provider calls=%d messages=%#v", provider.calls, provider.seen)
	}
	reopened, err := session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reopened.ContextMessages()[0].Content.(string), "automatic summary") {
		t.Fatalf("context=%#v", reopened.ContextMessages())
	}
}

func TestRunnerAutoCompactsBeforePromptAtContextThreshold(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conv-auto-context.jsonl")
	saved := session.New(path, session.Header{ID: "conv-auto-context", ConversationID: "conv-auto-context", CWD: dir, Channel: "cli"})
	for _, content := range []string{"one", "one reply", "two", "two reply", "three", "three reply"} {
		role := "user"
		if strings.HasSuffix(content, "reply") {
			role = "assistant"
		}
		if _, err := saved.Append(session.Message{Role: role, Content: content}); err != nil {
			t.Fatal(err)
		}
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	provider := &autoCompactionProvider{}
	runner := New(queue, provider, nil)
	runner.AutoCompactContextWindow = 20
	runner.AutoCompactReserveTokens = 10
	runner.AutoCompactKeepRecentTokens = 4
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	link := conversation.Link{Adapter: "cli", AdapterKey: dir, ConversationID: "conv-auto-context", WorkspaceID: dir}
	if _, err := runner.Submit(link, "four"); err != nil {
		t.Fatal(err)
	}
	if _, result, err := runner.RunNext(context.Background(), link.ConversationID); err != nil || result.FinalText != "continued" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if provider.calls != 2 || len(provider.seen[1]) == 0 || provider.seen[1][0].Content == "one" {
		t.Fatalf("provider calls=%d messages=%#v", provider.calls, provider.seen)
	}
}

func TestRunnerCanDisableAutomaticCompaction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conv-no-auto.jsonl")
	saved := session.New(path, session.Header{ID: "conv-no-auto", ConversationID: "conv-no-auto", CWD: dir, Channel: "cli"})
	for _, content := range []string{"one", "one reply", "two", "two reply", "three", "three reply"} {
		role := "user"
		if strings.HasSuffix(content, "reply") {
			role = "assistant"
		}
		if _, err := saved.Append(session.Message{Role: role, Content: content}); err != nil {
			t.Fatal(err)
		}
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	provider := &contextCaptureProvider{}
	runner := New(queue, provider, nil)
	runner.AutoCompactDisabled = true
	runner.AutoCompactMaxHistoryTurns = 2
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	link := conversation.Link{Adapter: "cli", AdapterKey: dir, ConversationID: "conv-no-auto", WorkspaceID: dir}
	if _, err := runner.Submit(link, "four"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runner.RunNext(context.Background(), link.ConversationID); err != nil {
		t.Fatal(err)
	}
	if len(provider.messages) == 0 || provider.messages[0].Content != "one" {
		t.Fatalf("automatic compaction was not disabled: %#v", provider.messages)
	}
}

func TestRunnerRetriesOnceAfterOptInContextOverflow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conv-overflow.jsonl")
	saved := session.New(path, session.Header{ID: "conv-overflow", ConversationID: "conv-overflow", CWD: dir, Channel: "cli"})
	for _, content := range []string{"one", "one reply", "two", "two reply", "three", "three reply"} {
		role := "user"
		if strings.HasSuffix(content, "reply") {
			role = "assistant"
		}
		if _, err := saved.Append(session.Message{Role: role, Content: content}); err != nil {
			t.Fatal(err)
		}
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	provider := &overflowRecoveryProvider{}
	runner := New(queue, provider, nil)
	runner.AutoCompactOnOverflow = true
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	link := conversation.Link{Adapter: "cli", AdapterKey: dir, ConversationID: "conv-overflow", WorkspaceID: dir}
	if _, err := runner.Submit(link, "recover"); err != nil {
		t.Fatal(err)
	}
	if _, result, err := runner.RunNext(context.Background(), link.ConversationID); err != nil || result.FinalText != "recovered" {
		t.Fatalf("result=%#v err=%v calls=%d", result, err, provider.calls)
	}
	if provider.calls != 3 {
		t.Fatalf("provider calls=%d, want overflow, summary, retry", provider.calls)
	}
	reopened, err := session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reopened.ContextMessages()[0].Content.(string), "overflow summary") {
		t.Fatalf("context=%#v", reopened.ContextMessages())
	}
}

func TestRunnerCompactsAndRetriesRecoverableLengthStop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conv-length.jsonl")
	saved := session.New(path, session.Header{ID: "conv-length", ConversationID: "conv-length", CWD: dir, Channel: "cli"})
	for _, content := range []string{"one", "one reply", "two", "two reply", "three", "three reply"} {
		role := "user"
		if strings.HasSuffix(content, "reply") {
			role = "assistant"
		}
		if _, err := saved.Append(session.Message{Role: role, Content: content}); err != nil {
			t.Fatal(err)
		}
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	provider := &lengthRecoveryProvider{}
	runner := New(queue, provider, nil)
	runner.AutoCompactOnOverflow = true
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	link := conversation.Link{Adapter: "cli", AdapterKey: dir, ConversationID: "conv-length", WorkspaceID: dir}
	if _, err := runner.Submit(link, "continue"); err != nil {
		t.Fatal(err)
	}
	if _, result, err := runner.RunNext(context.Background(), link.ConversationID); err != nil || result.FinalText != "recovered" {
		t.Fatalf("result=%#v err=%v calls=%d", result, err, provider.calls)
	}
	if provider.calls != 3 {
		t.Fatalf("provider calls=%d, want compaction and one retry", provider.calls)
	}
	reopened, err := session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprint(reopened.Messages()), "truncated") || !strings.Contains(fmt.Sprint(reopened.Messages()), "recovered") {
		t.Fatalf("persisted messages=%#v", reopened.Messages())
	}
}

func TestRunnerExposesScopedMemoryTools(t *testing.T) {
	dir := t.TempDir()
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := memory.OpenEngine(filepath.Join(dir, "memory"))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	provider := &memoryToolProvider{}
	runner := New(queue, provider, nil)
	runner.Memory = engine
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	link := conversation.Link{Adapter: "cli", AdapterKey: "cwd", ConversationID: "conv-memory-tool", WorkspaceID: "work-1"}
	_, err = runner.Submit(link, "save my preference")
	if err != nil {
		t.Fatal(err)
	}
	_, result, err := runner.RunNext(context.Background(), link.ConversationID)
	if err != nil || result.FinalText != "memory saved" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	hits, err := engine.Remember("concise replies", memory.Context{WorkspaceID: "work-1", ConversationID: link.ConversationID})
	if err != nil || len(hits) != 1 || hits[0].ConversationID != link.ConversationID {
		t.Fatalf("memory hits=%#v err=%v", hits, err)
	}
}

func TestRecallTurnsExcludesToolOutputAndBoundsResults(t *testing.T) {
	tool := recallTurnsTool{history: []agent.Message{
		{Role: "user", Content: "The project uses a local queue."},
		{Role: "tool", Content: "The project uses a remote queue."},
		{Role: "assistant", Content: "I will keep the local queue durable."},
	}}
	result, err := tool.Execute(context.Background(), map[string]any{"query": "what queue is local"})
	if err != nil || !strings.Contains(result, "user: The project uses a local queue.") || !strings.Contains(result, "assistant: I will keep the local queue durable.") || strings.Contains(result, "remote queue") {
		t.Fatalf("result=%q err=%v", result, err)
	}
}

func TestRunnerWaitsForQueuedTurnInsteadOfRejectingIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "queue.jsonl")
	queueOne, err := conversation.OpenQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	queueTwo, err := conversation.OpenQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	provider := &slowProvider{started: make(chan struct{}), release: make(chan struct{}), first: true}
	first := New(queueOne, provider, nil)
	second := New(queueTwo, provider, nil)
	first.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	second.SessionPath = first.SessionPath
	link := conversation.Link{Adapter: "cli", AdapterKey: "cwd", ConversationID: "conv-queued", WorkspaceID: dir}
	turnOne, err := first.Submit(link, "one")
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan error, 1)
	go func() { _, _, runErr := first.RunSubmitted(context.Background(), turnOne); firstDone <- runErr }()
	<-provider.started
	turnTwo, err := second.Submit(link, "two")
	if err != nil {
		t.Fatal(err)
	}
	secondDone := make(chan error, 1)
	go func() { _, _, runErr := second.RunSubmitted(context.Background(), turnTwo); secondDone <- runErr }()
	close(provider.release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.seen) != 2 || provider.seen[0] != "one" || provider.seen[1] != "two" {
		t.Fatalf("provider order=%#v", provider.seen)
	}
}

func TestRunnerRemoteCancelStopsActiveTurn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "queue.jsonl")
	queueOne, err := conversation.OpenQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	queueTwo, err := conversation.OpenQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	provider := cancelableProvider{started: make(chan struct{})}
	first := New(queueOne, provider, nil)
	second := New(queueTwo, provider, nil)
	first.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	link := conversation.Link{Adapter: "dashboard", AdapterKey: "tab-1", ConversationID: "conv-cancel", WorkspaceID: dir}
	turn, err := first.Submit(link, "stop me")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, _, runErr := first.RunSubmitted(context.Background(), turn); done <- runErr }()
	<-provider.started
	if err := second.Cancel(turn.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("run error=%v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("remote cancellation did not stop active turn")
	}
}

func TestRunnerSteersActiveTurnThroughAgentQueue(t *testing.T) {
	dir := t.TempDir()
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	provider := &steeringProvider{started: make(chan struct{}), release: make(chan struct{})}
	runner := New(queue, provider, nil)
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	link := conversation.Link{Adapter: "dashboard", AdapterKey: "tab-steer", ConversationID: "conv-steer", WorkspaceID: dir}
	turn, err := runner.Submit(link, "start")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, result, runErr := runner.RunSubmitted(context.Background(), turn)
		if runErr == nil && result.FinalText != "steered" {
			done <- errors.New("final response did not include steering")
		} else {
			done <- runErr
		}
	}()
	<-provider.started
	if err := runner.Steer(turn.ID, "change direction"); err != nil {
		t.Fatal(err)
	}
	close(provider.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.seen) != 2 || len(provider.seen[1]) != 3 || provider.seen[1][2].Content != "change direction" {
		t.Fatalf("provider messages=%#v", provider.seen)
	}
}

func TestToConsolidationTurnsCondensesToolPayloads(t *testing.T) {
	toolResult := strings.Repeat("r", consolidationToolResultChars+10)
	turns := toConsolidationTurns([]session.TimedMessage{
		{Message: session.Message{Role: "assistant", Content: []session.ContentPart{{Type: "toolCall", Name: "read", Arguments: map[string]any{"path": "README.md"}}}}},
		{Message: session.Message{Role: "toolResult", Content: []session.ContentPart{{Type: "text", Text: toolResult}}}},
	})
	if turns[0].Content != `called read({"path":"README.md"})` {
		t.Fatalf("tool call transcript=%q", turns[0].Content)
	}
	if !strings.HasPrefix(turns[1].Content, "OK — "+strings.Repeat("r", consolidationToolResultChars)) || !strings.HasSuffix(turns[1].Content, "…") {
		t.Fatalf("tool result transcript was not bounded: len=%d", len([]rune(turns[1].Content)))
	}
}

func TestToConsolidationTurnsFormatsSpecialEntries(t *testing.T) {
	exitCode := 2
	turns := toConsolidationTurns([]session.TimedMessage{
		{Message: session.Message{Role: "bashExecution", Command: "go test ./...", Output: "failed", ExitCode: &exitCode}},
		{Message: session.Message{Role: "bashExecution", Command: "sleep 10", Cancelled: true}},
		{Message: session.Message{Role: "branchSummary", Summary: "The branch chose the safer path."}},
		{Message: session.Message{Role: "compactionSummary", Summary: "Earlier context was compacted."}},
		{Message: session.Message{Role: "toolResult", Content: []session.ContentPart{{Type: "text", Text: "Tool error: missing file"}}}},
	})
	want := []string{
		"ran `go test ./...` — exit 2: failed",
		"ran `sleep 10` — cancelled",
		"The branch chose the safer path.",
		"Earlier context was compacted.",
		"FAILED — Tool error: missing file",
	}
	for i := range want {
		if turns[i].Content != want[i] {
			t.Fatalf("turn %d = %q, want %q", i, turns[i].Content, want[i])
		}
	}
}

func TestToAgentMessagesConvertsPersistedSpecialMessages(t *testing.T) {
	exitCode := 1
	messages := toAgentMessages([]session.Message{
		{Role: "bashExecution", Command: "go test", Output: "failed", ExitCode: &exitCode},
		{Role: "branchSummary", Summary: "safer branch"},
		{Role: "compactionSummary", Summary: "old context"},
		{Role: "custom", Content: "extension note"},
		{Role: "bashExecution", Command: "secret", ExcludeFromContext: true},
	})
	if len(messages) != 4 {
		t.Fatalf("converted messages=%#v", messages)
	}
	want := []string{
		"Ran `go test`\n```\nfailed\n```\n\nCommand exited with code 1",
		"The following is a summary of a branch that this conversation came back from:\n\n<summary>\nsafer branch\n</summary>",
		"The conversation history before this point was compacted into the following summary:\n\n<summary>\nold context\n</summary>",
		"extension note",
	}
	for i := range want {
		if messages[i].Role != "user" || messages[i].Content != want[i] {
			t.Fatalf("message %d=%#v want role user content %q", i, messages[i], want[i])
		}
	}
}

func TestThinkingRoundTripsThroughSessionContext(t *testing.T) {
	stored := toSessionMessage(agent.Message{Role: "assistant", Thinking: "plan first", ThinkingSignature: "reasoning_content", Content: "answer"})
	parts, ok := stored.Content.([]session.ContentPart)
	if !ok || len(parts) != 2 || parts[0].Type != "thinking" || parts[0].Text != "plan first" || parts[0].ThinkingSignature != "reasoning_content" || parts[1].Type != "text" || parts[1].Text != "answer" {
		t.Fatalf("stored content=%#v", stored.Content)
	}
	converted := toAgentMessages([]session.Message{stored})
	if len(converted) != 1 || converted[0].Thinking != "plan first" || converted[0].ThinkingSignature != "reasoning_content" || converted[0].Content != "answer" {
		t.Fatalf("converted=%#v", converted)
	}
}

func TestAssistantErrorMessageRoundTripsThroughSession(t *testing.T) {
	stored := toSessionMessage(agent.Message{Role: "assistant", Content: "blocked", StopReason: "error", ErrorMessage: "Provider finish_reason: content_filter", ResponseID: "resp-1", ResponseModel: "served-model", RawStopReason: "content_filter"})
	if stored.ErrorMessage != "Provider finish_reason: content_filter" {
		t.Fatalf("stored=%#v", stored)
	}
	converted := toAgentMessages([]session.Message{stored})
	if len(converted) != 1 || converted[0].ErrorMessage != stored.ErrorMessage || converted[0].ResponseID != "resp-1" || converted[0].ResponseModel != "served-model" || converted[0].RawStopReason != "content_filter" {
		t.Fatalf("converted=%#v", converted)
	}
}
