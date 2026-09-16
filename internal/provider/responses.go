package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/coder/websocket"
	"github.com/klauspost/compress/zstd"
)

// OpenAIResponses implements the native OpenAI Responses streaming protocol.
// Responses is intentionally not routed through OpenAICompletions: its input
// items and response event names are different, especially for tool calls.
type OpenAIResponses struct {
	BaseURL       string
	APIKey        string
	Model         string
	ProviderName  string
	Headers       map[string]string
	APIKeyHeader  string
	ThinkingLevel string
	Client        *http.Client
	MaxRetries    int
	// Transport selects the Codex transport. Empty means the normal HTTP path;
	// "websocket" enables the Codex WebSocket endpoint.
	Transport string
}

func NewOpenAIResponses(baseURL, apiKey, model string) OpenAIResponses {
	return OpenAIResponses{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, Model: model}
}

func (p OpenAIResponses) ListModels(ctx context.Context) ([]ModelInfo, error) {
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	endpoint, err := url.Parse(p.BaseURL + "/models")
	if err != nil {
		return nil, err
	}
	var models []ModelInfo
	after := ""
	for page := 0; page < 100; page++ {
		query := endpoint.Query()
		if after == "" {
			query.Del("after")
		} else {
			query.Set("after", after)
		}
		endpoint.RawQuery = query.Encode()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return nil, err
		}
		if p.APIKeyHeader != "" {
			request.Header.Set(p.APIKeyHeader, p.APIKey)
		} else if p.APIKey != "" {
			request.Header.Set("Authorization", "Bearer "+p.APIKey)
		}
		for name, value := range p.Headers {
			request.Header.Set(name, value)
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			response.Body.Close()
			return nil, fmt.Errorf("model catalog returned %s", response.Status)
		}
		var payload struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		decodeErr := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&payload)
		response.Body.Close()
		if decodeErr != nil {
			return nil, decodeErr
		}
		for _, model := range payload.Data {
			if strings.TrimSpace(model.ID) != "" {
				models = append(models, ModelInfo{Provider: p.name(), ID: model.ID})
			}
		}
		if !payload.HasMore || strings.TrimSpace(payload.LastID) == "" || payload.LastID == after {
			return models, nil
		}
		after = payload.LastID
	}
	return nil, errors.New("model catalog pagination exceeded 100 pages")
}

func (p OpenAIResponses) Next(ctx context.Context, messages []agent.Message, toolNames []string) (agent.Response, error) {
	return p.next(ctx, messages, toolNames, nil)
}

func (p OpenAIResponses) NextWithUpdates(ctx context.Context, messages []agent.Message, toolNames []string, update func(string)) (agent.Response, error) {
	return p.next(ctx, messages, toolNames, func(event agent.StreamEvent) {
		if update != nil && event.Type == "text_delta" {
			update(event.Delta)
		}
	})
}

func (p OpenAIResponses) NextWithEvents(ctx context.Context, messages []agent.Message, toolNames []string, emit func(agent.StreamEvent)) (agent.Response, error) {
	return p.next(ctx, messages, toolNames, emit)
}

