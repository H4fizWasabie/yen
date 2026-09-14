package agent

import "context"

type Message struct {
	Role       string
	Content    string
	ToolCalls  []ToolCall
	ToolCallID string
	StopReason string
}

type ToolCall struct {
	ID   string
	Name string
	Args map[string]any
}

type Response struct {
	Text       string
	ToolCalls  []ToolCall
	StopReason string
	Usage      Usage
}

type Usage struct {
	Input       int
	Output      int
	Reasoning   int
	CacheRead   int
	CacheWrite  int
	TotalTokens int
}

type Provider interface {
	Next(ctx context.Context, messages []Message, tools []string) (Response, error)
}

type StreamingProvider interface {
	NextWithUpdates(ctx context.Context, messages []Message, tools []string, update func(string)) (Response, error)
}

type Tool interface {
	Name() string
	Execute(ctx context.Context, args map[string]any) (string, error)
}

type Result struct {
	Messages  []Message
	Events    []string
	FinalText string
}

func Run(ctx context.Context, provider Provider, tools []Tool, prompt string) (Result, error) {
	return RunFrom(ctx, provider, tools, nil, prompt)
}

func RunFrom(ctx context.Context, provider Provider, tools []Tool, history []Message, prompt string) (Result, error) {
	return RunFromWithUpdates(ctx, provider, tools, history, prompt, nil)
}

func RunFromWithUpdates(ctx context.Context, provider Provider, tools []Tool, history []Message, prompt string, onUpdate func(string)) (Result, error) {
	result := Result{Messages: append([]Message(nil), history...), Events: []string{"agent_start"}}
	result.Messages = append(result.Messages, Message{Role: "user", Content: prompt})
	result.Events = append(result.Events, "turn_start", "message_start:user", "message_end:user")

	toolMap := make(map[string]Tool, len(tools))
	toolNames := make([]string, 0, len(tools))
	for _, tool := range tools {
		toolMap[tool.Name()] = tool
		toolNames = append(toolNames, tool.Name())
	}

	for {
		result.Events = append(result.Events, "message_start:assistant")
		var response Response
		var err error
		if streaming, ok := provider.(StreamingProvider); ok {
			response, err = streaming.NextWithUpdates(ctx, result.Messages, toolNames, func(text string) {
				if text != "" {
					result.Events = append(result.Events, "message_update")
					if onUpdate != nil {
						onUpdate(text)
					}
				}
			})
		} else {
			response, err = provider.Next(ctx, result.Messages, toolNames)
		}
		if err != nil {
			stopReason := "error"
			if ctx.Err() != nil {
				stopReason = "aborted"
			}
			result.Messages = append(result.Messages, Message{Role: "assistant", Content: err.Error(), StopReason: stopReason})
			result.Events = append(result.Events, "message_end:assistant:"+stopReason, "turn_end", "agent_end")
			return result, err
		}
		result.Messages = append(result.Messages, Message{Role: "assistant", Content: response.Text, ToolCalls: response.ToolCalls, StopReason: response.StopReason})
		if len(response.ToolCalls) > 0 {
			result.Events = append(result.Events, "message_end:assistant:toolUse")
		} else {
			result.Events = append(result.Events, "message_end:assistant")
		}

		if len(response.ToolCalls) == 0 {
			result.FinalText = response.Text
			result.Events = append(result.Events, "turn_end", "agent_end")
			return result, nil
		}

		for _, call := range response.ToolCalls {
			tool, ok := toolMap[call.Name]
			if !ok {
				result.Events = append(result.Events, "tool_execution_start:"+call.ID, "tool_execution_end:"+call.ID)
				result.Events = append(result.Events, "message_start:toolResult")
				result.Messages = append(result.Messages, Message{Role: "tool", Content: (&UnknownToolError{Name: call.Name}).Error(), ToolCallID: call.ID})
				result.Events = append(result.Events, "message_end:toolResult")
				continue
			}
			result.Events = append(result.Events, "tool_execution_start:"+call.ID)
			content, err := tool.Execute(ctx, call.Args)
			if err != nil {
				result.Events = append(result.Events, "tool_execution_end:"+call.ID)
				if ctx.Err() != nil {
					return result, ctx.Err()
				}
				content = "Tool error: " + err.Error()
			} else {
				result.Events = append(result.Events, "tool_execution_end:"+call.ID)
			}
			result.Events = append(result.Events, "message_start:toolResult")
			result.Messages = append(result.Messages, Message{Role: "tool", Content: content, ToolCallID: call.ID})
			result.Events = append(result.Events, "message_end:toolResult")
		}
		result.Events = append(result.Events, "turn_end")
		result.Events = append(result.Events, "turn_start")
	}
}

type UnknownToolError struct {
	Name string
}

func (e *UnknownToolError) Error() string { return "unknown tool: " + e.Name }
