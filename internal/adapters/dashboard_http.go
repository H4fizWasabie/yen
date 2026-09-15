package adapters

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type DashboardHTTP struct {
	Dashboard Dashboard
}

func (h DashboardHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
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
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			flush, _ := w.(http.Flusher)
			result, runErr := h.Dashboard.SendStream(r.Context(), id, input.Message, func(text string) {
				_, _ = io.WriteString(w, "event: delta\ndata: "+mustJSON(map[string]string{"text": text})+"\n\n")
				if flush != nil {
					flush.Flush()
				}
			})
			if runErr != nil {
				_, _ = io.WriteString(w, "event: error\ndata: "+mustJSON(map[string]string{"error": runErr.Error()})+"\n\n")
				return
			}
			_, _ = io.WriteString(w, "event: done\ndata: "+mustJSON(map[string]string{"text": result.FinalText})+"\n\n")
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
