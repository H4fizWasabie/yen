package agent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type scriptedProvider struct {
	responses []Response
}

type failingProvider struct{ err error }

func (p failingProvider) Next(context.Context, []Message, []string) (Response, error) {
	return Response{}, p.err
}

type updatingProvider struct{}

type eventStreamingProvider struct{}

func TestMessageQueueModesDrainAllOrOne(t *testing.T) {
	queue := &MessageQueues{}
	queue.SetModes("one-at-a-time", "one-at-a-time")
	queue.Steer(Message{Role: "user", Content: "a"})
	queue.Steer(Message{Role: "user", Content: "b"})
	if got := queue.drainSteering(); len(got) != 1 || got[0].Content != "a" {
		t.Fatalf("one-at-a-time steering=%#v", got)
	}
	queue.SetModes("all", "all")
	queue.FollowUp(Message{Role: "user", Content: "c"})
	queue.FollowUp(Message{Role: "user", Content: "d"})
	if got := queue.drainFollowUp(); len(got) != 2 {
		t.Fatalf("all follow-up=%#v", got)
	}
}

func (updatingProvider) Next(context.Context, []Message, []string) (Response, error) {
	return Response{Text: "done", StopReason: "stop"}, nil
}

func (updatingProvider) NextWithUpdates(_ context.Context, _ []Message, _ []string, update func(string)) (Response, error) {
	update("do")
	update("ne")
	return Response{Text: "done", StopReason: "stop"}, nil
}

func (eventStreamingProvider) Next(context.Context, []Message, []string) (Response, error) {
	return Response{Text: "done", StopReason: "stop"}, nil
}

