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
	"unicode/utf16"

	"github.com/H4fizWasabie/yen/internal/agent"
)

type openAIMessage struct {
	Role             string            `json:"role"`
	Content          any               `json:"content,omitempty"`
	Reasoning        string            `json:"reasoning,omitempty"`
	ReasoningContent string            `json:"reasoning_content,omitempty"`
	ReasoningText    string            `json:"reasoning_text,omitempty"`
	ReasoningDetails []json.RawMessage `json:"reasoning_details,omitempty"`
	ToolCalls        []openAIToolCall  `json:"tool_calls,omitempty"`
	ToolCallID       string            `json:"tool_call_id,omitempty"`
	Name             string            `json:"name,omitempty"`
	Prefix           *bool             `json:"prefix,omitempty"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Index    *int   `json:"index,omitempty"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIUsage struct {
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
}

type OpenAICompletions struct {
	BaseURL         string
	APIKey          string
	Headers         map[string]string
	ProviderRouting map[string]any
	Model           string
	ProviderName    string
	ReasoningEffort string
	MaxTokens       int
	Client          *http.Client
	MaxRetries      int
	MaxRetryDelay   time.Duration
	Timeout         time.Duration
}

func (p OpenAICompletions) ListModels(ctx context.Context) ([]ModelInfo, error) {
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	if p.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	for name, value := range p.Headers {
		request.Header.Set(name, value)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("model catalog returned %s", response.Status)
	}
	var payload struct {
		Data []struct {
			ID                 string `json:"id"`
			ModelPickerEnabled *bool  `json:"model_picker_enabled"`
			Capabilities       struct {
				Supports struct {
					ToolCalls *bool `json:"tool_calls"`
				} `json:"supports"`
			} `json:"capabilities"`
			Policy struct {
				State string `json:"state"`
			} `json:"policy"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	data := payload.Data
	if p.ProviderName == "github-copilot" {
		pickerIDs := make(map[string]struct{})
		policyIDs := make(map[string]struct{})
		metadata := false
		for _, model := range data {
			if model.Capabilities.Supports.ToolCalls != nil && !*model.Capabilities.Supports.ToolCalls {
				continue
			}
			if model.ModelPickerEnabled != nil || model.Policy.State != "" || model.Capabilities.Supports.ToolCalls != nil {
				metadata = true
			}
			if model.ModelPickerEnabled != nil && *model.ModelPickerEnabled && model.Policy.State != "disabled" {
				pickerIDs[model.ID] = struct{}{}
			}
			if model.Policy.State == "enabled" {
				policyIDs[model.ID] = struct{}{}
			}
		}
		if metadata {
			allowed := pickerIDs
			if len(allowed) == 0 {
				allowed = policyIDs
			}
			filtered := data[:0]
			for _, model := range data {
				if _, ok := allowed[model.ID]; ok {
					filtered = append(filtered, model)
				}
			}
			data = filtered
		}
	}
	models := make([]ModelInfo, 0, len(data))
	for _, model := range data {
		if strings.TrimSpace(model.ID) != "" {
			models = append(models, ModelInfo{Provider: p.ProviderName, ID: model.ID})
		}
	}
	return models, nil
}

func NewOpenAICompletions(baseURL, apiKey, model string) OpenAICompletions {
	return OpenAICompletions{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, Model: model}
}
func (p OpenAICompletions) ProviderID() string { return p.ProviderName }

func (p OpenAICompletions) Next(ctx context.Context, messages []agent.Message, toolNames []string) (agent.Response, error) {
	return p.nextWithUpdates(ctx, messages, toolNames, nil, nil, false)
}

func (p OpenAICompletions) NextWithUpdates(ctx context.Context, messages []agent.Message, toolNames []string, update func(string)) (agent.Response, error) {
	return p.nextWithUpdates(ctx, messages, toolNames, update, nil, false)
}

func (p OpenAICompletions) NextWithEvents(ctx context.Context, messages []agent.Message, toolNames []string, emit func(agent.StreamEvent)) (agent.Response, error) {
	return p.nextWithUpdates(ctx, messages, toolNames, nil, emit, false)
}

func (p OpenAICompletions) NextWithToolDefinitions(ctx context.Context, messages []agent.Message, definitions []agent.ToolDefinition) (agent.Response, error) {
	return p.nextWithUpdates(ctx, messages, toolDefinitionNames(definitions), nil, nil, false, definitions)
}

func (p OpenAICompletions) NextWithToolDefinitionsAndEvents(ctx context.Context, messages []agent.Message, definitions []agent.ToolDefinition, emit func(agent.StreamEvent)) (agent.Response, error) {
	return p.nextWithUpdates(ctx, messages, toolDefinitionNames(definitions), nil, emit, false, definitions)
}

// NextJSON requests the provider's object-mode response format for structured
// calls such as memory consolidation. Ordinary turns keep the existing wire shape.
func (p OpenAICompletions) NextJSON(ctx context.Context, messages []agent.Message, toolNames []string) (agent.Response, error) {
	return p.nextWithUpdates(ctx, messages, toolNames, nil, nil, true)
}

func (p OpenAICompletions) nextWithUpdates(ctx context.Context, messages []agent.Message, toolNames []string, update func(string), emit func(agent.StreamEvent), jsonMode bool, definitionSets ...[]agent.ToolDefinition) (agent.Response, error) {
	if p.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.Timeout)
		defer cancel()
	}
	if key, ok := agent.APIKeyFromContext(ctx); ok {
		p.APIKey = key
	}
	converted := convertMessages(messages)
	if p.ProviderName == "mistral" {
		converted = convertMistralMessages(messages)
	}
	payload := struct {
		Model                string            `json:"model"`
		Messages             []openAIMessage   `json:"messages"`
		Tools                []map[string]any  `json:"tools,omitempty"`
		Stream               bool              `json:"stream"`
		ResponseFormat       map[string]string `json:"response_format,omitempty"`
		Reasoning            map[string]any    `json:"reasoning,omitempty"`
		Thinking             map[string]any    `json:"thinking,omitempty"`
		EnableThinking       *bool             `json:"enable_thinking,omitempty"`
		ToolStream           bool              `json:"tool_stream,omitempty"`
		ChatTemplateArgs     map[string]any    `json:"chat_template_args,omitempty"`
		ReasoningEffort      string            `json:"reasoning_effort,omitempty"`
		ProviderRouting      map[string]any    `json:"provider,omitempty"`
		PromptCacheKey       string            `json:"prompt_cache_key,omitempty"`
		PromptCacheRetention string            `json:"prompt_cache_retention,omitempty"`
		MaxCompletionTokens  int               `json:"max_completion_tokens,omitempty"`
	}{Model: p.Model, Messages: converted, Stream: true}
	if supportsPromptCaching(p.ProviderName, p.BaseURL) {
		payload.PromptCacheKey, payload.PromptCacheRetention = promptCacheSettings(ctx)
	}
	payload.ProviderRouting = p.ProviderRouting
	payload.MaxCompletionTokens = p.MaxTokens
	if p.ReasoningEffort != "" {
		if p.ProviderName == "mistral" {
			payload.ReasoningEffort = p.ReasoningEffort
		} else if p.ProviderName == "zai" || p.ProviderName == "zai-coding-cn" {
			payload.Thinking = map[string]any{"type": "enabled", "clear_thinking": false}
			payload.ToolStream = true
		} else if isQwenTokenPlan(p.ProviderName) {
			enabled := true
			payload.EnableThinking = &enabled
		} else if p.ProviderName == "deepseek" {
			payload.Thinking = map[string]any{"type": "enabled"}
		} else if p.ProviderName == "together" {
			payload.Reasoning = map[string]any{"enabled": true}
			payload.ReasoningEffort = p.ReasoningEffort
		} else if p.ProviderName == "baseten" && basetenUsesChatTemplate(p.Model) {
			payload.ChatTemplateArgs = map[string]any{"enable_thinking": true}
		} else {
			payload.Reasoning = map[string]any{"effort": p.ReasoningEffort}
		}
	} else if isQwenTokenPlan(p.ProviderName) {
		enabled := false
		payload.EnableThinking = &enabled
	} else if p.ProviderName == "deepseek" {
		payload.Thinking = map[string]any{"type": "disabled"}
	} else if p.ProviderName == "together" {
		payload.Reasoning = map[string]any{"enabled": false}
	}
	if p.ProviderName == "baseten" && basetenUsesChatTemplate(p.Model) && p.ReasoningEffort == "" {
		payload.ChatTemplateArgs = map[string]any{"enable_thinking": false}
	}
	if jsonMode {
		payload.ResponseFormat = map[string]string{"type": "json_object"}
	}
	definitions := make([]agent.ToolDefinition, 0, len(toolNames))
	if len(definitionSets) > 0 {
		definitions = definitionSets[0]
	}
	for _, name := range toolNames {
		definition := agent.ToolDefinition{Name: name, Description: name, Parameters: toolParameters(name)}
		for _, candidate := range definitions {
			if candidate.Name == name {
				definition = candidate
				break
			}
		}
		payload.Tools = append(payload.Tools, map[string]any{
			"type":     "function",
			"function": map[string]any{"name": name, "description": definition.Description, "parameters": definition.Parameters},
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
		if p.ProviderName == "github-copilot" {
			applyCopilotHeaders(request.Header, messages)
		}
		if p.APIKey != "" {
			request.Header.Set("Authorization", "Bearer "+p.APIKey)
		}
		for name, value := range p.Headers {
			request.Header.Set(name, value)
		}
		applyProviderHeaderHook(ctx, request.Header)
		response, err = client.Do(request)
		if err != nil {
			return agent.Response{}, err
		}
		applyProviderResponseHook(ctx, response)
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
			delay = p.MaxRetryDelay
		}
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
	result.Provider = p.ProviderName
	if result.Provider == "" {
		result.Provider = "openai-completions"
	}
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
			ID      string       `json:"id"`
			Model   string       `json:"model"`
			Usage   *openAIUsage `json:"usage"`
			Choices []struct {
				Usage *openAIUsage `json:"usage"`
				Delta struct {
					Content          json.RawMessage   `json:"content"`
					Reasoning        string            `json:"reasoning"`
					ReasoningContent string            `json:"reasoning_content"`
					ReasoningText    string            `json:"reasoning_text"`
					ReasoningDetails []json.RawMessage `json:"reasoning_details"`
					ToolCalls        []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string          `json:"name"`
							Arguments json.RawMessage `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason json.RawMessage `json:"finish_reason"`
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
			applyOpenAIUsage(&result, event.Usage)
		}
		for _, choice := range event.Choices {
			if event.Usage == nil && choice.Usage != nil {
				applyOpenAIUsage(&result, choice.Usage)
			}
			reasoning := choice.Delta.ReasoningContent
			if reasoning == "" {
				reasoning = choice.Delta.Reasoning
			}
			if reasoning == "" {
				reasoning = choice.Delta.ReasoningText
			}
			if reasoning != "" {
				if partial.ThinkingSignature == "" {
					if choice.Delta.ReasoningContent != "" {
						partial.ThinkingSignature = "reasoning_content"
					} else if choice.Delta.Reasoning != "" {
						partial.ThinkingSignature = "reasoning"
					} else {
						partial.ThinkingSignature = "reasoning_text"
					}
				}
				if emit != nil && !startedThinking {
					startedThinking = true
					emit(agent.StreamEvent{Type: "thinking_start", ContentIndex: 0, Partial: partial})
				}
				partial.Thinking += reasoning
				if emit != nil {
					emit(agent.StreamEvent{Type: "thinking_delta", ContentIndex: 0, Delta: reasoning, Partial: partial})
				}
			}
			if len(choice.Delta.ReasoningDetails) > 0 {
				var details []json.RawMessage
				if strings.HasPrefix(strings.TrimSpace(partial.ThinkingSignature), "[") {
					_ = json.Unmarshal([]byte(partial.ThinkingSignature), &details)
				}
				for _, detail := range choice.Delta.ReasoningDetails {
					var object map[string]any
					if json.Unmarshal(detail, &object) == nil && object["type"] != nil {
						details = append(details, append(json.RawMessage(nil), detail...))
					}
				}
				if len(details) > 0 {
					encoded, _ := json.Marshal(details)
					partial.ThinkingSignature = string(encoded)
				}
			}
			text, reasoningChunk := openAIContentDelta(choice.Delta.Content)
			if reasoningChunk != "" {
				if partial.ThinkingSignature == "" {
					partial.ThinkingSignature = "reasoning"
				}
				if emit != nil && !startedThinking {
					startedThinking = true
					emit(agent.StreamEvent{Type: "thinking_start", ContentIndex: 0, Partial: partial})
				}
				partial.Thinking += reasoningChunk
				if emit != nil {
					emit(agent.StreamEvent{Type: "thinking_delta", ContentIndex: 0, Delta: reasoningChunk, Partial: partial})
				}
			}
			result.Text += text
			if text != "" {
				if emit != nil && !startedText {
					startedText = true
					emit(agent.StreamEvent{Type: "text_start", ContentIndex: 0, Partial: partial})
				}
				partial.Content += text
				if emit != nil {
					emit(agent.StreamEvent{Type: "text_delta", ContentIndex: 0, Delta: text, Partial: partial})
				}
			}
			if update != nil && text != "" {
				update(text)
			}
			if len(choice.FinishReason) > 0 {
				if string(choice.FinishReason) == "null" {
					result.RawStopReason = ""
					result.StopReason, result.ErrorMessage = "stop", ""
				} else {
					var reason string
					if err := json.Unmarshal(choice.FinishReason, &reason); err != nil {
						return agent.Response{}, fmt.Errorf("finish reason: %w", err)
					}
					result.RawStopReason = reason
					result.StopReason, result.ErrorMessage = mapStopReason(reason)
				}
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
				argsDelta := openAIToolArguments(delta.Function.Arguments)
				arguments[fmt.Sprint(delta.Index)] += argsDelta
				partial.ToolCalls = append([]agent.ToolCall(nil), toolCalls...)
				if emit != nil {
					if !startedTools[delta.Index] {
						startedTools[delta.Index] = true
						emit(agent.StreamEvent{Type: "toolcall_start", ContentIndex: delta.Index, Partial: partial})
					}
					if argsDelta != "" {
						emit(agent.StreamEvent{Type: "toolcall_delta", ContentIndex: delta.Index, Delta: argsDelta, Partial: partial})
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
		if p.ProviderName == "mistral" && toolCalls[index].ID == "" {
			toolCalls[index].ID = normalizeMistralToolID("toolcall:" + fmt.Sprint(index))
		}
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
	result.ThinkingSignature = partial.ThinkingSignature
	result.ToolCalls = toolCalls
	if len(toolCalls) > 0 && result.StopReason == "" {
		result.StopReason = "toolUse"
	}
	return result, nil
}

func toolDefinitionNames(definitions []agent.ToolDefinition) []string {
	result := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		if definition.Name != "" {
			result = append(result, definition.Name)
		}
	}
	return result
}

func applyOpenAIUsage(result *agent.Response, usage *openAIUsage) {
	result.Usage.Output = usage.CompletionTokens
	if usage.CompletionTokensDetails != nil {
		result.Usage.Reasoning = usage.CompletionTokensDetails.ReasoningTokens
	}
	result.Usage.CacheRead = usage.CachedTokens
	if usage.PromptCacheHitTokens != 0 {
		result.Usage.CacheRead = usage.PromptCacheHitTokens
	}
	if usage.PromptTokensDetails != nil {
		result.Usage.CacheRead = usage.PromptTokensDetails.CachedTokens
		result.Usage.CacheWrite = usage.PromptTokensDetails.CacheWriteTokens
	}
	result.Usage.Input = max(0, usage.PromptTokens-result.Usage.CacheRead-result.Usage.CacheWrite)
	result.Usage.TotalTokens = result.Usage.Input + result.Usage.Output + result.Usage.CacheRead + result.Usage.CacheWrite
}

func openAIToolArguments(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	return string(raw)
}

func copilotInitiator(messages []agent.Message) string {
	if len(messages) > 0 && messages[len(messages)-1].Role != "user" {
		return "agent"
	}
	return "user"
}

func hasMessageImages(messages []agent.Message) bool {
	for _, message := range messages {
		if len(message.Images) > 0 {
			return true
		}
	}
	return false
}

func applyCopilotHeaders(headers http.Header, messages []agent.Message) {
	headers.Set("X-Initiator", copilotInitiator(messages))
	headers.Set("Openai-Intent", "conversation-edits")
	if hasMessageImages(messages) {
		headers.Set("Copilot-Vision-Request", "true")
	}
}

func isQwenTokenPlan(provider string) bool {
	return provider == "qwen-token-plan" || provider == "qwen-token-plan-cn" || provider == "qwen-token-plan-individual"
}

func basetenUsesChatTemplate(model string) bool {
	return strings.HasPrefix(model, "moonshotai/") || strings.HasPrefix(model, "nvidia/") || strings.HasPrefix(model, "zai-org/GLM-4.7") || strings.HasPrefix(model, "zai-org/GLM-5")
}

func openAIContentDelta(raw json.RawMessage) (text, thinking string) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", ""
	}
	if json.Unmarshal(raw, &text) == nil {
		return text, ""
	}
	var chunks []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		Thinking []struct {
			Text string `json:"text"`
		} `json:"thinking"`
	}
	if json.Unmarshal(raw, &chunks) != nil {
		return "", ""
	}
	for _, chunk := range chunks {
		if chunk.Type == "thinking" {
			for _, part := range chunk.Thinking {
				thinking += part.Text
			}
		} else {
			text += chunk.Text
		}
	}
	return text, thinking
}

func normalizeMistralMessages(messages []openAIMessage) []openAIMessage {
	result := append([]openAIMessage(nil), messages...)
	for i := range result {
		if result[i].Role == "assistant" {
			thinking := result[i].ReasoningContent
			if thinking == "" {
				thinking = result[i].Reasoning
			}
			if thinking == "" {
				thinking = result[i].ReasoningText
			}
			if thinking != "" {
				content, _ := result[i].Content.(string)
				chunks := []map[string]any{{"type": "thinking", "thinking": []map[string]string{{"type": "text", "text": thinking}}}}
				if content != "" {
					chunks = append(chunks, map[string]any{"type": "text", "text": content})
				}
				result[i].Content = chunks
				result[i].Reasoning, result[i].ReasoningContent, result[i].ReasoningText = "", "", ""
			}
		}
		result[i].ToolCallID = normalizeMistralToolID(result[i].ToolCallID)
		if len(result[i].ToolCalls) == 0 {
			continue
		}
		result[i].ToolCalls = append([]openAIToolCall(nil), result[i].ToolCalls...)
		for j := range result[i].ToolCalls {
			result[i].ToolCalls[j].ID = normalizeMistralToolID(result[i].ToolCalls[j].ID)
		}
	}
	return result
}

func convertMistralMessages(messages []agent.Message) []openAIMessage {
	result := normalizeMistralMessages(convertMessages(messages))
	for i := range result {
		message := &result[i]
		if len(messages) <= i {
			continue
		}
		if message.Role == "assistant" {
			prefix := false
			message.Prefix = &prefix
			if text, ok := message.Content.(string); ok && text != "" {
				message.Content = []map[string]any{{"type": "text", "text": text}}
			}
			for j := range message.ToolCalls {
				index := 0
				message.ToolCalls[j].Index = &index
			}
		}
		if message.Role == "tool" {
			message.Name = messages[i].ToolName
			parts := make([]map[string]any, 0, len(messages[i].Images)+1)
			if messages[i].Content != "" {
				parts = append(parts, map[string]any{"type": "text", "text": messages[i].Content})
			}
			message.Content = parts
		}
		if len(messages[i].Images) > 0 {
			parts := make([]map[string]any, 0, len(messages[i].Images)+1)
			if messages[i].Content != "" {
				parts = append(parts, map[string]any{"type": "text", "text": messages[i].Content})
			}
			for _, image := range messages[i].Images {
				parts = append(parts, map[string]any{"type": "image_url", "image_url": image})
			}
			message.Content = parts
		}
	}
	return result
}

func normalizeMistralToolID(id string) string {
	if id == "" {
		return id
	}
	var normalized strings.Builder
	for _, r := range id {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			normalized.WriteRune(r)
		}
	}
	value := normalized.String()
	if len(value) == 9 {
		return value
	}
	seed := value
	if seed == "" {
		seed = id
	}
	return mistralShortHash(seed)[:9]
}

func mistralShortHash(value string) string {
	var h1 uint32 = 0xdeadbeef
	var h2 uint32 = 0x41c6ce57
	for _, codeUnit := range utf16.Encode([]rune(value)) {
		ch := uint32(codeUnit)
		h1 = (h1 ^ ch) * 2654435761
		h2 = (h2 ^ ch) * 1597334677
	}
	h1 = ((h1 ^ (h1 >> 16)) * 2246822507) ^ ((h2 ^ (h2 >> 13)) * 3266489909)
	h2 = ((h2 ^ (h2 >> 16)) * 2246822507) ^ ((h1 ^ (h1 >> 13)) * 3266489909)
	return strconv.FormatUint(uint64(h2), 36) + strconv.FormatUint(uint64(h1), 36)
}

func toolParameters(name string) map[string]any {
	stringProperty := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	numberProperty := func(description string) map[string]any {
		return map[string]any{"type": "number", "description": description}
	}
	optional := func(properties map[string]any, required ...string) map[string]any {
		result := map[string]any{"type": "object", "properties": properties}
		if len(required) > 0 {
			result["required"] = required
		}
		return result
	}
	switch name {
	case "read":
		return optional(map[string]any{
			"path":   stringProperty("Path to the file to read (relative or absolute)"),
			"offset": numberProperty("Line number to start reading from (1-indexed)"),
			"limit":  numberProperty("Maximum number of lines to read"),
		}, "path")
	case "bash", "powershell":
		return optional(map[string]any{
			"command": stringProperty("Shell command to execute"),
			"timeout": numberProperty("Timeout in seconds (optional)"),
		}, "command")
	case "write":
		return optional(map[string]any{
			"path":    stringProperty("Path to the file to write (relative or absolute)"),
			"content": stringProperty("Content to write to the file"),
		}, "path", "content")
	case "edit":
		return optional(map[string]any{
			"path": stringProperty("Path to the file to edit (relative or absolute)"),
			"edits": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"oldText": stringProperty("Exact text to replace"),
						"newText": stringProperty("Replacement text"),
					},
					"required": []string{"oldText", "newText"},
				},
			},
		}, "path", "edits")
	case "grep":
		return optional(map[string]any{
			"pattern":    stringProperty("Search pattern (regex or literal string)"),
			"path":       stringProperty("Directory or file to search"),
			"glob":       stringProperty("File glob filter"),
			"ignoreCase": map[string]any{"type": "boolean"},
			"literal":    map[string]any{"type": "boolean"},
			"context":    numberProperty("Lines before and after matches"),
			"limit":      numberProperty("Maximum number of matches"),
		}, "pattern")
	case "find":
		return optional(map[string]any{
			"pattern": stringProperty("Glob pattern to match files"),
			"path":    stringProperty("Directory to search"),
			"limit":   numberProperty("Maximum number of results"),
		}, "pattern")
	case "ls":
		return optional(map[string]any{
			"path":  stringProperty("Directory to list"),
			"limit": numberProperty("Maximum number of entries"),
		})
	case "remember", "recall_turns":
		return optional(map[string]any{"query": stringProperty("What to search for")}, "query")
	case "save_note":
		return optional(map[string]any{"note": stringProperty("A present, durable fact worth remembering")}, "note")
	case "working_note":
		return optional(map[string]any{
			"note":  stringProperty("One concise fact to append to the Working Note"),
			"clear": map[string]any{"type": "boolean", "description": "Clear the Working Note"},
		})
	case "note_operations":
		return optional(map[string]any{
			"section": stringProperty("Section heading for the operational note"),
			"content": stringProperty("One concise durable operational line"),
		}, "section", "content")
	case "convert_doc":
		return optional(map[string]any{"path": stringProperty("Path to a document file")}, "path")
	case "web_search":
		return optional(map[string]any{"query": stringProperty("The web search query")}, "query")
	case "generate_image":
		return optional(map[string]any{"prompt": stringProperty("Detailed description of the image to generate")}, "prompt")
	case "explore":
		return optional(map[string]any{
			"question": stringProperty("The single scouting question to answer"),
			"tier":     stringProperty("quick-scan or deep-map"),
		}, "question")
	case "tool_search":
		return optional(map[string]any{"query": stringProperty("Tool name or capability to search for")}, "query")
	case "tool_call":
		return optional(map[string]any{"name": stringProperty("Exact deferred tool name"), "args": map[string]any{"type": "object"}}, "name")
	default:
		return map[string]any{"type": "object"}
	}
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
		if message.Thinking != "" || strings.HasPrefix(strings.TrimSpace(message.ThinkingSignature), "[") {
			if strings.HasPrefix(strings.TrimSpace(message.ThinkingSignature), "[") {
				_ = json.Unmarshal([]byte(message.ThinkingSignature), &convertedMessage.ReasoningDetails)
			} else {
				switch message.ThinkingSignature {
				case "reasoning":
					convertedMessage.Reasoning = message.Thinking
				case "reasoning_text":
					convertedMessage.ReasoningText = message.Thinking
				default:
					convertedMessage.ReasoningContent = message.Thinking
				}
			}
		}
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