func (p OpenAIResponses) next(ctx context.Context, messages []agent.Message, toolNames []string, emit func(agent.StreamEvent)) (agent.Response, error) {
	if p.APIKey == "" && strings.TrimSpace(p.Headers["cf-aig-authorization"]) == "" {
		return agent.Response{}, fmt.Errorf("no API key for provider: %s", p.name())
	}
	input := messages
	payload := map[string]any{
		"model":  p.Model,
		"input":  responsesInput(input),
		"stream": true,
		"store":  false,
	}
	if p.ProviderName == "openai-codex" {
		if responseID, delta := codexContinuation(messages); responseID != "" {
			payload["previous_response_id"] = responseID
			payload["input"] = responsesInput(delta)
		}
	}
	if tools := responsesTools(toolNames); len(tools) > 0 {
		payload["tools"] = tools
	}
	if p.ThinkingLevel != "" && p.ThinkingLevel != "off" {
		payload["reasoning"] = map[string]string{"effort": p.ThinkingLevel, "summary": "auto"}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return agent.Response{}, err
	}
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	endpoint := p.BaseURL + "/responses"
	requestBody := body
	contentEncoding := ""
	if p.ProviderName == "openai-codex" {
		if compressed, compressErr := compressCodexBody(body); compressErr == nil {
			requestBody = compressed
			contentEncoding = "zstd"
		}
	}
	var response *http.Response
	if p.ProviderName == "openai-codex" && p.Transport == "websocket" {
		response, err = openCodexWebSocketResponse(ctx, p, body)
		if err == nil {
			defer response.Body.Close()
		} else {
			// A failed handshake is safe to retry over the documented SSE path.
			response = nil
		}
	}
	for attempt := 0; ; attempt++ {
		if response != nil {
			break
		}
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(requestBody))
		if requestErr != nil {
			return agent.Response{}, requestErr
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "text/event-stream")
		if contentEncoding != "" {
			request.Header.Set("Content-Encoding", contentEncoding)
		}
		if p.APIKeyHeader != "" {
			request.Header.Set(p.APIKeyHeader, p.APIKey)
		} else if p.APIKey != "" {
			request.Header.Set("Authorization", "Bearer "+p.APIKey)
		}
		for name, value := range p.Headers {
			request.Header.Set(name, value)
		}
		if p.ProviderName == "github-copilot" {
			applyCopilotHeaders(request.Header, messages)
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
			return agent.Response{}, fmt.Errorf("openai responses returned %s: %s", response.Status, strings.TrimSpace(string(message)))
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
	responseBody, closeResponseBody, err := decodeCodexResponseBody(response)
	if err != nil {
		return agent.Response{}, err
	}
	defer closeResponseBody()
	provider := p.name()
	result := agent.Response{Provider: provider, Model: p.Model}
	partial := agent.Message{Role: "assistant", Provider: provider, Model: p.Model}
	toolArgs := make(map[string]string)
	toolCalls := make(map[string]agent.ToolCall)
	if emit != nil {
		emit(agent.StreamEvent{Type: "start", Partial: partial})
	}
	scanner := bufio.NewScanner(responseBody)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			continue
		}
		var event struct {
			Type     string `json:"type"`
			Delta    string `json:"delta"`
			Response struct {
				ID     string `json:"id"`
				Model  string `json:"model"`
				Status string `json:"status"`
				Error  *struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
				Output []struct {
					Type             string `json:"type"`
					ID               string `json:"id"`
					EncryptedContent string `json:"encrypted_content"`
				} `json:"output"`
				Usage *struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
					TotalTokens  int `json:"total_tokens"`
					InputDetails *struct {
						CachedTokens int `json:"cached_tokens"`
					} `json:"input_tokens_details"`
				} `json:"usage"`
				IncompleteDetails *struct {
					Reason string `json:"reason"`
				} `json:"incomplete_details"`
			} `json:"response"`
			Item struct {
				Type      string `json:"type"`
				ID        string `json:"id"`
				CallID    string `json:"call_id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
				Summary   []struct {
					Text string `json:"text"`
				} `json:"summary"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
				EncryptedContent string `json:"encrypted_content"`
			} `json:"item"`
			OutputIndex int    `json:"output_index"`
			CallID      string `json:"call_id"`
			Name        string `json:"name"`
			Arguments   string `json:"arguments"`
			Code        string `json:"code"`
			Message     string `json:"message"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return agent.Response{}, fmt.Errorf("invalid OpenAI Responses event: %w", err)
		}
		switch event.Type {
		case "response.created":
			result.ResponseID = event.Response.ID
		case "response.output_text.delta":
			partial.Content += event.Delta
			result.Text += event.Delta
			if emit != nil {
				emit(agent.StreamEvent{Type: "text_delta", ContentIndex: event.OutputIndex, Delta: event.Delta, Partial: partial})
			}
		case "response.reasoning_summary_text.delta", "response.reasoning_summary_part.added":
			partial.Thinking += event.Delta
			if emit != nil {
				emit(agent.StreamEvent{Type: "thinking_delta", ContentIndex: event.OutputIndex, Delta: event.Delta, Partial: partial})
			}
		case "response.output_item.added":
			if event.Item.Type == "function_call" {
				callID := event.Item.CallID
				if callID == "" {
					callID = event.Item.ID
				}
				call := agent.ToolCall{ID: callID, Name: event.Item.Name, Args: map[string]any{}}
				toolCalls[callID] = call
				if emit != nil {
					emit(agent.StreamEvent{Type: "toolcall_start", ContentIndex: event.OutputIndex, ToolCall: &call, Partial: partial})
				}
			}
		case "response.function_call_arguments.delta":
			callID := event.CallID
			toolArgs[callID] += event.Delta
			if emit != nil {
				emit(agent.StreamEvent{Type: "toolcall_delta", ContentIndex: event.OutputIndex, Delta: event.Delta, Partial: partial})
			}
		case "response.function_call_arguments.done":
			callID := event.CallID
			toolArgs[callID] = event.Arguments
			call := toolCalls[callID]
			if call.ID == "" {
				call = agent.ToolCall{ID: callID, Name: event.Name, Args: map[string]any{}}
			}
			_ = json.Unmarshal([]byte(event.Arguments), &call.Args)
			toolCalls[callID] = call
		case "response.output_item.done":
			if event.Item.Type == "reasoning" {
				thinking := make([]string, 0, len(event.Item.Summary)+len(event.Item.Content))
				for _, part := range event.Item.Summary {
					thinking = append(thinking, part.Text)
				}
				if len(thinking) == 0 {
					for _, part := range event.Item.Content {
						thinking = append(thinking, part.Text)
					}
				}
				partial.Thinking = strings.Join(thinking, "\n\n")
				encoded, _ := json.Marshal(event.Item)
				partial.ThinkingSignature = string(encoded)
			} else if event.Item.Type == "function_call" {
				call := toolCalls[event.Item.CallID]
				if call.ID == "" {
					call = toolCalls[event.Item.ID]
				}
				if event.Item.Arguments != "" {
					_ = json.Unmarshal([]byte(event.Item.Arguments), &call.Args)
				}
				result.ToolCalls = append(result.ToolCalls, call)
				partial.ToolCalls = append(partial.ToolCalls, call)
				if emit != nil {
					emit(agent.StreamEvent{Type: "toolcall_end", ContentIndex: event.OutputIndex, ToolCall: &call, Partial: partial})
				}
			}
		case "response.completed", "response.incomplete":
			if len(event.Response.Output) > 0 && partial.ThinkingSignature != "" {
				var signature map[string]any
				if json.Unmarshal([]byte(partial.ThinkingSignature), &signature) == nil && signature["encrypted_content"] == "" {
					id, _ := signature["id"].(string)
					for _, item := range event.Response.Output {
						if item.Type == "reasoning" && item.ID == id && item.EncryptedContent != "" {
							signature["encrypted_content"] = item.EncryptedContent
							encoded, _ := json.Marshal(signature)
							partial.ThinkingSignature = string(encoded)
							break
						}
					}
				}
			}
			result.ResponseID = event.Response.ID
			if event.Response.Model != "" {
				result.ResponseModel = event.Response.Model
			}
			result.StopReason = "stop"
			if event.Type == "response.incomplete" && event.Response.IncompleteDetails != nil && event.Response.IncompleteDetails.Reason == "max_output_tokens" {
				result.StopReason = "length"
			}
			if event.Response.Usage != nil {
				cached := 0
				if event.Response.Usage.InputDetails != nil {
					cached = event.Response.Usage.InputDetails.CachedTokens
				}
				result.Usage = agent.Usage{Input: event.Response.Usage.InputTokens - cached, Output: event.Response.Usage.OutputTokens, CacheRead: cached, TotalTokens: event.Response.Usage.TotalTokens}
			}
		case "response.failed":
			if event.Response.Error != nil {
				return agent.Response{}, fmt.Errorf("%s: %s", event.Response.Error.Code, event.Response.Error.Message)
			}
			return agent.Response{}, errors.New("Unknown error (no error details in response)")
		case "error":
			result.StopReason = "error"
			result.ErrorMessage = event.Message
			if result.ErrorMessage == "" {
				result.ErrorMessage = event.Code
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return agent.Response{}, err
	}
	if result.StopReason == "" {
		result.StopReason = "stop"
	}
	result.Thinking = partial.Thinking
	result.ThinkingSignature = partial.ThinkingSignature
	if result.ResponseModel == "" {
		result.ResponseModel = p.Model
	}
	if result.ToolCalls != nil && result.StopReason == "stop" {
		result.StopReason = "toolUse"
	}
	if emit != nil {
		emit(agent.StreamEvent{Type: "done", Partial: partial})
	}
	return result, nil
}

func codexContinuation(messages []agent.Message) (string, []agent.Message) {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" && strings.TrimSpace(messages[i].ResponseID) != "" && i < len(messages)-1 {
			return messages[i].ResponseID, messages[i+1:]
		}
	}
	return "", nil
}

func compressCodexBody(body []byte) ([]byte, error) {
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(3)))
	if err != nil {
		return nil, err
	}
	defer encoder.Close()
	return encoder.EncodeAll(body, nil), nil
}

func decodeCodexResponseBody(response *http.Response) (io.Reader, func(), error) {
	if !strings.EqualFold(response.Header.Get("Content-Encoding"), "zstd") {
		return response.Body, func() {}, nil
	}
	decoder, err := zstd.NewReader(response.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("decode Codex zstd response: %w", err)
	}
	return decoder, func() { decoder.Close() }, nil
}

type codexWebSocketCacheEntry struct {
	mu        sync.Mutex
	conn      *websocket.Conn
	idleTimer *time.Timer
}

type codexWebSocketBody struct {
	*io.PipeReader
	release func()
	once    sync.Once
}

func (b *codexWebSocketBody) Close() error {
	err := b.PipeReader.Close()
	b.once.Do(b.release)
	return err
}

var codexWebSocketCache sync.Map

func openCodexWebSocketResponse(ctx context.Context, p OpenAIResponses, body []byte) (*http.Response, error) {
	cacheKey := p.BaseURL + "\x00" + p.Headers["chatgpt-account-id"]
	value, _ := codexWebSocketCache.LoadOrStore(cacheKey, &codexWebSocketCacheEntry{})
	entry := value.(*codexWebSocketCacheEntry)
	entry.mu.Lock()
	if entry.idleTimer != nil {
		entry.idleTimer.Stop()
		entry.idleTimer = nil
	}
	connection := entry.conn
	if connection == nil {
		var err error
		connection, err = dialCodexWebSocket(ctx, p)
		if err != nil {
			entry.mu.Unlock()
			return nil, err
		}
		entry.conn = connection
	}

	request := map[string]any{"type": "response.create"}
	var fields map[string]any
	if err := json.Unmarshal(body, &fields); err != nil {
		entry.mu.Unlock()
		return nil, err
	}
	for key, value := range fields {
		request[key] = value
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		entry.mu.Unlock()
		return nil, err
	}
	if err := connection.Write(ctx, websocket.MessageText, encoded); err != nil {
		entry.conn = nil
		_ = connection.Close(websocket.StatusInternalError, "request failed")
		entry.mu.Unlock()
		return nil, err
	}

	reader, writer := io.Pipe()
	go func() {
		for {
			typ, message, readErr := connection.Read(ctx)
			if readErr != nil {
				entry.conn = nil
				_ = writer.CloseWithError(readErr)
				return
			}
			if typ != websocket.MessageText && typ != websocket.MessageBinary {
				continue
			}
			var event struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(message, &event) != nil {
				entry.conn = nil
				_ = writer.CloseWithError(errors.New("invalid Codex WebSocket event"))
				return
			}
			if _, writeErr := fmt.Fprintf(writer, "data: %s\n\n", message); writeErr != nil {
				return
			}
			if event.Type == "response.completed" || event.Type == "response.done" || event.Type == "response.incomplete" || event.Type == "response.failed" {
				_ = writer.Close()
				return
			}
		}
	}()

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body: &codexWebSocketBody{PipeReader: reader, release: func() {
			entry.idleTimer = time.AfterFunc(5*time.Minute, func() {
				entry.mu.Lock()
				if entry.conn == connection {
					entry.conn = nil
					_ = connection.Close(websocket.StatusNormalClosure, "idle_timeout")
				}
				entry.mu.Unlock()
			})
			entry.mu.Unlock()
		}},
	}, nil
}

func dialCodexWebSocket(ctx context.Context, p OpenAIResponses) (*websocket.Conn, error) {
	endpoint, err := url.Parse(p.BaseURL + "/responses")
	if err != nil {
		return nil, err
	}
	switch endpoint.Scheme {
	case "https":
		endpoint.Scheme = "wss"
	case "http":
		endpoint.Scheme = "ws"
	default:
		return nil, fmt.Errorf("unsupported Codex WebSocket URL scheme %q", endpoint.Scheme)
	}
	headers := make(http.Header)
	for name, value := range p.Headers {
		headers.Set(name, value)
	}
	headers.Del("Accept")
	headers.Del("Content-Type")
	headers.Del("OpenAI-Beta")
	headers.Set("OpenAI-Beta", "responses_websockets=2026-02-06")
	headers.Set("Authorization", "Bearer "+p.APIKey)
	requestID := headers.Get("x-client-request-id")
	if requestID == "" {
		requestID = fmt.Sprintf("yen-%d", time.Now().UnixNano())
	}
	headers.Set("x-client-request-id", requestID)
	headers.Set("session-id", requestID)
	connection, _, err := websocket.Dial(ctx, endpoint.String(), &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		return nil, err
	}
	return connection, nil
}

func (p OpenAIResponses) name() string {
	if p.ProviderName != "" {
		return p.ProviderName
	}
	return "openai-responses"
}

func responsesInput(messages []agent.Message) []map[string]any {
	result := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		if message.Role == "system" {
			result = append(result, map[string]any{"role": "system", "content": message.Content})
			continue
		}
		if message.Role == "tool" {
			result = append(result, map[string]any{"type": "function_call_output", "call_id": message.ToolCallID, "output": message.Content})
			continue
		}
		role := message.Role
		if role != "assistant" {
			role = "user"
		}
		content := []map[string]any{}
		if message.Content != "" {
			content = append(content, map[string]any{"type": "input_text", "text": message.Content})
		}
		for _, image := range message.Images {
			content = append(content, map[string]any{"type": "input_image", "image_url": image, "detail": "auto"})
		}
		result = append(result, map[string]any{"role": role, "content": content})
		for _, call := range message.ToolCalls {
			args, _ := json.Marshal(call.Args)
			result = append(result, map[string]any{"type": "function_call", "call_id": call.ID, "name": call.Name, "arguments": string(args)})
		}
	}
	return result
}

func responsesTools(names []string) []map[string]any {
	result := make([]map[string]any, 0, len(names))
	for _, name := range names {
		result = append(result, map[string]any{"type": "function", "name": name, "description": name, "parameters": toolParameters(name), "strict": false})
	}
	return result
}

var _ agent.Provider = OpenAIResponses{}
var _ agent.StreamingProviderWithEvents = OpenAIResponses{}
