package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/H4fizWasabie/yen/internal/agent"
)

type AnthropicMessages struct {
	BaseURL string
	APIKey  string
	Model   string
	Client  *http.Client
}

func NewAnthropicMessages(baseURL, apiKey, model string) AnthropicMessages {
	return AnthropicMessages{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, Model: model}
}

func (p AnthropicMessages) Next(ctx context.Context, messages []agent.Message, toolNames []string) (agent.Response, error) {
	return p.next(ctx, messages, toolNames, nil, nil)
}

func (p AnthropicMessages) NextWithUpdates(ctx context.Context, messages []agent.Message, toolNames []string, update func(string)) (agent.Response, error) {
	return p.next(ctx, messages, toolNames, update, nil)
}

func (p AnthropicMessages) NextWithEvents(ctx context.Context, messages []agent.Message, toolNames []string, emit func(agent.StreamEvent)) (agent.Response, error) {
	return p.next(ctx, messages, toolNames, nil, emit)
}

func (p AnthropicMessages) next(ctx context.Context, messages []agent.Message, toolNames []string, update func(string), emit func(agent.StreamEvent)) (agent.Response, error) {
	system, converted := convertAnthropicMessages(messages)
	payload := map[string]any{"model": p.Model, "max_tokens": 8192, "stream": true, "messages": converted}
	if system != "" {
		payload["system"] = system
	}
	if len(toolNames) > 0 {
		tools := make([]map[string]any, 0, len(toolNames))
		for _, name := range toolNames {
			tools = append(tools, map[string]any{"name": name, "description": name, "input_schema": map[string]any{"type": "object"}})
		}
		payload["tools"] = tools
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return agent.Response{}, err
	}
	endpoint := p.BaseURL + "/messages"
	if strings.HasSuffix(p.BaseURL, "/messages") {
		endpoint = p.BaseURL
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return agent.Response{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("anthropic-version", "2023-06-01")
	if p.APIKey != "" {
		request.Header.Set("x-api-key", p.APIKey)
	}
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return agent.Response{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 16<<10))
		return agent.Response{}, fmt.Errorf("anthropic messages returned %s: %s", response.Status, strings.TrimSpace(string(message)))
	}
	result := agent.Response{Provider: "anthropic", Model: p.Model}
	partial := agent.Message{Role: "assistant", Provider: result.Provider, Model: result.Model}
	toolArgs := map[int]string{}
	toolMeta := map[int]agent.ToolCall{}
	var eventName string
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventName = strings.TrimPrefix(line, "event: ")
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event struct {
			Type    string `json:"type"`
			Message struct {
				ID    string `json:"id"`
				Usage struct {
					InputTokens int `json:"input_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Index        int `json:"index"`
			ContentBlock struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
			Delta struct {
				Type         string `json:"type"`
				Text         string `json:"text"`
				Thinking     string `json:"thinking"`
				PartialJSON  string `json:"partial_json"`
				StopReason   string `json:"stop_reason"`
				OutputTokens int    `json:"output_tokens"`
			} `json:"delta"`
			Usage struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			return agent.Response{}, err
		}
		if eventName == "message_start" || event.Type == "message_start" {
			result.ResponseID = event.Message.ID
			result.Usage.Input = event.Message.Usage.InputTokens
		}
		switch event.Type {
		case "content_block_start":
			if event.ContentBlock.Type == "tool_use" {
				toolMeta[event.Index] = agent.ToolCall{ID: event.ContentBlock.ID, Name: event.ContentBlock.Name}
				toolArgs[event.Index] = ""
			}
		case "content_block_delta":
			switch event.Delta.Type {
			case "text_delta":
				result.Text += event.Delta.Text
				partial.Content += event.Delta.Text
				if update != nil {
					update(event.Delta.Text)
				}
				if emit != nil {
					emit(agent.StreamEvent{Type: "text_delta", Delta: event.Delta.Text, Partial: partial})
				}
			case "thinking_delta":
				result.Thinking += event.Delta.Thinking
				partial.Thinking += event.Delta.Thinking
				if emit != nil {
					emit(agent.StreamEvent{Type: "thinking_delta", Delta: event.Delta.Thinking, Partial: partial})
				}
			case "input_json_delta":
				toolArgs[event.Index] += event.Delta.PartialJSON
			}
		case "message_delta":
			result.StopReason = mapAnthropicStopReason(event.Delta.StopReason)
			result.Usage.Output = event.Delta.OutputTokens
		}
	}
	if err := scanner.Err(); err != nil {
		return agent.Response{}, err
	}
	for index, call := range toolMeta {
		if strings.TrimSpace(toolArgs[index]) == "" {
			call.Args = map[string]any{}
		} else if err := json.Unmarshal([]byte(toolArgs[index]), &call.Args); err != nil {
			return agent.Response{}, err
		}
		result.ToolCalls = append(result.ToolCalls, call)
	}
	if len(result.ToolCalls) > 0 && result.StopReason == "" {
		result.StopReason = "toolUse"
	}
	return result, nil
}

func mapAnthropicStopReason(reason string) string {
	switch reason {
	case "end_turn", "stop_sequence":
		return "stop"
	case "tool_use":
		return "toolUse"
	case "max_tokens":
		return "length"
	case "refusal":
		return "error"
	default:
		return reason
	}
}

func convertAnthropicMessages(messages []agent.Message) (string, []map[string]any) {
	var system []string
	result := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		if message.Role == "system" {
			system = append(system, fmt.Sprint(message.Content))
			continue
		}
		role := message.Role
		if role != "assistant" {
			role = "user"
		}
		content := []any{}
		if message.Content != "" {
			content = append(content, map[string]any{"type": "text", "text": message.Content})
		}
		for _, image := range message.Images {
			if mime, data, ok := parseDataImage(image); ok {
				content = append(content, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": mime, "data": data}})
			}
		}
		if message.ToolCallID != "" {
			content = []any{map[string]any{"type": "tool_result", "tool_use_id": message.ToolCallID, "content": message.Content}}
		}
		result = append(result, map[string]any{"role": role, "content": content})
	}
	return strings.Join(system, "\n\n"), result
}

func parseDataImage(value string) (string, string, bool) {
	if !strings.HasPrefix(value, "data:image/") {
		return "", "", false
	}
	parts := strings.SplitN(value, ",", 2)
	if len(parts) != 2 || !strings.HasSuffix(parts[0], ";base64") {
		return "", "", false
	}
	data, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", false
	}
	mime := strings.TrimPrefix(strings.TrimSuffix(parts[0], ";base64"), "data:")
	return mime, base64.StdEncoding.EncodeToString(data), true
}

var _ agent.Provider = AnthropicMessages{}
