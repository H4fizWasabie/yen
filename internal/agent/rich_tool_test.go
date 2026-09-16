package agent

import (
	"context"
	"testing"
)

type richTool struct{}

func (richTool) Name() string                                            { return "image" }
func (richTool) Execute(context.Context, map[string]any) (string, error) { return "text", nil }
func (richTool) ExecuteRich(context.Context, map[string]any) (ToolResult, error) {
	return ToolResult{Text: "text", Images: []string{"data:image/png;base64,AA=="}, Details: map[string]any{"ok": true}}, nil
}

type richProvider struct{ calls int }

func (p *richProvider) Next(_ context.Context, messages []Message, _ []string) (Response, error) {
	p.calls++
	if p.calls == 1 {
		return Response{ToolCalls: []ToolCall{{ID: "image-1", Name: "image"}}, StopReason: "toolUse"}, nil
	}
	if len(messages) < 3 || len(messages[2].Images) != 1 || messages[2].ToolResultDetails == nil {
		return Response{}, &UnknownToolError{Name: "rich result was not preserved"}
	}
	return Response{Text: "done", StopReason: "stop"}, nil
}

func TestRunPreservesRichToolImages(t *testing.T) {
	result, err := Run(context.Background(), &richProvider{}, []Tool{richTool{}}, "make image")
	if err != nil || result.FinalText != "done" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
