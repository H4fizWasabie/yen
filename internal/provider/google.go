package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/H4fizWasabie/yen/internal/agent"
)

// GoogleGenerativeAI is the native Gemini generateContent streaming protocol.
// It deliberately stays separate from OpenAICompletions: Gemini's roles,
// function calls, usage fields, and thinking parts have different wire rules.
type GoogleGenerativeAI struct {
	BaseURL       string
	APIKey        string
	BearerToken   string
	BearerSource  func(context.Context) (string, error)
	Model         string
	ProviderName  string
	ThinkingLevel string
	Client        *http.Client
	MaxRetries    int
	Timeout       time.Duration
	MaxRetryDelay time.Duration
}

func (p GoogleGenerativeAI) ProviderID() string {
	if p.ProviderName != "" {
		return p.ProviderName
	}
	return "google"
}

func NewGoogleGenerativeAI(baseURL, apiKey, model string) GoogleGenerativeAI {
	return GoogleGenerativeAI{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, Model: model}
}

func (p GoogleGenerativeAI) Next(ctx context.Context, messages []agent.Message, toolNames []string) (agent.Response, error) {
	return p.next(ctx, messages, toolNames, nil)
}

func (p GoogleGenerativeAI) NextWithUpdates(ctx context.Context, messages []agent.Message, toolNames []string, update func(string)) (agent.Response, error) {
	return p.next(ctx, messages, toolNames, update)
}

func (p GoogleGenerativeAI) NextWithEvents(ctx context.Context, messages []agent.Message, toolNames []string, emit func(agent.StreamEvent)) (agent.Response, error) {
	return p.nextWithEvents(ctx, messages, toolNames, emit)
}

func (p GoogleGenerativeAI) ListModels(ctx context.Context) ([]ModelInfo, error) {
	bearer, err := p.bearerToken(ctx)
	if err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(p.BaseURL + "/models")
	if err != nil {
		return nil, err
	}
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	var result []ModelInfo
	providerName := p.ProviderName
	if providerName == "" {
		providerName = "google"
	}
	pageToken := ""
	for page := 0; ; page++ {
		if page >= 100 {
			return nil, errors.New("google model catalog exceeded 100 pages")
		}
		query := endpoint.Query()
		if bearer == "" {
			query.Set("key", p.APIKey)
		}
		if pageToken == "" {
			query.Del("pageToken")
		} else {
			query.Set("pageToken", pageToken)
		}
		endpoint.RawQuery = query.Encode()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return nil, err
		}
		if bearer != "" {
			request.Header.Set("Authorization", "Bearer "+bearer)
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			message, _ := io.ReadAll(io.LimitReader(response.Body, 16<<10))
			response.Body.Close()
			return nil, fmt.Errorf("google models returned %s: %s", response.Status, strings.TrimSpace(string(message)))
		}
		var payload struct {
			Models []struct {
				Name                       string   `json:"name"`
				SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
			} `json:"models"`
			NextPageToken string `json:"nextPageToken"`
		}
		err = json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&payload)
		response.Body.Close()
		if err != nil {
			return nil, err
		}
		for _, model := range payload.Models {
			for _, method := range model.SupportedGenerationMethods {
				if method == "generateContent" {
					result = append(result, ModelInfo{Provider: providerName, ID: strings.TrimPrefix(model.Name, "models/")})
					break
				}
			}
		}
		pageToken = payload.NextPageToken
		if pageToken == "" {
			return result, nil
		}
	}
}

type googleContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []googlePart `json:"parts"`
}

type googlePart struct {
	Text             string            `json:"text,omitempty"`
	Thought          bool              `json:"thought,omitempty"`
	InlineData       *googleInlineData `json:"inlineData,omitempty"`
	FunctionCall     map[string]any    `json:"functionCall,omitempty"`
	FunctionResponse map[string]any    `json:"functionResponse,omitempty"`
}

type googleInlineData struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}

