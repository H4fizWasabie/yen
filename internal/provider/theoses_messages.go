package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/H4fizWasabie/yen/internal/agent"
)

// TheosesMessages implements the gateway protocol used by the oracle's Radius provider.
type TheosesMessages struct {
	BaseURL      string
	GatewayURL   string
	APIKey       string
	Model        string
	ProviderName string
	Client       *http.Client
}

func NewTheosesMessages(baseURL, apiKey, model string) TheosesMessages {
	return TheosesMessages{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, Model: model, ProviderName: "radius"}
}

func (p TheosesMessages) Next(ctx context.Context, messages []agent.Message, tools []string) (agent.Response, error) {
	return p.next(ctx, messages, tools, nil)
}

func (p TheosesMessages) NextWithEvents(ctx context.Context, messages []agent.Message, tools []string, emit func(agent.StreamEvent)) (agent.Response, error) {
	return p.next(ctx, messages, tools, emit)
}

func (p TheosesMessages) NextWithUpdates(ctx context.Context, messages []agent.Message, tools []string, update func(string)) (agent.Response, error) {
	return p.next(ctx, messages, tools, func(event agent.StreamEvent) {
		if event.Type == "text_delta" && update != nil {
			update(event.Delta)
		}
	})
}

func (p TheosesMessages) ListModels(ctx context.Context) ([]ModelInfo, error) {
	if strings.TrimSpace(p.GatewayURL) == "" {
		return nil, errors.New("radius gateway URL is required")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(p.GatewayURL, "/")+"/v1/config", nil)
	if err != nil {
		return nil, err
	}
	if p.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("radius catalog returned %s", response.Status)
	}
	var payload struct {
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	models := make([]ModelInfo, 0, len(payload.Models))
	for _, model := range payload.Models {
		if strings.TrimSpace(model.ID) != "" {
			models = append(models, ModelInfo{Provider: p.providerName(), ID: model.ID})
		}
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
}

func (p TheosesMessages) next(ctx context.Context, messages []agent.Message, toolNames []string, emit func(agent.StreamEvent)) (agent.Response, error) {
	if strings.TrimSpace(p.APIKey) == "" {
		return agent.Response{}, fmt.Errorf("no API key for provider: %s", p.providerName())
	}
	if strings.TrimSpace(p.Model) == "" {
		return agent.Response{}, errors.New("radius model is required")
	}
	payload := struct {
		Model   string         `json:"model"`
		Context radiusContext  `json:"context"`
		Options map[string]any `json:"options"`
	}{
		Model:   p.Model,
		Context: radiusContext{Messages: radiusMessages(messages), Tools: radiusTools(toolNames)},
		Options: map[string]any{},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return agent.Response{}, err
	}
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.BaseURL, "/")+"/messages", strings.NewReader(string(body)))
	if err != nil {
		return agent.Response{}, err
	}
	request.Header.Set("Authorization", "Bearer "+p.APIKey)
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return agent.Response{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 16<<10))
		return agent.Response{}, fmt.Errorf("radius returned %s: %s", response.Status, strings.TrimSpace(string(data)))
	}
	result := agent.Response{Provider: p.providerName(), Model: p.Model}
	partial := agent.Message{Role: "assistant", Provider: p.providerName(), Model: p.Model}
	arguments := map[int]string{}
	toolCalls := map[int]agent.ToolCall{}
	if emit != nil {
		emit(agent.StreamEvent{Type: "start", Partial: partial})
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var event radiusEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return agent.Response{}, err
		}
		index := event.ContentIndex
		switch event.Type {
		case "text_delta":
			result.Text += event.Delta
			partial.Content += event.Delta
			if emit != nil {
				emit(agent.StreamEvent{Type: event.Type, ContentIndex: index, Delta: event.Delta, Partial: partial})
			}
		case "text_end":
			result.Text = event.Content
			partial.Content = event.Content
			result.TextSignature = event.ContentSignature
			partial.TextSignature = event.ContentSignature
			if emit != nil {
				emit(agent.StreamEvent{Type: event.Type, ContentIndex: index, Partial: partial})
			}
		case "thinking_delta":
			result.Thinking += event.Delta
			partial.Thinking += event.Delta
			if emit != nil {
				emit(agent.StreamEvent{Type: event.Type, ContentIndex: index, Delta: event.Delta, Partial: partial})
			}
		case "thinking_end":
			if event.Content != "" {
				result.Thinking = event.Content
				partial.Thinking = event.Content
			}
			result.ThinkingSignature = event.ContentSignature
			partial.ThinkingSignature = event.ContentSignature
		case "toolcall_start":
			toolCalls[index] = agent.ToolCall{ID: event.ID, Name: event.ToolName}
			if emit != nil {
				call := toolCalls[index]
				emit(agent.StreamEvent{Type: event.Type, ContentIndex: index, ToolCall: &call, Partial: partial})
			}
		case "toolcall_delta":
			arguments[index] += event.Delta
			if emit != nil {
				emit(agent.StreamEvent{Type: event.Type, ContentIndex: index, Delta: event.Delta, Partial: partial})
			}
		case "toolcall_end":
			if event.ToolCall != nil {
				toolCalls[index] = agent.ToolCall{ID: event.ToolCall.ID, Name: event.ToolCall.Name, Args: event.ToolCall.Arguments}
			}
		case "done":
			result.StopReason = radiusStopReason(event.Reason)
			result.ResponseID = event.ResponseID
			result.Usage = event.Usage.agentUsage()
		case "error":
			result.StopReason = "error"
			result.ErrorMessage = event.ErrorMessage
			result.ResponseID = event.ResponseID
			result.Usage = event.Usage.agentUsage()
		}
	}
	if err := scanner.Err(); err != nil {
		return agent.Response{}, err
	}
	indexes := make([]int, 0, len(toolCalls))
	for index := range toolCalls {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	for _, index := range indexes {
		call := toolCalls[index]
		if raw := arguments[index]; raw != "" {
			if err := json.Unmarshal([]byte(raw), &call.Args); err != nil {
				return agent.Response{}, fmt.Errorf("radius tool %s arguments: %w", call.Name, err)
			}
		}
		result.ToolCalls = append(result.ToolCalls, call)
		partial.ToolCalls = append(partial.ToolCalls, call)
		if emit != nil {
			emit(agent.StreamEvent{Type: "toolcall_end", ContentIndex: index, ToolCall: &call, Partial: partial})
		}
	}
	if result.StopReason == "" {
		return agent.Response{}, errors.New("radius stream ended without a terminal event")
	}
	if emit != nil {
		emit(agent.StreamEvent{Type: "done", Partial: partial})
	}
	return result, nil
}

