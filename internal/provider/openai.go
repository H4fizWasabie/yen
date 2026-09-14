package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/H4fizWasabie/theoses2-go/internal/agent"
)

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type OpenAICompletions struct {
	BaseURL string
	APIKey  string
	Model   string
	Client  *http.Client
}

func NewOpenAICompletions(baseURL, apiKey, model string) OpenAICompletions {
	return OpenAICompletions{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, Model: model}
}

func (p OpenAICompletions) Next(ctx context.Context, messages []agent.Message, toolNames []string) (agent.Response, error) {
	payload := struct {
		Model    string           `json:"model"`
		Messages []openAIMessage  `json:"messages"`
		Tools    []map[string]any `json:"tools,omitempty"`
		Stream   bool             `json:"stream"`
	}{Model: p.Model, Messages: convertMessages(messages), Stream: true}
	for _, name := range toolNames {
		payload.Tools = append(payload.Tools, map[string]any{
			"type":     "function",
			"function": map[string]any{"name": name, "parameters": map[string]any{"type": "object"}},
		})
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return agent.Response{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		return agent.Response{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+p.APIKey)
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
		return agent.Response{}, fmt.Errorf("openai completions returned %s", response.Status)
	}

	var result agent.Response
	var toolCalls []agent.ToolCall
	arguments := map[string]string{}
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var event struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return agent.Response{}, err
		}
		for _, choice := range event.Choices {
			result.Text += choice.Delta.Content
			if choice.FinishReason != nil {
				result.StopReason = *choice.FinishReason
			}
			for _, delta := range choice.Delta.ToolCalls {
				for len(toolCalls) <= delta.Index {
					toolCalls = append(toolCalls, agent.ToolCall{})
				}
				if delta.ID != "" {
					toolCalls[delta.Index].ID = delta.ID
				}
				if delta.Function.Name != "" {
					toolCalls[delta.Index].Name = delta.Function.Name
				}
				arguments[fmt.Sprint(delta.Index)] += delta.Function.Arguments
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return agent.Response{}, err
	}
	for index := range toolCalls {
		var args map[string]any
		if raw := arguments[fmt.Sprint(index)]; raw != "" {
			if err := json.Unmarshal([]byte(raw), &args); err != nil {
				return agent.Response{}, fmt.Errorf("tool arguments: %w", err)
			}
		}
		toolCalls[index].Args = args
	}
	result.ToolCalls = toolCalls
	if len(toolCalls) > 0 && result.StopReason == "" {
		result.StopReason = "toolUse"
	}
	return result, nil
}

func convertMessages(messages []agent.Message) []openAIMessage {
	converted := make([]openAIMessage, 0, len(messages))
	for _, message := range messages {
		convertedMessage := openAIMessage{Role: message.Role, Content: message.Content, ToolCallID: message.ToolCallID}
		for _, call := range message.ToolCalls {
			arguments, _ := json.Marshal(call.Args)
			toolCall := openAIToolCall{ID: call.ID, Type: "function"}
			toolCall.Function.Name = call.Name
			toolCall.Function.Arguments = string(arguments)
			convertedMessage.ToolCalls = append(convertedMessage.ToolCalls, toolCall)
		}
		converted = append(converted, convertedMessage)
	}
	return converted
}
