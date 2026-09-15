package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/H4fizWasabie/yen/internal/agent"
)

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content,omitempty"`
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
	BaseURL         string
	APIKey          string
	Model           string
	ReasoningEffort string
	Client          *http.Client
	MaxRetries      int
	MaxRetryDelay   time.Duration
}

func NewOpenAICompletions(baseURL, apiKey, model string) OpenAICompletions {
	return OpenAICompletions{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, Model: model}
}

func (p OpenAICompletions) Next(ctx context.Context, messages []agent.Message, toolNames []string) (agent.Response, error) {
	return p.nextWithUpdates(ctx, messages, toolNames, nil, nil, false)
}

func (p OpenAICompletions) NextWithUpdates(ctx context.Context, messages []agent.Message, toolNames []string, update func(string)) (agent.Response, error) {
	return p.nextWithUpdates(ctx, messages, toolNames, update, nil, false)
}

func (p OpenAICompletions) NextWithEvents(ctx context.Context, messages []agent.Message, toolNames []string, emit func(agent.StreamEvent)) (agent.Response, error) {
	return p.nextWithUpdates(ctx, messages, toolNames, nil, emit, false)
}

// NextJSON requests the provider's object-mode response format for structured
// calls such as memory consolidation. Ordinary turns keep the existing wire shape.
func (p OpenAICompletions) NextJSON(ctx context.Context, messages []agent.Message, toolNames []string) (agent.Response, error) {
	return p.nextWithUpdates(ctx, messages, toolNames, nil, nil, true)
}

