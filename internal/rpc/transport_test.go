package rpc

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/conversation"
	"github.com/H4fizWasabie/yen/internal/runtime"
)

func TestUnixTransportRoundTripsRPCResponse(t *testing.T) {
	dir := t.TempDir()
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.New(queue, rpcProvider{}, nil)
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	server := &Server{Runner: runner, Link: conversation.Link{Adapter: "rpc", AdapterKey: "test", ConversationID: "conv-rpc", WorkspaceID: dir}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	socket := filepath.Join(dir, "rpc.sock")
	result := make(chan error, 1)
	go func() { result <- ServeUnix(ctx, socket, server) }()
	var client *Client
	for i := 0; i < 50; i++ {
		client, err = DialUnix(socket)
		if err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	response, err := client.Call(map[string]any{"id": "state", "type": "get_state"})
	if err != nil || response["success"] != true {
		t.Fatalf("response=%#v err=%v", response, err)
	}
	cancel()
	select {
	case <-result:
	case <-time.After(time.Second):
		t.Fatal("unix server did not stop")
	}
}

var _ agent.Provider = rpcProvider{}
