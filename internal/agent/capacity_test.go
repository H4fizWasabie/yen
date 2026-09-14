package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

type capacityProvider struct {
	id int
}

func (p capacityProvider) Next(context.Context, []Message, []string) (Response, error) {
	return Response{Text: fmt.Sprintf("conversation-%d", p.id), StopReason: "stop"}, nil
}

func TestCapacity32ConversationsTenTurns(t *testing.T) {
	prompt := strings.Repeat("x", 4*1024)
	var wait sync.WaitGroup
	errors := make(chan error, 32)
	for id := 0; id < 32; id++ {
		wait.Add(1)
		go func(id int) {
			defer wait.Done()
			var history []Message
			for turn := 0; turn < 10; turn++ {
				result, err := RunFrom(context.Background(), capacityProvider{id: id}, nil, history, prompt)
				if err != nil {
					errors <- err
					return
				}
				if result.FinalText != fmt.Sprintf("conversation-%d", id) {
					errors <- fmt.Errorf("conversation %d returned %q", id, result.FinalText)
					return
				}
				history = result.Messages
			}
		}(id)
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
}
