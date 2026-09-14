package adapters

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/conversation"
	"github.com/H4fizWasabie/yen/internal/runtime"
)

type Service struct {
	Registry *conversation.Registry
	Runner   *runtime.Runner
}

func (s Service) Send(ctx context.Context, adapter, adapterKey, workspace, text string) (conversation.Turn, agent.Result, error) {
	if s.Registry == nil || s.Runner == nil {
		return conversation.Turn{}, agent.Result{}, errors.New("adapter service is not configured")
	}
	link, err := s.Registry.Resolve(adapter, adapterKey, workspace)
	if err != nil {
		return conversation.Turn{}, agent.Result{}, err
	}
	return s.SendLink(ctx, link, text)
}

func (s Service) SendLink(ctx context.Context, link conversation.Link, text string) (conversation.Turn, agent.Result, error) {
	turn, err := s.Runner.Submit(link, text)
	if err != nil {
		return conversation.Turn{}, agent.Result{}, err
	}
	_, result, err := s.Runner.RunSubmitted(ctx, turn)
	return turn, result, err
}

type Telegram struct {
	Service   Service
	Workspace string
}

func (a Telegram) HandleMessage(ctx context.Context, chatID, text string) (agent.Result, error) {
	_, result, err := a.Service.Send(ctx, "telegram", chatID, a.Workspace, text)
	return result, err
}

type Dashboard struct {
	Service   Service
	Workspace string
}

func (a Dashboard) NewSession() (conversation.Link, error) {
	key, err := randomKey("tab")
	if err != nil {
		return conversation.Link{}, err
	}
	return a.Service.Registry.Resolve("dashboard", key, a.Workspace)
}

func (a Dashboard) Send(ctx context.Context, conversationID, text string) (agent.Result, error) {
	return a.send(ctx, conversationID, text, nil)
}

func (a Dashboard) SendStream(ctx context.Context, conversationID, text string, onUpdate func(string)) (agent.Result, error) {
	return a.send(ctx, conversationID, text, onUpdate)
}

func (a Dashboard) send(ctx context.Context, conversationID, text string, onUpdate func(string)) (agent.Result, error) {
	link, ok := a.Service.Registry.FindConversationFor("dashboard", conversationID)
	if !ok {
		return agent.Result{}, errors.New("dashboard conversation not found")
	}
	turn, err := a.Service.Runner.Submit(link, text)
	if err != nil {
		return agent.Result{}, err
	}
	_, result, err := a.Service.Runner.RunSubmittedWithUpdates(ctx, turn, onUpdate)
	return result, err
}

func randomKey(prefix string) (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return prefix + ":" + hex.EncodeToString(raw[:]), nil
}
