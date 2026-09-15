package main

import (
	"flag"
	"log"
	"net/http"
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
	addr := flag.String("addr", ":8787", "HTTP listen address")
	workspace, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	flag.Parse()
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
	provider := provider.NewOpenAICompletions(baseURL, os.Getenv("OPENAI_API_KEY"), model)
	provider.ReasoningEffort = os.Getenv("THEOSES_REASONING_EFFORT")
	runner := runtime.New(queue, provider, func(workspace string) []agent.Tool { return []agent.Tool{tools.NewReadTool(workspace)} })
	runner.AutoCompactTurns = runtime.AutoCompactTurnsFromEnv()
	runner.AutoCompactMaxHistoryTurns = runtime.AutoCompactMaxHistoryTurnsFromEnv()
	runner.AutoCompactKeepRecentTokens = runtime.AutoCompactKeepRecentTokensFromEnv()
	runner.AutoCompactContextWindow = runtime.AutoCompactContextWindowFromEnv()
	runner.AutoCompactReserveTokens = runtime.AutoCompactReserveTokensFromEnv()
	runner.AutoCompactOnOverflow = runtime.AutoCompactOnOverflowFromEnv()
	runner.AutoConsolidate = runtime.AutoConsolidateFromEnv()
	runner.SharedMemory = os.Getenv("THEOSES_CANONICAL_CONVERSATION_ID") != ""
	runner.SessionPath = func(turn conversation.Turn) string {
		return filepath.Join(dataDir, "sessions", turn.ConversationID+".jsonl")
	}
	runner.Checkpoints, err = memory.OpenCheckpoints(filepath.Join(dataDir, "consolidation-checkpoints.json"))
	if err != nil {
		log.Fatal(err)
	}
	runner.Memory, err = memory.OpenEngine(filepath.Join(dataDir, "memory"))
	if err != nil {
		log.Fatal(err)
	}
	defer runner.Memory.Close()
	handler := adapters.DashboardHTTP{AccessToken: os.Getenv("THEOSES_DASHBOARD_TOKEN"), Dashboard: adapters.Dashboard{Service: adapters.Service{Registry: registry, Runner: runner, CanonicalConversationID: os.Getenv("THEOSES_CANONICAL_CONVERSATION_ID")}, Workspace: workspace}}
	log.Printf("theoses dashboard listening on %s", *addr)
	if err := http.ListenAndServe(*addr, handler); err != nil {
		log.Fatal(err)
	}
}
