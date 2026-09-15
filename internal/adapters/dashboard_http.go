package adapters

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
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
			_, runErr := h.Dashboard.SendStream(r.Context(), id, input.Message, func(text string) {
				_, _ = io.WriteString(w, "event: delta\ndata: "+mustJSON(map[string]string{"text": text})+"\n\n")
				if flush != nil {
					flush.Flush()
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
		writeJSON(w, http.StatusOK, map[string]any{"conversationId": id, "messages": session.Messages()})
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func mustJSON(value any) string { data, _ := json.Marshal(value); return string(data) }

var _ http.Handler = DashboardHTTP{}
