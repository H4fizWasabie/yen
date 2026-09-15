package agent

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
)

type scriptedProvider struct {
	responses []Response
}

type failingProvider struct{ err error }

func (p failingProvider) Next(context.Context, []Message, []string) (Response, error) {
	return Response{}, p.err
}

type updatingProvider struct{}

func (updatingProvider) Next(context.Context, []Message, []string) (Response, error) {
	return Response{Text: "done", StopReason: "stop"}, nil
}

func (updatingProvider) NextWithUpdates(_ context.Context, _ []Message, _ []string, update func(string)) (Response, error) {
	update("do")
	update("ne")
	return Response{Text: "done", StopReason: "stop"}, nil
}

func TestRunSupportsIndependentConversationsConcurrently(t *testing.T) {
	const count = 32
	var wait sync.WaitGroup
	results := make(chan Result, count)
	errors := make(chan error, count)
	for i := 0; i < count; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := Run(context.Background(), &scriptedProvider{responses: []Response{{Text: "ok", StopReason: "stop"}}}, nil, "hello")
			if err != nil {
				errors <- err
				return
			}
			results <- result
		}()
	}
	wait.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	if len(results) != count {
		t.Fatalf("completed conversations = %d, want %d", len(results), count)
	}
}

func (p *scriptedProvider) Next(context.Context, []Message, []string) (Response, error) {
	response := p.responses[0]
	p.responses = p.responses[1:]
	return response, nil
}

type readTool struct{}

func (readTool) Name() string { return "read" }

func (readTool) Execute(context.Context, map[string]any) (string, error) {
	return "README contents", nil
}

type failingTool struct{}

func (failingTool) Name() string { return "read" }

func (failingTool) Execute(context.Context, map[string]any) (string, error) {
	return "", errors.New("missing file")
}

type abortingTool struct{ cancel context.CancelFunc }

func (t abortingTool) Name() string { return "read" }

func (t abortingTool) Execute(context.Context, map[string]any) (string, error) {
	t.cancel()
	return "", context.Canceled
}

func TestRunExecutesToolThenContinues(t *testing.T) {
	result, err := Run(context.Background(), &scriptedProvider{responses: []Response{
		{Text: "I will read it.", ToolCalls: []ToolCall{{ID: "read-1", Name: "read"}}, StopReason: "toolUse"},
		{Text: "The README says hello.", StopReason: "stop"},
	}}, []Tool{readTool{}}, "summarize README")
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != "The README says hello." {
		t.Fatalf("final text = %q", result.FinalText)
	}
	wantEvents := []string{
		"agent_start", "turn_start", "message_start:user", "message_end:user",
		"message_start:assistant", "message_end:assistant:toolUse", "tool_execution_start:read-1",
		"tool_execution_end:read-1", "message_start:toolResult", "message_end:toolResult",
		"turn_end", "turn_start", "message_start:assistant",
		"message_end:assistant", "turn_end", "agent_end", "agent_settled",
	}
	if !reflect.DeepEqual(result.Events, wantEvents) {
		t.Fatalf("events = %#v, want %#v", result.Events, wantEvents)
	}
	if len(result.Messages) != 4 {
		t.Fatalf("messages = %d, want user, assistant, tool, assistant", len(result.Messages))
	}
}

