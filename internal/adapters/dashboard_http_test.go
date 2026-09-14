package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/conversation"
	"github.com/H4fizWasabie/yen/internal/memory"
	"github.com/H4fizWasabie/yen/internal/runtime"
)

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
	handler := DashboardHTTP{Dashboard: Dashboard{Service: Service{Registry: registry, Runner: runner}, Workspace: dir}}
	server := httptest.NewServer(handler)
	defer server.Close()
	response, err := http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status=%d", response.StatusCode)
	}
	response, err = http.Post(server.URL+"/api/sessions", "application/json", bytes.NewBufferString("{}"))
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
	response, err = http.Post(server.URL+"/api/sessions/"+link.ConversationID+"/messages", "application/json", bytes.NewBufferString(`{"message":"hello"}`))
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
	response, err = http.Get(server.URL + "/api/sessions/" + link.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var readback map[string]any
	if err := json.NewDecoder(response.Body).Decode(&readback); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || len(readback["messages"].([]any)) != 2 {
		t.Fatalf("readback=%#v status=%d", readback, response.StatusCode)
	}
	if episodes, err := runner.Memory.Episodic.Recent(link.ConversationID, 8); err != nil || len(episodes) != 1 {
		t.Fatalf("dashboard episodes=%#v err=%v", episodes, err)
	}
	response, err = http.Get(server.URL + "/api/sessions")
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
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/sessions/"+link.ConversationID+"/messages", bytes.NewBufferString(`{"message":"stream"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Accept", "text/event-stream")
	response, err = http.DefaultClient.Do(request)
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
}
