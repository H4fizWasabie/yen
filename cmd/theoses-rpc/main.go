package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/codingagent"
	"github.com/H4fizWasabie/yen/internal/conversation"
	"github.com/H4fizWasabie/yen/internal/extensions"
	"github.com/H4fizWasabie/yen/internal/memory"
	"github.com/H4fizWasabie/yen/internal/provider"
	"github.com/H4fizWasabie/yen/internal/rpc"
	"github.com/H4fizWasabie/yen/internal/runtime"
	"github.com/H4fizWasabie/yen/internal/session"
	"github.com/H4fizWasabie/yen/internal/settings"
)

func main() {
	workspace, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
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
	runner.AutoCompactTurns = runtime.AutoCompactTurnsFromEnv()
	runner.AutoCompactMaxHistoryTurns = runtime.AutoCompactMaxHistoryTurnsFromEnv()
	runner.AutoCompactKeepRecentTokens = runtime.AutoCompactKeepRecentTokensFromEnv()
	runner.AutoCompactContextWindow = runtime.AutoCompactContextWindowFromEnv()
	runner.AutoCompactReserveTokens = runtime.AutoCompactReserveTokensFromEnv()
	runner.AutoCompactDisabled = runtime.AutoCompactDisabledFromEnv()
	runner.AutoCompactOnOverflow = runtime.AutoCompactOnOverflowFromEnv()
	runner.AutoConsolidate = runtime.AutoConsolidateFromEnv()
	canonical := os.Getenv("YEN_CANONICAL_CONVERSATION_ID")
	if canonical == "" {
		canonical = "yen-primary"
	}
	runner.SharedMemory = true
	runner.SessionPath = func(turn conversation.Turn) string {
		return filepath.Join(dataDir, "sessions", turn.ConversationID+".jsonl")
	}
	runner.Memory, err = memory.OpenEngine(filepath.Join(dataDir, "memory"))
	if err != nil {
		log.Fatal(err)
	}
	defer runner.Memory.Close()
	var link conversation.Link
	link, err = registry.ResolveShared("rpc", workspace, workspace, canonical)
	if err != nil {
		log.Fatal(err)
	}
	agentDir, _ := os.UserConfigDir()
	agentDir = filepath.Join(agentDir, "yen")
	configured := []string(nil)
	if current, err := settings.Load(workspace); err == nil {
		configured = current.Extensions
	}
	operatorConfigured := strings.FieldsFunc(os.Getenv("YEN_EXTENSIONS"), func(r rune) bool { return r == os.PathListSeparator || r == ',' })
	loaded, _ := extensions.DiscoverAndLoadWithOperatorPaths(workspace, agentDir, configured, operatorConfigured)
	if loaded != nil {
		defer loaded.Close()
		runner.ExtensionRegistry = loaded.Registry
	}
	server := &rpc.Server{Runner: runner, Link: link}
	if loaded != nil {
		server.Extensions = loaded.Registry
		loaded.SetUIRequester(server.RequestExtensionUI)
	}
	if socket := os.Getenv("YEN_RPC_UNIX_SOCKET"); socket != "" {
		if err := rpc.ServeUnix(context.Background(), socket, server); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := server.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}
