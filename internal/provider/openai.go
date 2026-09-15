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
	BaseURL       string
	APIKey        string
	Model         string
	Client        *http.Client
	MaxRetries    int
	MaxRetryDelay time.Duration
}

func NewOpenAICompletions(baseURL, apiKey, model string) OpenAICompletions {
	return OpenAICompletions{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, Model: model}
}

func (p OpenAICompletions) Next(ctx context.Context, messages []agent.Message, toolNames []string) (agent.Response, error) {
	return p.NextWithUpdates(ctx, messages, toolNames, nil)
}

func (p OpenAICompletions) NextWithUpdates(ctx context.Context, messages []agent.Message, toolNames []string, update func(string)) (agent.Response, error) {
	payload := struct {
		Model    string           `json:"model"`
		Messages []openAIMessage  `json:"messages"`
		Tools    []map[string]any `json:"tools,omitempty"`
		Stream   bool             `json:"stream"`
	}{Model: p.Model, Messages: convertMessages(messages), Stream: true}
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
			result.Text += choice.Delta.Content
			if update != nil && choice.Delta.Content != "" {
				update(choice.Delta.Content)
			}
			if choice.FinishReason != nil {
				result.StopReason = normalizeStopReason(*choice.FinishReason)
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
	}
	result.ToolCalls = toolCalls
	if len(toolCalls) > 0 && result.StopReason == "" {
		result.StopReason = "toolUse"
	}
	return result, nil
}

func normalizeStopReason(reason string) string {
	if reason == "tool_calls" || reason == "function_call" {
		return "toolUse"
	}
	return reason
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
