package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/codingagent"
	"github.com/H4fizWasabie/yen/internal/conversation"
	"github.com/H4fizWasabie/yen/internal/memory"
	"github.com/H4fizWasabie/yen/internal/provider"
	"github.com/H4fizWasabie/yen/internal/runtime"
	"github.com/H4fizWasabie/yen/internal/session"
	"github.com/H4fizWasabie/yen/internal/settings"
	"github.com/H4fizWasabie/yen/internal/tools"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("theoses", flag.ContinueOnError)
	flags.SetOutput(stderr)
	prompt := flags.String("p", "", "run one non-interactive prompt")
	interactive := flags.Bool("i", false, "read prompts from stdin until EOF")
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
	if *prompt == "" && !*interactive {
		fmt.Fprintln(stderr, "usage: theoses -p PROMPT")
		return 2
	}

	cwd, err := os.Getwd()
	if err != nil {
		return reportError(stderr, err)
	}
	sessionPath := os.Getenv("YEN_SESSION_FILE")
	if sessionPath == "" {
		sessionPath = filepath.Join(cwd, ".theoses-go", "session.jsonl")
	}
	dataDir := os.Getenv("YEN_DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Dir(sessionPath)
	}
	registry, err := conversation.OpenRegistry(filepath.Join(dataDir, "conversations.jsonl"))
	if err != nil {
		return reportError(stderr, err)
	}
	canonicalConversationID := os.Getenv("YEN_CANONICAL_CONVERSATION_ID")
	if canonicalConversationID == "" {
		canonicalConversationID = "yen-primary"
	}
	var link conversation.Link
	link, err = registry.ResolveShared("cli", cwd, cwd, canonicalConversationID)
	if err != nil {
		return reportError(stderr, err)
	}
	client := provider.ConfiguredFromEnv()
	queue, err := conversation.OpenQueue(filepath.Join(dataDir, "conversation-queue.jsonl"))
	if err != nil {
		return reportError(stderr, err)
	}
	runner := runtime.New(queue, client, func(workspace string) []agent.Tool { return codingagent.NewTools(workspace) })
	runner.PersistentTools = codingagent.NewExternalTools()
	defer func() { _ = codingagent.CloseTools(runner.PersistentTools) }()
	if current, err := settings.Load(cwd); err == nil {
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
	currentSession, err := runner.OpenSession(link)
	if err != nil {
		return reportError(stderr, err)
	}
	runPrompt := func(prompt string) error {
		turn, err := runner.Submit(link, prompt)
		if err != nil {
			return err
		}
		_, result, err := runner.RunSubmitted(context.Background(), turn)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, result.FinalText)
		return err
	}
	if *interactive {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			prompt := scanner.Text()
			if prompt == "/quit" || prompt == "/exit" {
				break
			}
			if strings.TrimSpace(prompt) == "" {
				continue
			}
			handled, err := handleInteractiveCommand(prompt, currentSession, runner, link, &sessionPath, stdout)
			if err != nil {
				return reportError(stderr, err)
			}
			if handled {
				continue
			}
			if err := runPrompt(prompt); err != nil {
				return reportError(stderr, err)
			}
		}
		if err := scanner.Err(); err != nil {
			return reportError(stderr, err)
		}
		return 0
	}
	if err := runPrompt(*prompt); err != nil {
		return reportError(stderr, err)
	}
	return 0
}

