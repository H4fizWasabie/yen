package runtime

import (
	"context"
	"errors"
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
	return agent.Response{Text: "done", StopReason: "stop"}, nil
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
