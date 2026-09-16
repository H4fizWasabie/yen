package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/H4fizWasabie/yen/internal/adapters"
	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/codingagent"
	"github.com/H4fizWasabie/yen/internal/conversation"
	"github.com/H4fizWasabie/yen/internal/extensions"
	"github.com/H4fizWasabie/yen/internal/memory"
	"github.com/H4fizWasabie/yen/internal/provider"
	"github.com/H4fizWasabie/yen/internal/runtime"
	"github.com/H4fizWasabie/yen/internal/session"
	"github.com/H4fizWasabie/yen/internal/settings"
)

func main() {
	addr := flag.String("addr", ":8787", "HTTP listen address")
	workspace, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	flag.Parse()
	dataDir := os.Getenv("YEN_DATA_DIR")
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
	client := provider.ConfiguredFromEnv()
	runner := runtime.New(queue, client, func(workspace string) []agent.Tool { return codingagent.NewTools(workspace) })
	runner.PersistentTools = codingagent.NewExternalTools()
	defer func() { _ = codingagent.CloseTools(runner.PersistentTools) }()
	if current, err := settings.Load(workspace); err == nil {
		runner.ApplySettings(current)
	}
	runner.SessionToolFactory = func(workspace string, current *session.Session) []agent.Tool {
		return codingagent.NewToolsForSession(workspace, current)
	}
	runner.SessionToolFactoryWithProvider = func(workspace string, current *session.Session, client agent.Provider) []agent.Tool {
		return codingagent.NewToolsForSessionWithProviderWithoutExternal(workspace, current, client)
	}
	runner.SessionToolFactoryWithProviderAndRegistry = func(workspace string, current *session.Session, client agent.Provider, registry *extensions.Registry) []agent.Tool {
		return codingagent.NewToolsForSessionWithProviderAndRegistry(workspace, current, client, registry)
	}
	runner.AutoCompactTurns = runtime.AutoCompactTurnsFromEnv()
	runner.AutoCompactMaxHistoryTurns = runtime.AutoCompactMaxHistoryTurnsFromEnv()
	runner.AutoCompactKeepRecentTokens = runtime.AutoCompactKeepRecentTokensFromEnv()
	runner.AutoCompactContextWindow = runtime.AutoCompactContextWindowFromEnv()
	runner.AutoCompactReserveTokens = runtime.AutoCompactReserveTokensFromEnv()
	runner.AutoCompactDisabled = runtime.AutoCompactDisabledFromEnv()
	runner.AutoCompactOnOverflow = runtime.AutoCompactOnOverflowFromEnv()
	runner.AutoConsolidate = runtime.AutoConsolidateFromEnv()
	canonicalConversationID := os.Getenv("YEN_CANONICAL_CONVERSATION_ID")
	if canonicalConversationID == "" {
		canonicalConversationID = "yen-primary"
	}
	runner.SharedMemory = true
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
	handler := adapters.DashboardHTTP{AccessToken: os.Getenv("YEN_DASHBOARD_TOKEN"), Dashboard: adapters.Dashboard{Service: adapters.Service{Registry: registry, Runner: runner, CanonicalConversationID: canonicalConversationID}, Workspace: workspace}}
	log.Printf("theoses dashboard listening on %s", *addr)
	if err := http.ListenAndServe(*addr, handler); err != nil {
		log.Fatal(err)
	}
}