func TestRunWithEventsReportsDashboardToolAndUsageEvents(t *testing.T) {
	var events []Event
	_, err := RunFromWithQueuesAndEvents(context.Background(), &scriptedProvider{responses: []Response{
		{ToolCalls: []ToolCall{{ID: "read-1", Name: "read"}}, StopReason: "toolUse", Usage: Usage{Input: 2, Output: 3, TotalTokens: 5}},
		{Text: "done", StopReason: "stop", Usage: Usage{Input: 4, Output: 1, TotalTokens: 5}},
	}}, []Tool{readTool{}}, nil, "hello", nil, nil, func(event Event) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 || events[0].Type != "usage" || events[1].Type != "tool_call" || events[2].Type != "tool_result" || events[3].Type != "usage" {
		t.Fatalf("events = %#v", events)
	}
	if events[2].Result != "README contents" || events[2].IsError {
		t.Fatalf("tool result = %#v", events[2])
	}
}

func TestRunNormalizedTracesMatchGoldenOutcomes(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		want     []string
	}{
		{
			name:     "success",
			provider: updatingProvider{},
			want:     []string{"agent_start", "turn_start", "message_start:user", "message_end:user", "message_start:assistant", "message_update", "message_update", "message_end:assistant", "turn_end", "agent_end", "agent_settled"},
		},
		{
			name:     "tool",
			provider: &scriptedProvider{responses: []Response{{ToolCalls: []ToolCall{{ID: "calc-1", Name: "read"}}, StopReason: "toolUse"}, {Text: "done", StopReason: "stop"}}},
			want:     []string{"agent_start", "turn_start", "message_start:user", "message_end:user", "message_start:assistant", "message_end:assistant:toolUse", "tool_execution_start:calc-1", "tool_execution_end:calc-1", "message_start:toolResult", "message_end:toolResult", "turn_end", "turn_start", "message_start:assistant", "message_end:assistant", "turn_end", "agent_end", "agent_settled"},
		},
		{
			name:     "error",
			provider: failingProvider{err: errors.New("provider down")},
			want:     []string{"agent_start", "turn_start", "message_start:user", "message_end:user", "message_start:assistant", "message_end:assistant:error", "turn_end", "agent_end", "agent_settled"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := Run(context.Background(), tt.provider, []Tool{readTool{}}, "hello")
			if !reflect.DeepEqual(got.Events, tt.want) {
				t.Fatalf("events = %#v, want %#v", got.Events, tt.want)
			}
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, _ := Run(ctx, failingProvider{err: context.Canceled}, nil, "hello")
	want := []string{"agent_start", "turn_start", "message_start:user", "message_end:user", "message_start:assistant", "message_end:assistant:aborted", "turn_end", "agent_end", "agent_settled"}
	if !reflect.DeepEqual(got.Events, want) {
		t.Fatalf("abort events = %#v, want %#v", got.Events, want)
	}
}

func TestRunPersistsAssistantErrorBoundary(t *testing.T) {
	result, err := Run(context.Background(), failingProvider{err: errors.New("provider down")}, nil, "hello")
	if err == nil {
		t.Fatal("expected provider error")
	}
	if len(result.Messages) != 2 || result.Messages[1].Role != "assistant" || result.Messages[1].StopReason != "error" {
		t.Fatalf("messages = %#v", result.Messages)
	}
}

func TestRunRecordsAbortedAssistantBoundary(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := Run(ctx, failingProvider{err: context.Canceled}, nil, "hello")
	if !errors.Is(err, context.Canceled) || len(result.Messages) != 2 || result.Messages[1].StopReason != "aborted" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !reflect.DeepEqual(result.Events[4:], []string{"message_start:assistant", "message_end:assistant:aborted", "turn_end", "agent_end", "agent_settled"}) {
		t.Fatalf("events=%#v", result.Events)
	}
}

func TestRunRecordsAbortedToolResultBoundary(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	result, err := Run(ctx, &scriptedProvider{responses: []Response{{ToolCalls: []ToolCall{{ID: "read-1", Name: "read"}}, StopReason: "toolUse"}}}, []Tool{abortingTool{cancel: cancel}}, "read it")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if len(result.Messages) != 3 || result.Messages[2].Content != "Operation aborted" || result.Messages[2].Role != "tool" {
		t.Fatalf("messages=%#v", result.Messages)
	}
	want := []string{"tool_execution_start:read-1", "tool_execution_end:read-1", "message_start:toolResult", "message_end:toolResult", "turn_end", "agent_end", "agent_settled"}
	if !reflect.DeepEqual(result.Events[len(result.Events)-len(want):], want) {
		t.Fatalf("events=%#v", result.Events)
	}
}

func TestRunContinuesAfterToolError(t *testing.T) {
	result, err := Run(context.Background(), &scriptedProvider{responses: []Response{
		{ToolCalls: []ToolCall{{ID: "read-1", Name: "read"}}, StopReason: "toolUse"},
		{Text: "recovered", StopReason: "stop"},
	}}, []Tool{failingTool{}}, "read it")
	if err != nil || result.FinalText != "recovered" || len(result.Messages) != 4 || result.Messages[2].Content != "Tool error: missing file" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if count := len(result.Events); count != 17 {
		t.Fatalf("events=%#v (count=%d)", result.Events, count)
	}
}

func TestRunIncludesProviderUpdates(t *testing.T) {
	result, err := Run(context.Background(), updatingProvider{}, nil, "hello")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"message_start:assistant", "message_update", "message_update", "message_end:assistant"}
	for i, event := range want {
		if result.Events[4+i] != event {
			t.Fatalf("events = %#v", result.Events)
		}
	}
}

func TestRunCarriesProviderUsageOnAssistantMessage(t *testing.T) {
	result, err := Run(context.Background(), &scriptedProvider{responses: []Response{{
		Text:       "done",
		StopReason: "stop",
		Provider:   "openrouter",
		Model:      "z-ai/glm-5.3-flash",
		Usage:      Usage{Input: 4, Output: 2, TotalTokens: 6},
	}}}, nil, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if result.Messages[1].Usage == nil || *result.Messages[1].Usage != (Usage{Input: 4, Output: 2, TotalTokens: 6}) {
		t.Fatalf("assistant usage=%#v", result.Messages[1].Usage)
	}
	if result.Messages[1].Provider != "openrouter" || result.Messages[1].Model != "z-ai/glm-5.3-flash" {
		t.Fatalf("assistant model=%#v", result.Messages[1])
	}
}

type queuedMessagesProvider struct {
	queues *MessageQueues
	calls  int
	seen   [][]Message
}

func (p *queuedMessagesProvider) Next(_ context.Context, messages []Message, _ []string) (Response, error) {
	p.seen = append(p.seen, append([]Message(nil), messages...))
	p.calls++
	if p.calls == 1 {
		p.queues.FollowUp(Message{Role: "user", Content: "follow up"})
		return Response{Text: "first", StopReason: "stop"}, nil
	}
	return Response{Text: "second", StopReason: "stop"}, nil
}

func TestRunWithQueuesProcessesSteeringAndFollowUp(t *testing.T) {
	queues := &MessageQueues{}
	queues.Steer(Message{Role: "user", Content: "steer now"})
	provider := &queuedMessagesProvider{queues: queues}
	result, err := RunFromWithQueues(context.Background(), provider, nil, nil, "start", queues, nil)
	if err != nil || result.FinalText != "second" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if len(provider.seen) != 2 || len(provider.seen[0]) != 2 || provider.seen[0][1].Content != "steer now" || len(provider.seen[1]) != 4 || provider.seen[1][3].Content != "follow up" {
		t.Fatalf("provider messages=%#v", provider.seen)
	}
}

type prioritizedQueuesProvider struct {
	queues *MessageQueues
	calls  int
	seen   [][]Message
}

func (p *prioritizedQueuesProvider) Next(_ context.Context, messages []Message, _ []string) (Response, error) {
	p.seen = append(p.seen, append([]Message(nil), messages...))
	p.calls++
	if p.calls == 1 {
		p.queues.Steer(Message{Role: "user", Content: "steer first"})
		p.queues.FollowUp(Message{Role: "user", Content: "follow second"})
	}
	return Response{Text: "reply", StopReason: "stop"}, nil
}

func TestRunWithQueuesPrioritizesSteeringOverFollowUp(t *testing.T) {
	queues := &MessageQueues{}
	provider := &prioritizedQueuesProvider{queues: queues}
	result, err := RunFromWithQueues(context.Background(), provider, nil, nil, "start", queues, nil)
	if err != nil || result.FinalText != "reply" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if len(provider.seen) != 3 || provider.seen[1][2].Content != "steer first" || provider.seen[2][4].Content != "follow second" {
		t.Fatalf("provider messages=%#v", provider.seen)
	}
}