func (eventStreamingProvider) NextWithEvents(_ context.Context, _ []Message, _ []string, emit func(StreamEvent)) (Response, error) {
	partial := Message{Role: "assistant", Content: "done"}
	emit(StreamEvent{Type: "text_start", Partial: partial})
	emit(StreamEvent{Type: "text_delta", Delta: "done", Partial: partial})
	emit(StreamEvent{Type: "text_end", Partial: partial})
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

type countingTool struct{ calls *int }

func (t countingTool) Name() string { return "read" }

func (t countingTool) Execute(context.Context, map[string]any) (string, error) {
	*t.calls++
	return "should not run", nil
}

type parallelTool struct {
	started chan<- struct{}
	release <-chan struct{}
}

func (parallelTool) Name() string { return "parallel" }

func (t parallelTool) Execute(ctx context.Context, _ map[string]any) (string, error) {
	t.started <- struct{}{}
	select {
	case <-t.release:
		return "done", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
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

func TestRunToolHooksCanBlockAndRewriteResults(t *testing.T) {
	calls := 0
	tool := countingTool{calls: &calls}
	result, err := RunFromWithQueuesAndEventsAndImagesAndHooks(context.Background(), &scriptedProvider{responses: []Response{
		{ToolCalls: []ToolCall{{ID: "read-1", Name: "read"}}, StopReason: "toolUse"},
		{Text: "done", StopReason: "stop"},
	}}, []Tool{tool}, nil, "read it", nil, nil, nil, nil, &ToolHooks{
		Before: func(_ context.Context, _ Message, call ToolCall) (bool, string, error) {
			if call.Name != "read" {
				t.Fatalf("hook tool=%q", call.Name)
			}
			return false, "", nil
		},
		After: func(_ context.Context, _ Message, _ ToolCall, result ToolResult, _ bool) (ToolResult, bool, error) {
			result.Text = "rewritten"
			return result, false, nil
		},
	})
	if err != nil || result.FinalText != "done" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if calls != 1 || result.Messages[2].Content != "rewritten" {
		t.Fatalf("calls=%d messages=%#v", calls, result.Messages)
	}

	calls = 0
	result, err = RunFromWithQueuesAndEventsAndImagesAndHooks(context.Background(), &scriptedProvider{responses: []Response{
		{ToolCalls: []ToolCall{{ID: "read-2", Name: "read"}}, StopReason: "toolUse"},
		{Text: "blocked", StopReason: "stop"},
	}}, []Tool{tool}, nil, "read it", nil, nil, nil, nil, &ToolHooks{
		Before: func(context.Context, Message, ToolCall) (bool, string, error) {
			return true, "policy denied", nil
		},
	})
	if err != nil || result.FinalText != "blocked" || calls != 0 || result.Messages[2].Content != "policy denied" {
		t.Fatalf("blocked result=%#v err=%v calls=%d", result, err, calls)
	}
}

func TestRunDoesNotExecuteToolCallsFromLengthLimitedResponse(t *testing.T) {
	calls := 0
	var events []Event
	result, err := RunFromWithQueuesAndEvents(context.Background(), &scriptedProvider{responses: []Response{
		{ToolCalls: []ToolCall{{ID: "read-1", Name: "read", Args: map[string]any{"path": "README.md"}}}, StopReason: "length"},
		{Text: "re-issued", StopReason: "stop"},
	}}, []Tool{countingTool{calls: &calls}}, nil, "read it", nil, nil, func(event Event) { events = append(events, event) })
	if err != nil || result.FinalText != "re-issued" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if calls != 0 {
		t.Fatalf("tool calls=%d, want 0", calls)
	}
	if len(result.Messages) != 4 || !strings.Contains(result.Messages[2].Content, "arguments may be truncated") {
		t.Fatalf("messages=%#v", result.Messages)
	}
	var toolEvents []Event
	for _, event := range events {
		if strings.HasPrefix(event.Type, "tool_") || event.Type == "usage" {
			toolEvents = append(toolEvents, event)
		}
	}
	if len(toolEvents) != 6 || toolEvents[3].Type != "tool_execution_end" || !toolEvents[3].IsError || toolEvents[4].Type != "tool_result" || !toolEvents[4].IsError {
		t.Fatalf("events=%#v", events)
	}
}

func TestRunExecutesIndependentToolCallsInParallel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	go func() {
		select {
		case <-started:
			select {
			case <-started:
				close(release)
			case <-ctx.Done():
				close(release)
			}
		case <-ctx.Done():
			close(release)
		}
	}()
	result, err := Run(ctx, &scriptedProvider{responses: []Response{
		{ToolCalls: []ToolCall{{ID: "one", Name: "parallel"}, {ID: "two", Name: "parallel"}}, StopReason: "toolUse"},
		{Text: "complete", StopReason: "stop"},
	}}, []Tool{parallelTool{started: started, release: release}}, "run both")
	if err != nil || result.FinalText != "complete" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if len(result.Messages) != 5 || result.Messages[2].Content != "done" || result.Messages[3].Content != "done" {
		t.Fatalf("messages=%#v", result.Messages)
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
	var dashboardEvents []Event
	for _, event := range events {
		if event.Type == "usage" || strings.HasPrefix(event.Type, "tool_") {
			dashboardEvents = append(dashboardEvents, event)
		}
	}
	if len(dashboardEvents) != 6 || dashboardEvents[0].Type != "usage" || dashboardEvents[1].Type != "tool_call" || dashboardEvents[2].Type != "tool_execution_start" || dashboardEvents[3].Type != "tool_execution_end" || dashboardEvents[4].Type != "tool_result" || dashboardEvents[5].Type != "usage" {
		t.Fatalf("events = %#v", events)
	}
	if dashboardEvents[3].Result != "README contents" || dashboardEvents[3].IsError || dashboardEvents[4].Result != "README contents" || dashboardEvents[4].IsError {
		t.Fatalf("tool result = %#v", dashboardEvents[3:5])
	}
	if dashboardEvents[0].Message == nil || dashboardEvents[0].Message.Role != "assistant" || dashboardEvents[0].StopReason != "toolUse" {
		t.Fatalf("usage payload = %#v", dashboardEvents[0])
	}
	if dashboardEvents[1].Message == nil || len(dashboardEvents[1].Message.ToolCalls) != 1 || dashboardEvents[3].Message == nil || dashboardEvents[3].Message.Role != "tool" || dashboardEvents[3].Message.ToolCallID != "read-1" {
		t.Fatalf("message payloads = %#v %#v", dashboardEvents[1].Message, dashboardEvents[3].Message)
	}
	for _, event := range events {
		if event.Type == "turn_end" && len(event.ToolResults) == 1 {
			if event.ToolResults[0].ToolCallID != "read-1" || event.ToolResults[0].Content != "README contents" {
				t.Fatalf("turn payload=%#v", event)
			}
			return
		}
	}
	t.Fatalf("missing tool turn payload: %#v", events)
}

func TestRunWithEventsIncludesProviderStreamPayload(t *testing.T) {
	var events []Event
	result, err := RunFromWithQueuesAndEvents(context.Background(), eventStreamingProvider{}, nil, nil, "hello", nil, nil, func(event Event) {
		events = append(events, event)
	})
	if err != nil || result.FinalText != "done" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	var delta Event
	for _, event := range events {
		if event.Type == "message_update" && event.AssistantEvent == "text_delta" {
			delta = event
			break
		}
	}
	if delta.Delta != "done" || delta.Message == nil || delta.Message.Content != "done" {
		t.Fatalf("stream payload=%#v", delta)
	}
}

func TestRunWithEventsReportsLifecyclePayloads(t *testing.T) {
	var events []Event
	result, err := RunFromWithQueuesAndEvents(context.Background(), updatingProvider{}, nil, nil, "hello", nil, nil, func(event Event) {
		events = append(events, event)
	})
	if err != nil || result.FinalText != "done" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	var types []string
	for _, event := range events {
		types = append(types, event.Type)
	}
	want := []string{"agent_start", "turn_start", "message_start", "message_end", "message_start", "usage", "message_end", "turn_end", "agent_end", "agent_settled"}
	if !reflect.DeepEqual(types, want) {
		t.Fatalf("event types=%#v want %#v", types, want)
	}
	if events[2].Message == nil || events[2].Message.Role != "user" || events[3].Message == nil || events[3].Message.Content != "hello" {
		t.Fatalf("user lifecycle=%#v %#v", events[2], events[3])
	}
	if events[6].Message == nil || events[6].Message.Content != "done" || len(events[8].Messages) != 2 {
		t.Fatalf("assistant lifecycle=%#v agent end=%#v", events[6], events[8])
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
	if len(result.Messages) != 2 || result.Messages[1].Role != "assistant" || result.Messages[1].StopReason != "error" || result.Messages[1].ErrorMessage != "provider down" {
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