type googleChunk struct {
	ResponseID string `json:"responseId"`
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text         string         `json:"text"`
				Thought      bool           `json:"thought"`
				ThoughtSig   string         `json:"thoughtSignature"`
				FunctionCall map[string]any `json:"functionCall"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount        int `json:"promptTokenCount"`
		CandidatesTokenCount    int `json:"candidatesTokenCount"`
		ThoughtsTokenCount      int `json:"thoughtsTokenCount"`
		CachedContentTokenCount int `json:"cachedContentTokenCount"`
		TotalTokenCount         int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

func (p GoogleGenerativeAI) next(ctx context.Context, messages []agent.Message, tools []string, update func(string)) (agent.Response, error) {
	if p.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.Timeout)
		defer cancel()
	}
	if key, ok := agent.APIKeyFromContext(ctx); ok {
		p.APIKey = key
	}
	return p.nextWithEvents(ctx, messages, tools, func(event agent.StreamEvent) {
		if update != nil && event.Type == "text_delta" {
			update(event.Delta)
		}
	})
}

func (p GoogleGenerativeAI) nextWithEvents(ctx context.Context, messages []agent.Message, toolNames []string, emit func(agent.StreamEvent)) (agent.Response, error) {
	if p.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.Timeout)
		defer cancel()
	}
	if key, ok := agent.APIKeyFromContext(ctx); ok {
		p.APIKey = key
	}
	bearer, err := p.bearerToken(ctx)
	if err != nil {
		return agent.Response{}, err
	}
	if p.APIKey == "" && bearer == "" {
		return agent.Response{}, fmt.Errorf("no Google credential configured")
	}
	payload := map[string]any{"contents": googleContents(messages)}
	if system := googleSystemInstruction(messages); system != "" {
		payload["systemInstruction"] = map[string]any{"parts": []map[string]string{{"text": system}}}
	}
	if declarations := googleTools(toolNames); len(declarations) > 0 {
		payload["tools"] = []any{map[string]any{"functionDeclarations": declarations}}
	}
	if p.ThinkingLevel != "" && p.ThinkingLevel != "off" {
		budget := map[string]int{"minimal": 1024, "low": 2048, "medium": 4096, "high": 8192, "xhigh": 12288, "max": 24576}[p.ThinkingLevel]
		if budget == 0 {
			budget = 1024
		}
		payload["generationConfig"] = map[string]any{"thinkingConfig": map[string]any{"thinkingBudget": budget}}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return agent.Response{}, err
	}
	endpoint := p.BaseURL + "/models/" + url.PathEscape(p.Model) + ":streamGenerateContent?alt=sse"
	if bearer == "" {
		endpoint += "&key=" + url.QueryEscape(p.APIKey)
	}
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	var response *http.Response
	for attempt := 0; ; attempt++ {
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if requestErr != nil {
			return agent.Response{}, requestErr
		}
		request.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			request.Header.Set("Authorization", "Bearer "+bearer)
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
		retryable := response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		if !retryable || attempt >= p.MaxRetries {
			message, _ := io.ReadAll(io.LimitReader(response.Body, 16<<10))
			response.Body.Close()
			return agent.Response{}, fmt.Errorf("google generative ai returned %s: %s", response.Status, strings.TrimSpace(string(message)))
		}
		response.Body.Close()
		timer := time.NewTimer(200 * time.Millisecond * time.Duration(1<<min(attempt, 4)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return agent.Response{}, ctx.Err()
		case <-timer.C:
		}
	}
	defer response.Body.Close()
	providerName := p.ProviderName
	if providerName == "" {
		providerName = "google"
	}
	result := agent.Response{Provider: providerName, Model: p.Model}
	partial := agent.Message{Role: "assistant", Provider: result.Provider, Model: result.Model}
	if emit != nil {
		emit(agent.StreamEvent{Type: "start", Partial: partial})
	}
	textOpen, thinkingOpen := false, false
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var chunk googleChunk
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &chunk); err != nil {
			return agent.Response{}, fmt.Errorf("invalid google stream event: %w", err)
		}
		if result.ResponseID == "" {
			result.ResponseID = chunk.ResponseID
		}
		if len(chunk.Candidates) == 0 {
			continue
		}
		candidate := chunk.Candidates[0]
		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				if part.Thought && !thinkingOpen {
					thinkingOpen, textOpen = true, false
					if emit != nil {
						emit(agent.StreamEvent{Type: "thinking_start", ContentIndex: 0, Partial: partial})
					}
				} else if !part.Thought && !textOpen {
					textOpen, thinkingOpen = true, false
					if emit != nil {
						emit(agent.StreamEvent{Type: "text_start", ContentIndex: 0, Partial: partial})
					}
				}
				if part.Thought {
					partial.Thinking += part.Text
				} else {
					partial.Content += part.Text
					result.Text += part.Text
				}
				if emit != nil {
					typ := "text_delta"
					if part.Thought {
						typ = "thinking_delta"
					}
					emit(agent.StreamEvent{Type: typ, ContentIndex: 0, Delta: part.Text, Partial: partial})
				}
			}
			if part.FunctionCall != nil {
				name, _ := part.FunctionCall["name"].(string)
				args, _ := part.FunctionCall["args"].(map[string]any)
				call := agent.ToolCall{ID: name + "_google", Name: name, Args: args}
				result.ToolCalls = append(result.ToolCalls, call)
				partial.ToolCalls = append(partial.ToolCalls, call)
				if emit != nil {
					emit(agent.StreamEvent{Type: "toolcall_end", ContentIndex: len(partial.ToolCalls) - 1, ToolCall: &call, Partial: partial})
				}
			}
		}
		if candidate.FinishReason != "" {
			result.RawStopReason = candidate.FinishReason
			result.StopReason = googleStopReason(candidate.FinishReason)
		}
		usage := chunk.UsageMetadata
		result.Usage = agent.Usage{Input: usage.PromptTokenCount - usage.CachedContentTokenCount, Output: usage.CandidatesTokenCount + usage.ThoughtsTokenCount, Reasoning: usage.ThoughtsTokenCount, CacheRead: usage.CachedContentTokenCount, TotalTokens: usage.TotalTokenCount}
	}
	if err := scanner.Err(); err != nil {
		return agent.Response{}, err
	}
	if textOpen && emit != nil {
		emit(agent.StreamEvent{Type: "text_end", ContentIndex: 0, Partial: partial})
	}
	if thinkingOpen && emit != nil {
		emit(agent.StreamEvent{Type: "thinking_end", ContentIndex: 0, Partial: partial})
	}
	result.Thinking = partial.Thinking
	if result.StopReason == "" {
		result.StopReason = "stop"
	}
	result.ResponseModel = p.Model
	if emit != nil {
		emit(agent.StreamEvent{Type: "done", Partial: partial})
	}
	return result, nil
}

func (p GoogleGenerativeAI) bearerToken(ctx context.Context) (string, error) {
	if p.BearerToken != "" || p.BearerSource == nil {
		return p.BearerToken, nil
	}
	return p.BearerSource(ctx)
}

func googleContents(messages []agent.Message) []googleContent {
	result := make([]googleContent, 0, len(messages))
	for _, message := range messages {
		if message.Role == "system" {
			continue
		}
		role := "user"
		if message.Role == "assistant" {
			role = "model"
		}
		parts := []googlePart{}
		if message.Content != "" {
			parts = append(parts, googlePart{Text: message.Content})
		}
		for _, image := range message.Images {
			if mime, data, ok := googleDataImage(image); ok {
				parts = append(parts, googlePart{InlineData: &googleInlineData{MIMEType: mime, Data: data}})
			}
		}
		for _, call := range message.ToolCalls {
			parts = append(parts, googlePart{FunctionCall: map[string]any{"name": call.Name, "args": call.Args}})
		}
		if message.Role == "tool" && message.Content != "" {
			parts = []googlePart{{Text: message.Content}}
		}
		if len(parts) > 0 {
			result = append(result, googleContent{Role: role, Parts: parts})
		}
	}
	return result
}

func googleSystemInstruction(messages []agent.Message) string {
	var instructions []string
	for _, message := range messages {
		if message.Role == "system" && strings.TrimSpace(message.Content) != "" {
			instructions = append(instructions, message.Content)
		}
	}
	return strings.Join(instructions, "\n\n")
}

func googleTools(names []string) []map[string]any {
	result := make([]map[string]any, 0, len(names))
	for _, name := range names {
		result = append(result, map[string]any{"name": name, "description": name, "parameters": toolParameters(name)})
	}
	return result
}

func googleDataImage(value string) (string, string, bool) {
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
	return strings.TrimPrefix(strings.TrimSuffix(parts[0], ";base64"), "data:"), base64.StdEncoding.EncodeToString(data), true
}

func googleStopReason(reason string) string {
	switch reason {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "":
		return ""
	default:
		return "error"
	}
}

var _ agent.Provider = GoogleGenerativeAI{}
var _ agent.StreamingProviderWithEvents = GoogleGenerativeAI{}
var _ ModelLister = GoogleGenerativeAI{}
