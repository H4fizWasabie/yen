package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/codingagent"
	"github.com/H4fizWasabie/yen/internal/conversation"
	"github.com/H4fizWasabie/yen/internal/memory"
	"github.com/H4fizWasabie/yen/internal/provider"
	"github.com/H4fizWasabie/yen/internal/runtime"
	"github.com/H4fizWasabie/yen/internal/session"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("theoses", flag.ContinueOnError)
	flags.SetOutput(stderr)
	prompt := flags.String("p", "", "run one non-interactive prompt")
	semanticSource := flags.String("migrate-semantic", "", "copy semantic Markdown or legacy JSONL from this path")
	episodicSource := flags.String("migrate-episodes", "", "copy episodes from this SQLite database")
	memoryDir := flags.String("memory-dir", "", "target semantic memory directory")
	episodesDB := flags.String("episodes-db", "", "target episodic SQLite database")
	scope := flags.String("scope", "", "migration scope: engine, owner, workspace, or conversation")
	ownerID := flags.String("owner", "", "owner scope for migration")
	workspaceID := flags.String("workspace", "", "workspace scope for migration")
	conversationID := flags.String("conversation", "", "canonical conversation for migration")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *semanticSource != "" || *episodicSource != "" {
		return runMigration(stdout, stderr, *semanticSource, *episodicSource, *memoryDir, *episodesDB, *scope, *ownerID, *workspaceID, *conversationID)
	}
	if *prompt == "" {
		fmt.Fprintln(stderr, "usage: theoses -p PROMPT")
		return 2
	}

	cwd, err := os.Getwd()
	if err != nil {
		return reportError(stderr, err)
	}
	sessionPath := os.Getenv("THEOSES_SESSION_FILE")
	if sessionPath == "" {
		sessionPath = filepath.Join(cwd, ".theoses-go", "session.jsonl")
	}
	dataDir := os.Getenv("THEOSES_DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Dir(sessionPath)
	}
	registry, err := conversation.OpenRegistry(filepath.Join(dataDir, "conversations.jsonl"))
	if err != nil {
		return reportError(stderr, err)
	}
	canonicalConversationID := os.Getenv("THEOSES_CANONICAL_CONVERSATION_ID")
	var link conversation.Link
	if canonicalConversationID != "" {
		link, err = registry.ResolveShared("cli", cwd, cwd, canonicalConversationID)
	} else {
		link, err = registry.Resolve("cli", cwd, cwd)
	}
	if err != nil {
		return reportError(stderr, err)
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
	queue, err := conversation.OpenQueue(filepath.Join(dataDir, "conversation-queue.jsonl"))
	if err != nil {
		return reportError(stderr, err)
	}
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
	runner.SharedMemory = canonicalConversationID != ""
	runner.Checkpoints, err = memory.OpenCheckpoints(filepath.Join(dataDir, "consolidation-checkpoints.json"))
	if err != nil {
		return reportError(stderr, err)
	}
	runner.Memory, err = memory.OpenEngine(filepath.Join(dataDir, "memory"))
	if err != nil {
		return reportError(stderr, err)
	}
	defer runner.Memory.Close()
	runner.SessionPath = func(conversation.Turn) string { return sessionPath }
	turn, err := runner.Submit(link, *prompt)
	if err != nil {
		return reportError(stderr, err)
	}
	if _, result, err := runner.RunSubmitted(context.Background(), turn); err != nil {
		return reportError(stderr, err)
	} else {
		fmt.Fprintln(stdout, result.FinalText)
	}
	return 0
}

func runMigration(stdout, stderr io.Writer, semanticSource, episodicSource, memoryDir, episodesDB, scope, ownerID, workspaceID, conversationID string) int {
	if semanticSource != "" {
		if memoryDir == "" || scope == "" {
			fmt.Fprintln(stderr, "semantic migration requires -memory-dir and -scope")
			return 2
		}
		store := memory.NewStore(memoryDir)
		count, err := memory.MigrateSemantic(semanticSource, store, memory.Scope(scope), memory.Context{OwnerID: ownerID, WorkspaceID: workspaceID, ConversationID: conversationID})
		if err != nil {
			return reportError(stderr, err)
		}
		fmt.Fprintf(stdout, "migrated %d semantic nodes\n", count)
	}
	if episodicSource != "" {
		if episodesDB == "" || conversationID == "" {
			fmt.Fprintln(stderr, "episode migration requires -episodes-db and -conversation")
			return 2
		}
		store, err := memory.OpenEpisodicStore(episodesDB)
		if err != nil {
			return reportError(stderr, err)
		}
		defer store.Close()
		count, err := memory.MigrateEpisodes(episodicSource, store, conversationID, workspaceID)
		if err != nil {
			return reportError(stderr, err)
		}
		fmt.Fprintf(stdout, "migrated %d episodes\n", count)
	}
	return 0
}

func reportError(stderr io.Writer, err error) int {
	fmt.Fprintln(stderr, err)
	return 1
}
