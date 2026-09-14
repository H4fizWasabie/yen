package agent

import (
	"context"
	"reflect"
	"sync"
	"testing"
)

type scriptedProvider struct {
	responses []Response
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
		"message_start:assistant", "message_end:assistant", "tool_execution_start:read-1",
		"tool_execution_end:read-1", "message_start:toolResult", "message_end:toolResult",
		"turn_end", "turn_start", "message_start:assistant",
		"message_end:assistant", "turn_end", "agent_end",
	}
	if !reflect.DeepEqual(result.Events, wantEvents) {
		t.Fatalf("events = %#v, want %#v", result.Events, wantEvents)
	}
	if len(result.Messages) != 4 {
		t.Fatalf("messages = %d, want user, assistant, tool, assistant", len(result.Messages))
	}
}
