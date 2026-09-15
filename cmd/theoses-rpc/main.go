package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/codingagent"
	"github.com/H4fizWasabie/yen/internal/conversation"
	"github.com/H4fizWasabie/yen/internal/memory"
	"github.com/H4fizWasabie/yen/internal/provider"
	"github.com/H4fizWasabie/yen/internal/rpc"
	"github.com/H4fizWasabie/yen/internal/runtime"
	"github.com/H4fizWasabie/yen/internal/session"
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
	client.ReasoningEffort = os.Getenv("THEOSES_REASONING_EFFORT")
	runner := runtime.New(queue, client, func(workspace string) []agent.Tool { return codingagent.NewTools(workspace) })
	runner.SessionToolFactory = func(workspace string, current *session.Session) []agent.Tool {
		return codingagent.NewToolsForSession(workspace, current)
	}
	runner.SessionToolFactoryWithProvider = func(workspace string, current *session.Session, client agent.Provider) []agent.Tool {
		return codingagent.NewToolsForSessionWithProvider(workspace, current, client)
	}
	runner.AutoCompactTurns = runtime.AutoCompactTurnsFromEnv()
	runner.AutoCompactMaxHistoryTurns = runtime.AutoCompactMaxHistoryTurnsFromEnv()
	runner.AutoCompactKeepRecentTokens = runtime.AutoCompactKeepRecentTokensFromEnv()
	runner.AutoCompactContextWindow = runtime.AutoCompactContextWindowFromEnv()
	runner.AutoCompactReserveTokens = runtime.AutoCompactReserveTokensFromEnv()
	runner.AutoCompactDisabled = runtime.AutoCompactDisabledFromEnv()
	runner.AutoCompactOnOverflow = runtime.AutoCompactOnOverflowFromEnv()
	runner.AutoConsolidate = runtime.AutoConsolidateFromEnv()
	runner.SharedMemory = os.Getenv("THEOSES_CANONICAL_CONVERSATION_ID") != ""
	runner.SessionPath = func(turn conversation.Turn) string {
		return filepath.Join(dataDir, "sessions", turn.ConversationID+".jsonl")
	}
	runner.Memory, err = memory.OpenEngine(filepath.Join(dataDir, "memory"))
	if err != nil {
		log.Fatal(err)
	}
	defer runner.Memory.Close()
	canonical := os.Getenv("THEOSES_CANONICAL_CONVERSATION_ID")
	var link conversation.Link
	if canonical != "" {
		link, err = registry.ResolveShared("rpc", workspace, workspace, canonical)
	} else {
		link, err = registry.Resolve("rpc", workspace, workspace)
	}
	if err != nil {
		log.Fatal(err)
	}
	if err := (&rpc.Server{Runner: runner, Link: link}).Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}