func (p OpenAICompletions) nextWithUpdates(ctx context.Context, messages []agent.Message, toolNames []string, update func(string), emit func(agent.StreamEvent), jsonMode bool) (agent.Response, error) {
	payload := struct {
		Model          string            `json:"model"`
		Messages       []openAIMessage   `json:"messages"`
		Tools          []map[string]any  `json:"tools,omitempty"`
		Stream         bool              `json:"stream"`
		ResponseFormat map[string]string `json:"response_format,omitempty"`
		Reasoning      map[string]string `json:"reasoning,omitempty"`
	}{Model: p.Model, Messages: convertMessages(messages), Stream: true}
	if p.ReasoningEffort != "" {
		payload.Reasoning = map[string]string{"effort": p.ReasoningEffort}
	}
	if jsonMode {
		payload.ResponseFormat = map[string]string{"type": "json_object"}
	}
	for _, name := range toolNames {
		parameters := map[string]any{"type": "object"}
		if name == "read" {
			parameters = map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":   map[string]any{"type": "string", "description": "Path to the file to read (relative or absolute)"},
					"offset": map[string]any{"type": "number", "description": "Line number to start reading from (1-indexed)"},
					"limit":  map[string]any{"type": "number", "description": "Maximum number of lines to read"},
				},
				"required": []string{"path"},
			}
		} else if name == "remember" {
			parameters = map[string]any{
				"type":       "object",
				"properties": map[string]any{"query": map[string]any{"type": "string", "description": "What to recall from memory"}},
				"required":   []string{"query"},
			}
		} else if name == "save_note" {
			parameters = map[string]any{
				"type":       "object",
				"properties": map[string]any{"note": map[string]any{"type": "string", "description": "A present, durable fact worth remembering"}},
				"required":   []string{"note"},
			}
		} else if name == "recall_turns" {
			parameters = map[string]any{
				"type":       "object",
				"properties": map[string]any{"query": map[string]any{"type": "string", "description": "What to search for in this session's past turns"}},
				"required":   []string{"query"},
			}
		}
		payload.Tools = append(payload.Tools, map[string]any{
			"type":     "function",
			"function": map[string]any{"name": name, "parameters": parameters},
		})
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return agent.Response{}, err
	}
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	var response *http.Response
	for attempt := 0; ; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/chat/completions", strings.NewReader(string(body)))
		if err != nil {
			return agent.Response{}, err
		}
		request.Header.Set("Content-Type", "application/json")
		if p.APIKey != "" {
			request.Header.Set("Authorization", "Bearer "+p.APIKey)
		}
		response, err = client.Do(request)
		if err != nil {
			return agent.Response{}, err
		}
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			break
		}
		retryable := response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusConflict || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		if response.Header.Get("x-should-retry") == "true" {
			retryable = true
		} else if response.Header.Get("x-should-retry") == "false" {
			retryable = false
		}
		if !retryable || attempt >= p.MaxRetries {
			body, _ := io.ReadAll(io.LimitReader(response.Body, 16<<10))
			response.Body.Close()
			if len(body) > 0 {
				return agent.Response{}, fmt.Errorf("openai completions returned %s: %s", response.Status, strings.TrimSpace(string(body)))
			}
			return agent.Response{}, fmt.Errorf("openai completions returned %s", response.Status)
		}
		response.Body.Close()
		delay := retryDelay(response.Header, attempt)
		if p.MaxRetryDelay > 0 && delay > p.MaxRetryDelay {
			return agent.Response{}, fmt.Errorf("provider retry delay %s exceeds maximum %s", delay, p.MaxRetryDelay)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return agent.Response{}, ctx.Err()
		case <-timer.C:
		}
	}
	defer response.Body.Close()

	var result agent.Response
	result.Provider = "openai-completions"
	result.Model = p.Model
	var toolCalls []agent.ToolCall
	arguments := map[string]string{}
	partial := agent.Message{Role: "assistant", Provider: result.Provider, Model: result.Model}
	startedText := false
	startedThinking := false
	startedTools := map[int]bool{}
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
			ID    string `json:"id"`
			Model string `json:"model"`
			Usage *struct {
				PromptTokens         int `json:"prompt_tokens"`
				CachedTokens         int `json:"cached_tokens"`
				PromptCacheHitTokens int `json:"prompt_cache_hit_tokens"`
				CompletionTokens     int `json:"completion_tokens"`
				PromptTokensDetails  *struct {
					CachedTokens     int `json:"cached_tokens"`
					CacheWriteTokens int `json:"cache_write_tokens"`
				} `json:"prompt_tokens_details"`
				CompletionTokensDetails *struct {
					ReasoningTokens int `json:"reasoning_tokens"`
				} `json:"completion_tokens_details"`
			} `json:"usage"`
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					Reasoning        string `json:"reasoning"`
					ReasoningContent string `json:"reasoning_content"`
					ReasoningText    string `json:"reasoning_text"`
					ToolCalls        []struct {
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
		if event.ID != "" {
			result.ResponseID = event.ID
		}
		if event.Model != "" && event.Model != p.Model {
			result.ResponseModel = event.Model
		}
		if event.Usage != nil {
			result.Usage.Output = event.Usage.CompletionTokens
			if event.Usage.CompletionTokensDetails != nil {
				result.Usage.Reasoning = event.Usage.CompletionTokensDetails.ReasoningTokens
			}
			result.Usage.CacheRead = event.Usage.CachedTokens
			if event.Usage.PromptCacheHitTokens != 0 {
				result.Usage.CacheRead = event.Usage.PromptCacheHitTokens
			}
			if event.Usage.PromptTokensDetails != nil {
				result.Usage.CacheRead = event.Usage.PromptTokensDetails.CachedTokens
				result.Usage.CacheWrite = event.Usage.PromptTokensDetails.CacheWriteTokens
			}
			result.Usage.Input = max(0, event.Usage.PromptTokens-result.Usage.CacheRead-result.Usage.CacheWrite)
			result.Usage.TotalTokens = result.Usage.Input + result.Usage.Output + result.Usage.CacheRead + result.Usage.CacheWrite
		}
		for _, choice := range event.Choices {
			reasoning := choice.Delta.ReasoningContent
			if reasoning == "" {
				reasoning = choice.Delta.Reasoning
			}
			if reasoning == "" {
				reasoning = choice.Delta.ReasoningText
			}
			if reasoning != "" {
				if emit != nil && !startedThinking {
					startedThinking = true
					emit(agent.StreamEvent{Type: "thinking_start", ContentIndex: 0, Partial: partial})
				}
				partial.Thinking += reasoning
				if emit != nil {
					emit(agent.StreamEvent{Type: "thinking_delta", ContentIndex: 0, Delta: reasoning, Partial: partial})
				}
			}
			result.Text += choice.Delta.Content
			if choice.Delta.Content != "" {
				if emit != nil && !startedText {
					startedText = true
					emit(agent.StreamEvent{Type: "text_start", ContentIndex: 0, Partial: partial})
				}
				partial.Content += choice.Delta.Content
				if emit != nil {
					emit(agent.StreamEvent{Type: "text_delta", ContentIndex: 0, Delta: choice.Delta.Content, Partial: partial})
				}
			}
			if update != nil && choice.Delta.Content != "" {
				update(choice.Delta.Content)
			}
			if choice.FinishReason != nil {
				result.RawStopReason = *choice.FinishReason
				result.StopReason, result.ErrorMessage = mapStopReason(*choice.FinishReason)
			}
			for _, delta := range choice.Delta.ToolCalls {
				if update != nil {
					update("tool")
				}
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
				partial.ToolCalls = append([]agent.ToolCall(nil), toolCalls...)
				if emit != nil {
					if !startedTools[delta.Index] {
						startedTools[delta.Index] = true
						emit(agent.StreamEvent{Type: "toolcall_start", ContentIndex: delta.Index, Partial: partial})
					}
					if delta.Function.Arguments != "" {
						emit(agent.StreamEvent{Type: "toolcall_delta", ContentIndex: delta.Index, Delta: delta.Function.Arguments, Partial: partial})
					}
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return agent.Response{}, err
	}
	if result.StopReason == "" {
		return agent.Response{}, fmt.Errorf("openai completions stream ended without finish_reason")
	}
	for index := range toolCalls {
		var args map[string]any
		if raw := arguments[fmt.Sprint(index)]; raw != "" {
			if err := json.Unmarshal([]byte(raw), &args); err != nil {
				return agent.Response{}, fmt.Errorf("tool arguments: %w", err)
			}
		}
		toolCalls[index].Args = args
		if emit != nil {
			call := toolCalls[index]
			partial.ToolCalls = append([]agent.ToolCall(nil), toolCalls...)
			emit(agent.StreamEvent{Type: "toolcall_end", ContentIndex: index, ToolCall: &call, Partial: partial})
		}
	}
	if emit != nil && startedText {
		emit(agent.StreamEvent{Type: "text_end", ContentIndex: 0, Partial: partial})
	}
	if emit != nil && startedThinking {
		emit(agent.StreamEvent{Type: "thinking_end", ContentIndex: 0, Partial: partial})
	}
	result.Thinking = partial.Thinking
	result.ToolCalls = toolCalls
	if len(toolCalls) > 0 && result.StopReason == "" {
		result.StopReason = "toolUse"
	}
	return result, nil
}

func mapStopReason(reason string) (string, string) {
	switch reason {
	case "stop", "end":
		return "stop", ""
	case "length":
		return "length", ""
	case "tool_calls", "function_call":
		return "toolUse", ""
	case "content_filter", "network_error":
		return "error", "Provider finish_reason: " + reason
	default:
		return "error", "Provider finish_reason: " + reason
	}
}

func retryDelay(header http.Header, attempt int) time.Duration {
	if value, err := strconv.ParseFloat(header.Get("retry-after-ms"), 64); err == nil {
		return time.Duration(value * float64(time.Millisecond))
	}
	return time.Duration(500*(1<<min(attempt, 4))) * time.Millisecond
}

func convertMessages(messages []agent.Message) []openAIMessage {
	converted := make([]openAIMessage, 0, len(messages))
	for _, message := range messages {
		var content any = message.Content
		if len(message.Images) > 0 {
			parts := make([]map[string]any, 0, len(message.Images)+1)
			if message.Content != "" {
				parts = append(parts, map[string]any{"type": "text", "text": message.Content})
			}
			for _, image := range message.Images {
				parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]string{"url": image}})
			}
			content = parts
		}
		convertedMessage := openAIMessage{Role: message.Role, Content: content, ToolCallID: message.ToolCallID}
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
