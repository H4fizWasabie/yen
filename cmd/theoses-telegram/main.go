package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/H4fizWasabie/yen/internal/adapters"
	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/conversation"
	"github.com/H4fizWasabie/yen/internal/memory"
	"github.com/H4fizWasabie/yen/internal/provider"
	"github.com/H4fizWasabie/yen/internal/runtime"
	"github.com/H4fizWasabie/yen/internal/tools"
)

func main() {
	workspace, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	dataDir := os.Getenv("THEOSES_DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Join(workspace, ".theoses-go")
	}
	registry, err := conversation.OpenRegistry(filepath.Join(dataDir, "conversations.jsonl"))
	if err != nil {
		log.Fatal(err)
	}
	queue, err := conversation.OpenQueue(filepath.Join(dataDir, "conversation-queue.jsonl"))
	if err != nil {
		log.Fatal(err)
	}
	baseURL := os.Getenv("THEOSES_OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	model := os.Getenv("THEOSES_MODEL")
	if model == "" {
		model = "gpt-4o-mini"
	}
	client := provider.NewOpenAICompletions(baseURL, os.Getenv("OPENAI_API_KEY"), model)
	runner := runtime.New(queue, client, func(workspace string) []agent.Tool { return []agent.Tool{tools.NewReadTool(workspace)} })
	runner.SessionPath = func(turn conversation.Turn) string {
		return filepath.Join(dataDir, "sessions", turn.ConversationID+".jsonl")
	}
	runner.Memory, err = memory.OpenEngine(filepath.Join(dataDir, "memory"))
	if err != nil {
		log.Fatal(err)
	}
	defer runner.Memory.Close()
	bot := &adapters.TelegramBot{Adapter: adapters.Telegram{Service: adapters.Service{Registry: registry, Runner: runner}, Workspace: workspace}, Token: os.Getenv("THEOSES_TELEGRAM_BOT_TOKEN"), OwnerChatID: os.Getenv("THEOSES_TELEGRAM_CHAT_ID"), APIBase: os.Getenv("THEOSES_TELEGRAM_API_BASE")}
	if err := bot.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
