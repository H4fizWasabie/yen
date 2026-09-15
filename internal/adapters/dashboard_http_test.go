package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/conversation"
	"github.com/H4fizWasabie/yen/internal/memory"
	"github.com/H4fizWasabie/yen/internal/runtime"
	"github.com/H4fizWasabie/yen/internal/session"
)

type dashboardAuthTransport struct{ base http.RoundTripper }

func (t dashboardAuthTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header.Set("Authorization", "Bearer secret")
	return t.base.RoundTrip(clone)
}

func TestDashboardHTTPRequiresAndAcceptsBearerToken(t *testing.T) {
	registry, err := conversation.OpenRegistry(filepath.Join(t.TempDir(), "links.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	dashboard := DashboardHTTP{AccessToken: "secret", Dashboard: Dashboard{Service: Service{Registry: registry}}}
	server := httptest.NewServer(dashboard)
	defer server.Close()
	response, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("<title>Yen dashboard</title>")) {
		t.Fatalf("dashboard shell status=%d body=%q err=%v", response.StatusCode, body, err)
	}

	response, err = http.Get(server.URL + "/api/sessions")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized || response.Header.Get("WWW-Authenticate") == "" {
		t.Fatalf("unauthorized status=%d headers=%v", response.StatusCode, response.Header)
	}

	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/login", bytes.NewBufferString(`{"token":"secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Set-Cookie") == "" {
		t.Fatalf("login status=%d headers=%v", response.StatusCode, response.Header)
	}

	request, err = http.NewRequest(http.MethodGet, server.URL+"/api/sessions", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer secret")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("authorized status=%d", response.StatusCode)
	}
}

