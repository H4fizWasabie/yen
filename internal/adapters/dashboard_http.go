package adapters

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/session"
)

type DashboardHTTP struct {
	Dashboard   Dashboard
	AccessToken string
}

func (h DashboardHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		if r.URL.Path == "/api/login" {
			h.login(w, r)
			return
		}
		if !h.authorized(w, r) {
			return
		}
	}
	if r.URL.Path == "/api/sessions" && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{"sessions": h.Dashboard.Service.Registry.List("dashboard")})
		return
	}
	if r.URL.Path == "/api/sessions" && r.Method == http.MethodPost {
		h.newSession(w)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/sessions/") {
		h.session(w, r)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}

func (h DashboardHTTP) authorized(w http.ResponseWriter, r *http.Request) bool {
	if h.AccessToken == "" {
		return true
	}
	candidate := bearerToken(r.Header.Get("Authorization"))
	if candidate == "" {
		candidate = cookieToken(r.Header.Get("Cookie"))
	}
	if secureToken(candidate, h.AccessToken) {
		return true
	}
	w.Header().Set("WWW-Authenticate", `Bearer realm="Theoses dashboard"`)
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Dashboard authentication required"})
	return false
}

func (h DashboardHTTP) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}
	if h.AccessToken == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Dashboard access token is not configured"})
		return
	}
	var input struct {
		Token string `json:"token"`
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil || json.Unmarshal(body, &input) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid login body"})
		return
	}
	if !secureToken(input.Token, h.AccessToken) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="Theoses dashboard"`)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid dashboard token"})
		return
	}
	w.Header().Set("Set-Cookie", "theoses_dashboard_token="+url.QueryEscape(h.AccessToken)+"; Path=/; Max-Age=31536000; HttpOnly; SameSite=Strict")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func bearerToken(header string) string {
	parts := strings.Fields(header)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}
	return ""
}

func cookieToken(header string) string {
	for _, item := range strings.Split(header, ";") {
		parts := strings.SplitN(strings.TrimSpace(item), "=", 2)
		if len(parts) == 2 && parts[0] == "theoses_dashboard_token" {
			value, err := url.QueryUnescape(parts[1])
			if err == nil {
				return value
			}
		}
	}
	return ""
}

func secureToken(candidate, expected string) bool {
	if len(candidate) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(expected)) == 1
}

func (h DashboardHTTP) newSession(w http.ResponseWriter) {
	link, err := h.Dashboard.NewSession()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, link)
}

func (h DashboardHTTP) session(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/sessions/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "conversation ID is required"})
		return
	}
	id, err := url.PathUnescape(parts[0])
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid conversation ID"})
		return
	}
	switch {
	case len(parts) == 2 && parts[1] == "messages" && r.Method == http.MethodPost:
		var input struct {
			Message string `json:"message"`
		}
		body, readErr := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if readErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": readErr.Error()})
			return
		}
		if json.Unmarshal(body, &input) != nil || strings.TrimSpace(input.Message) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message is required"})
			return
		}
		if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache, no-store")
			w.Header().Set("Connection", "keep-alive")
			w.Header().Set("X-Accel-Buffering", "no")
			w.WriteHeader(http.StatusOK)
			flush, _ := w.(http.Flusher)
			emit := func(event string, value any) {
				_, _ = io.WriteString(w, "event: "+event+"\ndata: "+mustJSON(value)+"\n\n")
				if flush != nil {
					flush.Flush()
				}
			}
			_, runErr := h.Dashboard.SendStreamWithEvents(r.Context(), id, input.Message, func(text string) {
				emit("delta", map[string]string{"text": text})
			}, func(event agent.Event) {
				switch event.Type {
				case "tool_call":
					emit("tool_call", map[string]any{"id": event.ID, "name": event.Name, "args": event.Args})
				case "tool_result":
					emit("tool_result", map[string]any{"id": event.ID, "name": event.Name, "result": event.Result, "isError": event.IsError})
				case "usage":
					emit("usage", map[string]any{"input": event.Usage.Input, "output": event.Usage.Output, "totalTokens": event.Usage.TotalTokens, "cost": 0})
				}
			})
			if runErr != nil {
				_, _ = io.WriteString(w, "event: error\ndata: "+mustJSON(map[string]string{"message": runErr.Error()})+"\n\n")
				return
			}
			_, _ = io.WriteString(w, "event: done\ndata: {}\n\n")
			if flush != nil {
				flush.Flush()
			}
			return
		}
		result, runErr := h.Dashboard.Send(r.Context(), id, input.Message)
		if runErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": runErr.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"text": result.FinalText})
	case len(parts) == 2 && parts[1] == "stop" && r.Method == http.MethodPost:
		turn, ok := h.Dashboard.Service.Runner.Active(id)
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "active": false})
			return
		}
		if err := h.Dashboard.Service.Runner.Cancel(turn.ID); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "active": true, "turnId": turn.ID})
	case len(parts) == 1 && r.Method == http.MethodGet:
		link, ok := h.Dashboard.Service.Registry.FindConversationFor("dashboard", id)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		}
		session, openErr := h.Dashboard.Service.Runner.OpenSession(link)
		if openErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": openErr.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"session": map[string]any{"id": id, "channel": "dashboard", "title": id, "messageCount": len(session.Messages())},
			"history": dashboardHistory(session.Messages()),
			"runtime": dashboardRuntime(session.Messages()),
			// Keep the early Go pilot response available to non-UI clients.
			"conversationId": id,
			"messages":       session.Messages(),
		})
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func dashboardHistory(messages []session.Message) []map[string]any {
	history := make([]map[string]any, 0)
	for _, message := range messages {
		if message.Role == "user" {
			history = append(history, map[string]any{"role": "user", "segments": []map[string]any{{"type": "text", "text": contentText(message.Content)}}})
			continue
		}
		if message.Role == "assistant" {
			turn := map[string]any{"role": "assistant", "segments": assistantSegments(message.Content), "usage": usageSummary(message.Usage)}
			history = append(history, turn)
			continue
		}
		if message.Role == "toolResult" {
			segment := map[string]any{"type": "tool_result", "id": message.ToolCallID, "name": "", "result": contentText(message.Content), "isError": strings.HasPrefix(contentText(message.Content), "Tool error:")}
			if len(history) > 0 && history[len(history)-1]["role"] == "assistant" {
				history[len(history)-1]["segments"] = append(history[len(history)-1]["segments"].([]map[string]any), segment)
			}
		}
	}
	return history
}

func assistantSegments(content any) []map[string]any {
	parts, ok := content.([]session.ContentPart)
	if !ok {
		return []map[string]any{{"type": "text", "text": contentText(content)}}
	}
	segments := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		if part.Type == "toolCall" {
			args, _ := part.Arguments.(map[string]any)
			segments = append(segments, map[string]any{"type": "tool_call", "id": part.ID, "name": part.Name, "args": args})
		} else if part.Type == "text" {
			segments = append(segments, map[string]any{"type": "text", "text": part.Text})
		}
	}
	return segments
}

func contentText(content any) string {
	if text, ok := content.(string); ok {
		return text
	}
	parts, ok := content.([]session.ContentPart)
	if !ok {
		return fmt.Sprint(content)
	}
	var result strings.Builder
	for _, part := range parts {
		if part.Type == "text" {
			result.WriteString(part.Text)
		}
	}
	return result.String()
}

func usageSummary(usage *session.Usage) map[string]any {
	if usage == nil {
		return nil
	}
	return map[string]any{"input": usage.Input, "output": usage.Output, "totalTokens": usage.TotalTokens, "cost": 0}
}

func dashboardRuntime(messages []session.Message) map[string]any {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			return map[string]any{"provider": nullableString(messages[i].Provider), "modelId": nullableString(messages[i].Model), "thinkingLevel": "", "lastUsage": usageSummary(messages[i].Usage)}
		}
	}
	return map[string]any{"provider": nil, "modelId": nil, "thinkingLevel": "", "lastUsage": nil}
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func mustJSON(value any) string { data, _ := json.Marshal(value); return string(data) }

var _ http.Handler = DashboardHTTP{}
