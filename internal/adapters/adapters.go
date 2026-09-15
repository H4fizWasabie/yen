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
	Registry                *conversation.Registry
	Runner                  *runtime.Runner
	CanonicalConversationID string
}

func (s Service) Send(ctx context.Context, adapter, adapterKey, workspace, text string) (conversation.Turn, agent.Result, error) {
	if s.Registry == nil || s.Runner == nil {
		return conversation.Turn{}, agent.Result{}, errors.New("adapter service is not configured")
	}
	link, err := s.resolve(adapter, adapterKey, workspace)
	if err != nil {
		return conversation.Turn{}, agent.Result{}, err
	}
	return s.SendLink(ctx, link, text)
}

func (s Service) resolve(adapter, adapterKey, workspace string) (conversation.Link, error) {
	if s.CanonicalConversationID != "" {
		return s.Registry.ResolveShared(adapter, adapterKey, workspace, s.CanonicalConversationID)
	}
	return s.Registry.Resolve(adapter, adapterKey, workspace)
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
	return a.HandleMessageWithReply(ctx, chatID, text, "")
}

func (a Telegram) HandleMessageWithReply(ctx context.Context, chatID, text, replyContext string) (agent.Result, error) {
	return a.handleMessageWithReply(ctx, chatID, text, replyContext, nil, nil)
}

func (a Telegram) HandleMessageWithReplyEvents(ctx context.Context, chatID, text, replyContext string, onEvent agent.EventFunc) (agent.Result, error) {
	return a.handleMessageWithReply(ctx, chatID, text, replyContext, nil, onEvent)
}

func (a Telegram) HandleMessageWithImagesEvents(ctx context.Context, chatID, text, replyContext, image string, onEvent agent.EventFunc) (agent.Result, error) {
	var images []string
	if image != "" {
		images = []string{image}
	}
	return a.handleMessageWithReply(ctx, chatID, text, replyContext, images, onEvent)
}

func (a Telegram) handleMessageWithReply(ctx context.Context, chatID, text, replyContext string, images []string, onEvent agent.EventFunc) (agent.Result, error) {
	text = addReplyContext(text, replyContext)
	link, err := a.Service.resolve("telegram", chatID, a.Workspace)
	if err != nil {
		return agent.Result{}, err
	}
	turn, err := a.Service.Runner.Submit(link, text)
	if err != nil {
		return agent.Result{}, err
	}
	_, result, err := a.Service.Runner.RunSubmittedWithEventsAndImages(ctx, turn, images, nil, onEvent)
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
	if a.Service.CanonicalConversationID != "" {
		return a.Service.Registry.ResolveShared("dashboard", key, a.Workspace, a.Service.CanonicalConversationID)
	}
	return a.Service.Registry.Resolve("dashboard", key, a.Workspace)
}

func (a Dashboard) Send(ctx context.Context, conversationID, text string) (agent.Result, error) {
	return a.send(ctx, conversationID, text, nil, nil)
}

func (a Dashboard) SendWithReply(ctx context.Context, conversationID, text, replyContext string) (agent.Result, error) {
	return a.sendWithReply(ctx, conversationID, text, replyContext, nil, nil)
}

func (a Dashboard) SendStream(ctx context.Context, conversationID, text string, onUpdate func(string)) (agent.Result, error) {
	return a.send(ctx, conversationID, text, onUpdate, nil)
}

func (a Dashboard) SendStreamWithEvents(ctx context.Context, conversationID, text string, onUpdate func(string), onEvent agent.EventFunc) (agent.Result, error) {
	return a.send(ctx, conversationID, text, onUpdate, onEvent)
}

func (a Dashboard) SendStreamWithEventsAndReply(ctx context.Context, conversationID, text, replyContext string, onUpdate func(string), onEvent agent.EventFunc) (agent.Result, error) {
	return a.sendWithReply(ctx, conversationID, text, replyContext, onUpdate, onEvent)
}

func (a Dashboard) send(ctx context.Context, conversationID, text string, onUpdate func(string), onEvent agent.EventFunc) (agent.Result, error) {
	return a.sendWithReply(ctx, conversationID, text, "", onUpdate, onEvent)
}

func (a Dashboard) sendWithReply(ctx context.Context, conversationID, text, replyContext string, onUpdate func(string), onEvent agent.EventFunc) (agent.Result, error) {
	link, ok := findDashboardConversation(a.Service.Registry, conversationID)
	if !ok {
		return agent.Result{}, errors.New("dashboard conversation not found")
	}
	turn, err := a.Service.Runner.Submit(link, addReplyContext(text, replyContext))
	if err != nil {
		return agent.Result{}, err
	}
	_, result, err := a.Service.Runner.RunSubmittedWithEvents(ctx, turn, onUpdate, onEvent)
	return result, err
}

func addReplyContext(text, replyContext string) string {
	if replyContext == "" {
		return text
	}
	runes := []rune(replyContext)
	if len(runes) > 2000 {
		runes = runes[:2000]
	}
	return "[Quoted message context]\n" + string(runes) + "\n[/Quoted message context]\n\n" + text
}

func findDashboardConversation(registry *conversation.Registry, conversationID string) (conversation.Link, bool) {
	link, ok := registry.FindConversationFor("dashboard", conversationID)
	if !ok {
		link, ok = registry.FindConversationFor("telegram", conversationID)
	}
	return link, ok
}

func randomKey(prefix string) (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return prefix + ":" + hex.EncodeToString(raw[:]), nil
}