func TestDashboardHTTPRejectsUnconfiguredAccessToken(t *testing.T) {
	server := httptest.NewServer(DashboardHTTP{})
	defer server.Close()
	response, err := http.Get(server.URL + "/api/sessions")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want %d", response.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestDashboardSessionsSortsMostRecentlyModifiedFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.jsonl")
	if err := os.WriteFile(path, []byte(
		`{"adapter":"telegram","adapterKey":"old","conversationId":"conv-old","createdAt":"2026-01-01T00:00:00Z"}`+"\n"+
			`{"adapter":"dashboard","adapterKey":"new","conversationId":"conv-new","createdAt":"2026-01-02T00:00:00Z"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := conversation.OpenRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	sessions := dashboardSessions(registry, nil)
	if len(sessions) != 2 || sessions[0]["id"] != "conv-new" || sessions[1]["id"] != "conv-old" {
		t.Fatalf("sessions=%#v", sessions)
	}
}

func TestDashboardHistoryIncludesMessageImages(t *testing.T) {
	history := dashboardHistory([]session.Message{
		{Role: "user", Content: "make an image", Images: []string{"data:image/png;base64,AA=="}},
		{Role: "assistant", Content: "working"},
		{Role: "toolResult", Content: "saved", Images: []string{"data:image/png;base64,BB=="}},
	})
	userSegments := history[0]["segments"].([]map[string]any)
	toolSegments := history[1]["segments"].([]map[string]any)
	if userSegments[1]["type"] != "image" || toolSegments[2]["type"] != "image" {
		t.Fatalf("history=%#v", history)
	}
}

func TestDashboardHistoryIncludesAssistantThinking(t *testing.T) {
	history := dashboardHistory([]session.Message{{Role: "assistant", Content: []session.ContentPart{
		{Type: "thinking", Text: "plan first"},
		{Type: "text", Text: "answer"},
	}}})
	segments := history[0]["segments"].([]map[string]any)
	if len(segments) != 2 || segments[0]["type"] != "thinking" || segments[0]["text"] != "plan first" {
		t.Fatalf("history=%#v", history)
	}
}

func TestDashboardShellRendersThinkingSegments(t *testing.T) {
	if !strings.Contains(dashboardHTML, "s.type==='thinking'") {
		t.Fatal("dashboard shell does not render thinking segments")
	}
}

func TestDashboardShellRendersParentAwareBranchTree(t *testing.T) {
	if !strings.Contains(dashboardHTML, "e.parentId") || !strings.Contains(dashboardHTML, "padding-left") || !strings.Contains(dashboardHTML, ".branch{display:block") {
		t.Fatal("dashboard shell does not render branch hierarchy")
	}
}

func TestDashboardHistoryIncludesBashExecution(t *testing.T) {
	history := dashboardHistory([]session.Message{{Role: "bashExecution", Command: "pwd", Output: "/work", ExcludeFromContext: true}})
	segments := history[0]["segments"].([]map[string]any)
	if history[0]["role"] != "bash" || segments[0]["type"] != "bash" || segments[0]["excludeFromContext"] != true {
		t.Fatalf("history=%#v", history)
	}
	if !strings.Contains(dashboardHTML, "s.type==='bash'") {
		t.Fatal("dashboard shell does not render bash segments")
	}
}

type httpProvider struct{}

func (httpProvider) Next(context.Context, []agent.Message, []string) (agent.Response, error) {
	return agent.Response{Text: "http response", StopReason: "stop"}, nil
}

func (httpProvider) NextWithUpdates(_ context.Context, _ []agent.Message, _ []string, update func(string)) (agent.Response, error) {
	update("http ")
	update("stream")
	return agent.Response{Text: "http stream", StopReason: "stop"}, nil
}

func TestDashboardHTTPHealthSubmitReadbackAndStop(t *testing.T) {
	dir := t.TempDir()
	registry, err := conversation.OpenRegistry(filepath.Join(dir, "links.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.New(queue, httpProvider{}, nil)
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	runner.Memory, err = memory.OpenEngine(filepath.Join(dir, "memory"))
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Memory.Close()
	handler := DashboardHTTP{AccessToken: "secret", Dashboard: Dashboard{Service: Service{Registry: registry, Runner: runner}, Workspace: dir}}
	server := httptest.NewServer(handler)
	defer server.Close()
	client := &http.Client{Transport: dashboardAuthTransport{base: http.DefaultTransport}}
	response, err := client.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status=%d", response.StatusCode)
	}
	response, err = client.Post(server.URL+"/api/sessions", "application/json", bytes.NewBufferString("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var link conversation.Link
	if err := json.NewDecoder(response.Body).Decode(&link); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated || link.ConversationID == "" {
		t.Fatalf("new session=%#v status=%d", link, response.StatusCode)
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/sessions/"+link.ConversationID+"/messages", bytes.NewBufferString(`{"message":"hello","replyContext":"quoted"}`))
	if err != nil {
		t.Fatal(err)
	}
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || result["text"] != "http stream" {
		t.Fatalf("message=%#v status=%d", result, response.StatusCode)
	}
	response, err = client.Get(server.URL + "/api/sessions/" + link.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var readback map[string]any
	if err := json.NewDecoder(response.Body).Decode(&readback); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || len(readback["messages"].([]any)) != 2 || len(readback["history"].([]any)) != 2 {
		t.Fatalf("readback=%#v status=%d", readback, response.StatusCode)
	}
	response, err = client.Get(server.URL + "/api/sessions")
	if err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Sessions []struct {
			MessageCount int    `json:"messageCount"`
			Title        string `json:"title"`
		} `json:"sessions"`
	}
	if err := json.NewDecoder(response.Body).Decode(&listed); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(listed.Sessions) != 1 || listed.Sessions[0].MessageCount != 2 || listed.Sessions[0].Title != "[Quoted message context]\nquoted\n[/Quoted message context]\n\nhello" {
		t.Fatalf("session list=%#v status=%d", listed, response.StatusCode)
	}
	if _, ok := readback["runtime"].(map[string]any); !ok {
		t.Fatalf("runtime readback=%#v", readback["runtime"])
	}
	if episodes, err := runner.Memory.Episodic.Recent(link.ConversationID, 8); err != nil || len(episodes) != 1 {
		t.Fatalf("dashboard episodes=%#v err=%v", episodes, err)
	}
	if _, err := registry.ResolveShared("telegram", "chat-1", dir, link.ConversationID); err != nil {
		t.Fatal(err)
	}
	response, err = client.Get(server.URL + "/api/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var sessions map[string]any
	if err := json.NewDecoder(response.Body).Decode(&sessions); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || len(sessions["sessions"].([]any)) != 1 {
		t.Fatalf("sessions=%#v status=%d", sessions, response.StatusCode)
	}
	if sessions["sessions"].([]any)[0].(map[string]any)["id"] != link.ConversationID {
		t.Fatalf("session view=%#v", sessions["sessions"])
	}
	request, err = http.NewRequest(http.MethodPost, server.URL+"/api/sessions/"+link.ConversationID+"/messages", bytes.NewBufferString(`{"message":"stream"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Accept", "text/event-stream")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	streamBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !bytes.Contains(streamBody, []byte("event: delta")) || !bytes.Contains(streamBody, []byte("event: done")) {
		t.Fatalf("stream=%q status=%d", streamBody, response.StatusCode)
	}
	if response.Header.Get("Content-Type") != "text/event-stream; charset=utf-8" || response.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("stream headers=%v", response.Header)
	}
	if !bytes.Contains(streamBody, []byte("event: done\ndata: {}\n\n")) {
		t.Fatalf("done event=%q", streamBody)
	}
}