type radiusContext struct {
	Messages []radiusMessage  `json:"messages"`
	Tools    []map[string]any `json:"tools,omitempty"`
}

type radiusMessage struct {
	Role              string           `json:"role"`
	Content           string           `json:"content,omitempty"`
	TextSignature     string           `json:"textSignature,omitempty"`
	Thinking          string           `json:"thinking,omitempty"`
	ThinkingSignature string           `json:"thinkingSignature,omitempty"`
	Images            []string         `json:"images,omitempty"`
	ToolCalls         []agent.ToolCall `json:"toolCalls,omitempty"`
	ToolCallID        string           `json:"toolCallId,omitempty"`
	ErrorMessage      string           `json:"errorMessage,omitempty"`
}

func radiusMessages(messages []agent.Message) []radiusMessage {
	result := make([]radiusMessage, 0, len(messages))
	for _, message := range messages {
		result = append(result, radiusMessage{Role: message.Role, Content: message.Content, TextSignature: message.TextSignature, Thinking: message.Thinking, ThinkingSignature: message.ThinkingSignature, Images: message.Images, ToolCalls: message.ToolCalls, ToolCallID: message.ToolCallID, ErrorMessage: message.ErrorMessage})
	}
	return result
}

func radiusTools(names []string) []map[string]any {
	result := make([]map[string]any, 0, len(names))
	for _, name := range names {
		result = append(result, map[string]any{"name": name, "description": name, "parameters": toolParameters(name)})
	}
	return result
}

type radiusEvent struct {
	Type             string          `json:"type"`
	ContentIndex     int             `json:"contentIndex"`
	Delta            string          `json:"delta"`
	Content          string          `json:"content"`
	ContentSignature string          `json:"contentSignature"`
	ID               string          `json:"id"`
	ToolName         string          `json:"toolName"`
	Reason           string          `json:"reason"`
	ResponseID       string          `json:"responseId"`
	Usage            radiusUsage     `json:"usage"`
	ErrorMessage     string          `json:"errorMessage"`
	ToolCall         *radiusToolCall `json:"toolCall"`
}

type radiusToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type radiusUsage struct {
	Input       int `json:"input"`
	Output      int `json:"output"`
	Reasoning   int `json:"reasoning"`
	CacheRead   int `json:"cacheRead"`
	CacheWrite  int `json:"cacheWrite"`
	TotalTokens int `json:"totalTokens"`
}

func (u radiusUsage) agentUsage() agent.Usage {
	return agent.Usage{Input: u.Input, Output: u.Output, Reasoning: u.Reasoning, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite, TotalTokens: u.TotalTokens}
}

func (p TheosesMessages) providerName() string {
	if p.ProviderName == "" {
		return "radius"
	}
	return p.ProviderName
}

func radiusStopReason(reason string) string {
	switch reason {
	case "toolUse", "tool_use":
		return "toolUse"
	case "length", "max_tokens":
		return "length"
	case "stop", "end_turn":
		return "stop"
	case "error", "aborted":
		return reason
	default:
		return reason
	}
}

var _ agent.Provider = TheosesMessages{}
var _ agent.StreamingProviderWithEvents = TheosesMessages{}