func handleInteractiveCommand(input string, current *session.Session, runner *runtime.Runner, link conversation.Link, sessionPath *string, stdout io.Writer) (bool, error) {
	text := strings.TrimSpace(input)
	if strings.HasPrefix(text, "!") {
		excludeFromContext := strings.HasPrefix(text, "!!")
		command := strings.TrimSpace(strings.TrimPrefix(text, "!"))
		if excludeFromContext {
			command = strings.TrimSpace(strings.TrimPrefix(text, "!!"))
		}
		if command == "" {
			return true, nil
		}
		cwd := current.Header().CWD
		bashResult, err := tools.NewBashTool(cwd).ExecuteResult(context.Background(), map[string]any{"command": command})
		result := bashResult.Output
		message := session.Message{
			Role: "bashExecution", Command: command, Output: result, ExitCode: bashResult.ExitCode,
			Cancelled: bashResult.Cancelled, Truncated: bashResult.Truncated, FullOutputPath: bashResult.FullOutputPath,
			ExcludeFromContext: excludeFromContext,
		}
		if _, appendErr := current.Append(message); appendErr != nil {
			return true, appendErr
		}
		if result != "" {
			if _, writeErr := fmt.Fprintln(stdout, result); writeErr != nil {
				return true, writeErr
			}
		}
		if err != nil {
			_, writeErr := fmt.Fprintln(stdout, err)
			return true, writeErr
		}
		return true, nil
	}
	switch {
	case text == "/model" || strings.HasPrefix(text, "/model "):
		model := strings.TrimSpace(strings.TrimPrefix(text, "/model"))
		if model == "" {
			name, currentModel := provider.Describe(runner.Provider)
			_, err := fmt.Fprintf(stdout, "Provider: %s\nModel: %s\n", name, currentModel)
			return true, err
		}
		configured, err := provider.SetModel(runner.Provider, model)
		if err != nil {
			return true, err
		}
		runner.Provider = configured
		_, err = fmt.Fprintf(stdout, "Model set: %s\n", model)
		return true, err
	case text == "/scoped-models":
		models, err := provider.AvailableModels(context.Background(), runner.Provider)
		if err != nil {
			return true, err
		}
		data, err := json.Marshal(models)
		if err != nil {
			return true, err
		}
		_, err = fmt.Fprintf(stdout, "%s\n", data)
		return true, err
	case text == "/thinking" || strings.HasPrefix(text, "/thinking "):
		level := strings.TrimSpace(strings.TrimPrefix(text, "/thinking"))
		if level == "" {
			_, err := fmt.Fprintf(stdout, "Thinking: %s\n", provider.ThinkingLevel(runner.Provider))
			return true, err
		}
		configured, err := provider.SetThinkingLevel(runner.Provider, level)
		if err != nil {
			return true, err
		}
		runner.Provider = configured
		_, err = fmt.Fprintf(stdout, "Thinking set: %s\n", level)
		return true, err
	case text == "/retry" || strings.HasPrefix(text, "/retry "):
		value := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(text, "/retry")))
		if value != "on" && value != "off" {
			_, err := fmt.Fprintf(stdout, "Retry: %t\nUsage: /retry on|off\n", provider.RetryEnabled(runner.Provider))
			return true, err
		}
		configured, err := provider.SetRetryEnabled(runner.Provider, value == "on")
		if err != nil {
			return true, err
		}
		runner.Provider = configured
		_, err = fmt.Fprintf(stdout, "Retry %s\n", value)
		return true, err
	case text == "/trust":
		workspace := link.WorkspaceID
		if workspace == "" {
			workspace = current.Header().CWD
		}
		if workspace == "" {
			return true, fmt.Errorf("workspace is required for trust")
		}
		if err := settings.Trust(workspace); err != nil {
			return true, err
		}
		_, err := fmt.Fprintf(stdout, "Trusted: %s\n", workspace)
		return true, err
	case text == "/settings":
		workspace := link.WorkspaceID
		if workspace == "" {
			workspace = current.Header().CWD
		}
		configured, err := settings.Load(workspace)
		if err != nil {
			return true, err
		}
		data, err := json.Marshal(configured)
		if err != nil {
			return true, err
		}
		_, err = fmt.Fprintf(stdout, "%s\n", data)
		return true, err
	case text == "/reload":
		if err := current.Reload(); err != nil {
			return true, err
		}
		_, err := fmt.Fprintln(stdout, "Session reloaded")
		return true, err
	case text == "/name":
		if name := current.SessionName(); name != "" {
			_, err := fmt.Fprintf(stdout, "Session name: %s\n", name)
			return true, err
		}
		_, err := fmt.Fprintln(stdout, "Usage: /name <name>")
		return true, err
	case strings.HasPrefix(text, "/name "):
		name := strings.TrimSpace(strings.TrimPrefix(text, "/name "))
		if name == "" {
			_, err := fmt.Fprintln(stdout, "Usage: /name <name>")
			return true, err
		}
		_, err := current.AppendSessionInfo(name)
		if err != nil {
			return true, err
		}
		_, err = fmt.Fprintf(stdout, "Session name set: %s\n", current.SessionName())
		return true, err
	case text == "/session":
		stats := session.Stats(current)
		_, err := fmt.Fprintf(stdout, "Session Info\n\nName: %s\nFile: %s\nID: %s\nMessages: %v\n", current.SessionName(), current.Path(), current.Header().ID, stats["totalMessages"])
		return true, err
	case text == "/tree":
		data, err := json.Marshal(current.Tree())
		if err != nil {
			return true, err
		}
		_, err = fmt.Fprintf(stdout, "%s\n", data)
		return true, err
	case text == "/artifacts":
		catalog := current.ArtifactCatalog(16 * 1024)
		if catalog == "" {
			catalog = "No artifacts"
		}
		_, err := fmt.Fprintln(stdout, catalog)
		return true, err
	case text == "/export" || strings.HasPrefix(text, "/export "):
		path := strings.TrimSpace(strings.TrimPrefix(text, "/export"))
		if path == "" {
			path = filepath.Join(filepath.Dir(current.Path()), "session-export.jsonl")
		}
		if filepath.Ext(path) != ".jsonl" {
			return true, fmt.Errorf("only .jsonl export is supported")
		}
		if _, err := session.Import(current.Path(), path); err != nil {
			return true, err
		}
		_, err := fmt.Fprintf(stdout, "Session exported to: %s\n", path)
		return true, err
	case text == "/import" || strings.HasPrefix(text, "/import "):
		path := strings.TrimSpace(strings.TrimPrefix(text, "/import"))
		if path == "" {
			return true, fmt.Errorf("usage: /import <path.jsonl>")
		}
		if filepath.Clean(path) == filepath.Clean(current.Path()) {
			return true, fmt.Errorf("cannot import the active session")
		}
		if _, err := session.Import(path, current.Path()); err != nil {
			return true, err
		}
		if err := current.Reload(); err != nil {
			return true, err
		}
		_, err := fmt.Fprintf(stdout, "Session imported from: %s\n", path)
		return true, err
	case text == "/clone" || strings.HasPrefix(text, "/clone "):
		path := strings.TrimSpace(strings.TrimPrefix(text, "/clone"))
		if path == "" {
			path = filepath.Join(filepath.Dir(current.Path()), fmt.Sprintf("clone-%d.jsonl", time.Now().UnixNano()))
		}
		header := current.Header()
		header.ID = "clone-" + fmt.Sprint(time.Now().UnixNano())
		cloned, err := current.Fork(path, current.LeafID(), header)
		if err != nil {
			return true, err
		}
		if err := current.ReplaceFrom(cloned.Path()); err != nil {
			return true, err
		}
		*sessionPath = cloned.Path()
		_, err = fmt.Fprintf(stdout, "Session cloned to: %s\n", cloned.Path())
		return true, err
	case text == "/fork" || strings.HasPrefix(text, "/fork "):
		arguments := strings.Fields(strings.TrimSpace(strings.TrimPrefix(text, "/fork")))
		if len(arguments) == 0 {
			return true, fmt.Errorf("usage: /fork <entry-id> [path.jsonl]")
		}
		path := filepath.Join(filepath.Dir(current.Path()), fmt.Sprintf("fork-%d.jsonl", time.Now().UnixNano()))
		if len(arguments) > 1 {
			path = arguments[1]
		}
		header := current.Header()
		header.ID = "fork-" + fmt.Sprint(time.Now().UnixNano())
		forked, err := current.Fork(path, arguments[0], header)
		if err != nil {
			return true, err
		}
		if err := current.ReplaceFrom(forked.Path()); err != nil {
			return true, err
		}
		*sessionPath = forked.Path()
		_, err = fmt.Fprintf(stdout, "Session forked to: %s\n", forked.Path())
		return true, err
	case text == "/resume" || strings.HasPrefix(text, "/resume "):
		path := strings.TrimSpace(strings.TrimPrefix(text, "/resume"))
		if path == "" {
			return true, fmt.Errorf("usage: /resume <path.jsonl>")
		}
		if err := current.ReplaceFrom(path); err != nil {
			return true, err
		}
		*sessionPath = current.Path()
		_, err := fmt.Fprintf(stdout, "Session resumed: %s\n", current.Path())
		return true, err
	case text == "/new":
		path := filepath.Join(filepath.Dir(current.Path()), fmt.Sprintf("session-%d.jsonl", time.Now().UnixNano()))
		header := current.Header()
		header.ID = "session-" + fmt.Sprint(time.Now().UnixNano())
		header.ParentSession = ""
		fresh := session.New(path, header)
		if err := fresh.Save(); err != nil {
			return true, err
		}
		if err := current.ReplaceFrom(path); err != nil {
			return true, err
		}
		*sessionPath = path
		_, err := fmt.Fprintf(stdout, "New session started: %s\n", path)
		return true, err
	case text == "/working-note":
		note := current.WorkingNote()
		if note == "" {
			note = "Working Note is empty"
		} else {
			note = "Working Note\n\n" + note
		}
		_, err := fmt.Fprintln(stdout, note)
		return true, err
	case text == "/stats":
		data, err := json.Marshal(session.Stats(current))
		if err != nil {
			return true, err
		}
		_, err = fmt.Fprintf(stdout, "%s\n", data)
		return true, err
	case text == "/compact" || strings.HasPrefix(text, "/compact "):
		if err := runner.Compact(context.Background(), link.ConversationID, 2); err != nil {
			return true, err
		}
		_, err := fmt.Fprintln(stdout, "Session compacted")
		return true, err
	default:
		return false, nil
	}
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
