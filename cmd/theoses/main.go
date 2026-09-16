package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/auth"
	"github.com/H4fizWasabie/yen/internal/codingagent"
	"github.com/H4fizWasabie/yen/internal/conversation"
	"github.com/H4fizWasabie/yen/internal/extensions"
	"github.com/H4fizWasabie/yen/internal/memory"
	"github.com/H4fizWasabie/yen/internal/provider"
	"github.com/H4fizWasabie/yen/internal/runtime"
	"github.com/H4fizWasabie/yen/internal/session"
	"github.com/H4fizWasabie/yen/internal/settings"
	"github.com/H4fizWasabie/yen/internal/tools"
	"github.com/H4fizWasabie/yen/internal/tui"
)

func main() {
	os.Exit(runWithInput(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runWithInput(args, os.Stdin, stdout, stderr)
}

func runWithInput(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
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
	var interactiveReader *bufio.Reader
	rawInput := false
	var screen *tui.Screen
	agentDir, _ := os.UserConfigDir()
	configured := []string(nil)
	if current, err := settings.Load(cwd); err == nil {
		configured = current.Extensions
	}
	operatorConfigured := strings.FieldsFunc(os.Getenv("YEN_EXTENSIONS"), func(r rune) bool { return r == os.PathListSeparator || r == ',' })
	loaded, _ := extensions.DiscoverAndLoadWithOperatorPaths(cwd, filepath.Join(agentDir, "yen"), configured, operatorConfigured)
	if loaded != nil {
		defer loaded.Close()
		runner.ExtensionRegistry = loaded.Registry
		loaded.SetUIRequester(func(ctx context.Context, request map[string]any) (map[string]any, error) {
			if interactiveReader == nil {
				return nil, errors.New("interactive extension UI is unavailable")
			}
			return tui.HandleExtensionUIWithScreenMode(ctx, request, interactiveReader, stdout, screen, rawInput)
		})
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
	runPrompt := func(prompt string, out io.Writer, onUpdate func(string), onEvent agent.EventFunc) (string, error) {
		turn, err := runner.Submit(link, prompt)
		if err != nil {
			return "", err
		}
		_, result, err := runner.RunSubmittedWithEvents(context.Background(), turn, onUpdate, onEvent)
		if err != nil {
			return "", err
		}
		_, err = fmt.Fprintln(out, result.FinalText)
		return result.FinalText, err
	}
	if *interactive {
		screen = &tui.Screen{Status: "Ready"}
		if err := screen.Render(stdout); err != nil {
			return reportError(stderr, err)
		}
		interactiveReader = bufio.NewReader(stdin)
		reader := interactiveReader
		history := tui.NewLineHistory()
		restoreTerminal, enabled, err := tui.EnableRawInput(stdin)
		if err != nil {
			return reportError(stderr, err)
		}
		rawInput = enabled
		defer func() { _ = restoreTerminal() }()
		for {
			var prompt string
			var readErr error
			if rawInput {
				prompt, readErr = tui.ReadLineWithOutputAndHistory(reader, stdout, "> ", history)
			} else {
				prompt, readErr = tui.ReadLineWithHistory(reader, history)
			}
			if readErr != nil {
				if readErr != io.EOF {
					return reportError(stderr, readErr)
				}
				break
			}
			screen.Input = prompt
			if prompt == "/quit" || prompt == "/exit" {
				break
			}
			if strings.TrimSpace(prompt) == "" {
				continue
			}
			var response strings.Builder
			handled, err := handleInteractiveTree(prompt, reader, rawInput, currentSession, &response)
			if !handled && err == nil {
				handled, err = handleInteractiveSelector(prompt, reader, rawInput, currentSession, runner, link, &sessionPath, &response)
			}
			if !handled && err == nil {
				handled, err = handleInteractiveCommand(prompt, currentSession, runner, link, &sessionPath, &response)
			}
			if err != nil {
				return reportError(stderr, err)
			}
			if handled {
				if response.Len() > 0 {
					screen.Scrollback = append(screen.Scrollback, strings.TrimSuffix(response.String(), "\n"))
				}
				screen.Input = ""
				if err := screen.Render(stdout); err != nil {
					return reportError(stderr, err)
				}
				continue
			}
			screen.Status = "Streaming"
			messageCount := len(currentSession.Messages())
			var renderErr error
			result, err := runPrompt(prompt, &response, func(update string) {
				screen.Status = "Streaming: " + strings.TrimSpace(update)
				if renderErr == nil {
					renderErr = screen.Render(stdout)
				}
			}, func(event agent.Event) {
				switch event.Type {
				case "tool_execution_start":
					screen.Status = interactiveToolStatus(event)
				case "tool_execution_end":
					if result := interactiveToolResult(event); result != "" {
						screen.Scrollback = append(screen.Scrollback, result)
					}
					screen.Status = "Streaming"
				case "tool_result":
					screen.Status = "Streaming"
				default:
					return
				}
				if renderErr == nil {
					renderErr = screen.Render(stdout)
				}
			})
			if err != nil {
				return reportError(stderr, err)
			}
			if renderErr != nil {
				return reportError(stderr, renderErr)
			}
			screen.Status = "Ready"
			if runner.ExtensionRegistry != nil {
				if err := currentSession.Reload(); err != nil {
					return reportError(stderr, err)
				}
				messages := currentSession.Messages()
				if messageCount > len(messages) {
					messageCount = 0
				}
				for _, message := range messages[messageCount:] {
					if rendered, ok := tui.RenderMessage(runner.ExtensionRegistry, message); ok {
						screen.Scrollback = append(screen.Scrollback, rendered)
					}
				}
			}
			screen.Scrollback = append(screen.Scrollback, prompt, result)
			screen.Input = ""
			if err := screen.Render(stdout); err != nil {
				return reportError(stderr, err)
			}
		}
		return 0
	}
	if _, err := runPrompt(*prompt, stdout, nil, nil); err != nil {
		return reportError(stderr, err)
	}
	return 0
}

func interactiveToolStatus(event agent.Event) string {
	preview := ""
	for _, key := range []string{"command", "path", "query", "note"} {
		if value, ok := event.Args[key].(string); ok {
			preview = strings.Join(strings.Fields(value), " ")
			break
		}
	}
	if preview == "" {
		preview = "tool call"
	}
	runes := []rune(preview)
	if len(runes) > 140 {
		preview = string(runes[:137]) + "..."
	}
	return "Running " + event.Name + ": " + preview
}

func interactiveToolResult(event agent.Event) string {
	result := strings.Join(strings.Fields(event.Result), " ")
	if result == "" {
		return ""
	}
	runes := []rune(result)
	if len(runes) > 140 {
		result = string(runes[:137]) + "..."
	}
	return "Tool " + event.Name + ": " + result
}

func handleInteractiveTree(input string, reader *bufio.Reader, rawInput bool, current *session.Session, stdout io.Writer) (bool, error) {
	if strings.TrimSpace(input) != "/tree" {
		return false, nil
	}
	return runTreeSelector(reader, rawInput, current, stdout)
}

// runTreeSelector shows the tree selector and, once a non-leaf entry is
// chosen, walks the oracle's summarize-before-navigate prompt loop
// (packages/coding-agent/src/modes/interactive/interactive-mode.ts:4953-5031):
// cancelling the branch-summary choice re-shows the tree selector, and
// cancelling the custom-instructions prompt loops back to the summary
// choice instead of the tree selector. This slice implements only the
// prompt/navigation flow, not actual LLM-generated branch summary content:
// the chosen summary mode is recorded in the status message and not yet
// persisted or used to summarize the abandoned branch. It also does not
// preserve the previously highlighted entry when re-showing the tree
// selector after a cancelled summary choice, unlike the oracle's
// initialSelectedId.
func runTreeSelector(reader *bufio.Reader, rawInput bool, current *session.Session, stdout io.Writer) (bool, error) {
	for {
		selected, err := tui.SelectTree(reader, stdout, "Select tree entry", current.Tree(), rawInput)
		if err != nil || selected == "" {
			return true, err
		}
		if selected == current.LeafID() {
			_, err = fmt.Fprintln(stdout, "Already at this point")
			return true, err
		}

		summaryChoice, rescanTree, err := chooseBranchSummary(reader, rawInput, stdout)
		if err != nil {
			return true, err
		}
		if rescanTree {
			continue
		}

		if err := current.Branch(selected); err != nil {
			return true, err
		}
		status := fmt.Sprintf("Branched from: %s", selected)
		if summaryChoice != "No summary" {
			status += " (summary requested; generation not yet implemented)"
		}
		_, err = fmt.Fprintln(stdout, status)
		return true, err
	}
}

// chooseBranchSummary presents the oracle's "Summarize branch?" choice
// (interactive-mode.ts:4986-4990) and, for "Summarize with custom prompt",
// its follow-up instructions prompt (interactive-mode.ts:5000-5006).
// Escaping the choice returns rescanTree=true; "q" at the instructions
// prompt matches this codebase's existing extension UI text-input
// cancellation convention (internal/tui/ui.go's "input"/"editor" handling)
// and loops back to the summary choice.
func chooseBranchSummary(reader *bufio.Reader, rawInput bool, stdout io.Writer) (choice string, rescanTree bool, err error) {
	options := []string{"No summary", "Summarize", "Summarize with custom prompt"}
	selectFn := tui.Select
	if rawInput {
		selectFn = tui.SelectRaw
	}
	for {
		index, err := selectFn(reader, stdout, "Summarize branch?", options)
		if err != nil {
			return "", false, err
		}
		if index < 0 {
			return "", true, nil
		}
		choice := options[index]
		if choice != "Summarize with custom prompt" {
			return choice, false, nil
		}
		if _, err := fmt.Fprint(stdout, "Custom summarization instructions\n> "); err != nil {
			return "", false, err
		}
		line, err := reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			return "", false, err
		}
		value := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if value == "q" {
			continue
		}
		return choice, false, nil
	}
}

func handleInteractiveSelector(input string, reader *bufio.Reader, rawInput bool, current *session.Session, runner *runtime.Runner, link conversation.Link, sessionPath *string, stdout io.Writer) (bool, error) {
	switch strings.TrimSpace(input) {
	case "/model":
		models, err := provider.AvailableModels(context.Background(), runner.Provider)
		if err != nil {
			return true, err
		}
		options := make([]string, len(models))
		for i, model := range models {
			options[i] = model.Provider + "/" + model.ID
		}
		selectFn := tui.Select
		if rawInput {
			selectFn = tui.SelectRaw
		}
		selected, err := selectFn(reader, stdout, "Select model", options)
		if err != nil || selected < 0 {
			return true, err
		}
		configured, err := provider.SetModel(runner.Provider, models[selected].ID)
		if err != nil {
			return true, err
		}
		runner.Provider = configured
		_, err = fmt.Fprintf(stdout, "Model set: %s\n", models[selected].ID)
		return true, err
	case "/resume":
		paths, err := filepath.Glob(filepath.Join(filepath.Dir(current.Path()), "*.jsonl"))
		if err != nil {
			return true, err
		}
		validPaths := paths[:0]
		for _, path := range paths {
			if _, err := session.Open(path); err == nil {
				validPaths = append(validPaths, path)
			}
		}
		paths = validPaths
		sort.Strings(paths)
		options := make([]string, len(paths))
		for i, path := range paths {
			options[i] = filepath.Base(path)
		}
		selectFn := tui.Select
		if rawInput {
			selectFn = tui.SelectRaw
		}
		selected, err := selectFn(reader, stdout, "Select session", options)
		if err != nil || selected < 0 {
			return true, err
		}
		if err := current.ReplaceFrom(paths[selected]); err != nil {
			return true, err
		}
		*sessionPath = current.Path()
		_, err = fmt.Fprintf(stdout, "Session resumed: %s\n", current.Path())
		return true, err
	default:
		return false, nil
	}
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
		resourceSettings, _ := settings.Load(cwd)
		bashResult, err := tools.NewBashToolWithOptions(cwd, tools.BashOptions{ShellPath: resourceSettings.ShellPath, CommandPrefix: resourceSettings.ShellCommandPrefix}).ExecuteResult(context.Background(), map[string]any{"command": command})
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
	case text == "/login openai-codex":
		path := strings.TrimSpace(os.Getenv("YEN_AUTH_FILE"))
		if path == "" {
			return true, fmt.Errorf("YEN_AUTH_FILE is required for /login")
		}
		credential, err := auth.LoginOpenAICodexDevice(context.Background(), func(message string) {
			_, _ = fmt.Fprintln(stdout, message)
		})
		if err != nil {
			return true, err
		}
		if _, err := auth.Open(path).Modify("openai-codex", func(*auth.Credential) (*auth.Credential, error) {
			return &credential, nil
		}); err != nil {
			return true, err
		}
		_, err = fmt.Fprintln(stdout, "Logged in: openai-codex")
		return true, err
	case text == "/login openai-codex browser":
		path := strings.TrimSpace(os.Getenv("YEN_AUTH_FILE"))
		if path == "" {
			return true, fmt.Errorf("YEN_AUTH_FILE is required for /login")
		}
		credential, err := auth.LoginOpenAICodexBrowser(context.Background(), func(message string) {
			_, _ = fmt.Fprintln(stdout, message)
		})
		if err != nil {
			return true, err
		}
		if _, err := auth.Open(path).Modify("openai-codex", func(*auth.Credential) (*auth.Credential, error) {
			return &credential, nil
		}); err != nil {
			return true, err
		}
		_, err = fmt.Fprintln(stdout, "Logged in: openai-codex")
		return true, err
	case text == "/login anthropic":
		path := strings.TrimSpace(os.Getenv("YEN_AUTH_FILE"))
		if path == "" {
			return true, fmt.Errorf("YEN_AUTH_FILE is required for /login")
		}
		credential, err := auth.LoginAnthropic(context.Background(), func(message string) {
			_, _ = fmt.Fprintln(stdout, message)
		})
		if err != nil {
			return true, err
		}
		if _, err := auth.Open(path).Modify("anthropic", func(*auth.Credential) (*auth.Credential, error) {
			return &credential, nil
		}); err != nil {
			return true, err
		}
		_, err = fmt.Fprintln(stdout, "Logged in: anthropic")
		return true, err
	case text == "/login amazon-bedrock" || strings.HasPrefix(text, "/login amazon-bedrock "):
		path := strings.TrimSpace(os.Getenv("YEN_AUTH_FILE"))
		if path == "" {
			return true, fmt.Errorf("YEN_AUTH_FILE is required for /login")
		}
		parts := strings.Fields(strings.TrimSpace(strings.TrimPrefix(text, "/login amazon-bedrock")))
		credential := auth.Credential{Type: "api_key"}
		switch {
		case len(parts) == 2 && parts[0] == "bearer-token":
			credential.Key = parts[1]
		case len(parts) == 2 && parts[0] == "aws-profile":
			credential.Env = map[string]string{"AWS_PROFILE": parts[1]}
		case len(parts) == 1 && parts[0] == "credential-chain":
		default:
			return true, fmt.Errorf("usage: /login amazon-bedrock bearer-token <token> | aws-profile <profile> | credential-chain")
		}
		if _, err := auth.Open(path).Modify("amazon-bedrock", func(*auth.Credential) (*auth.Credential, error) {
			return &credential, nil
		}); err != nil {
			return true, err
		}
		_, err := fmt.Fprintln(stdout, "Logged in: amazon-bedrock")
		return true, err
	case text == "/login google-vertex" || strings.HasPrefix(text, "/login google-vertex "):
		path := strings.TrimSpace(os.Getenv("YEN_AUTH_FILE"))
		if path == "" {
			return true, fmt.Errorf("YEN_AUTH_FILE is required for /login")
		}
		parts := strings.Fields(strings.TrimSpace(strings.TrimPrefix(text, "/login google-vertex")))
		credential := auth.Credential{Type: "api_key"}
		switch {
		case len(parts) == 2 && parts[0] == "api-key":
			credential.Key = parts[1]
		case len(parts) == 3 && parts[0] == "adc":
			credential.Env = map[string]string{"GOOGLE_CLOUD_PROJECT": parts[1], "GOOGLE_CLOUD_LOCATION": parts[2]}
		case len(parts) == 4 && parts[0] == "service-account":
			credential.Env = map[string]string{
				"GOOGLE_CLOUD_PROJECT":           parts[1],
				"GOOGLE_CLOUD_LOCATION":          parts[2],
				"GOOGLE_APPLICATION_CREDENTIALS": parts[3],
			}
		default:
			return true, fmt.Errorf("usage: /login google-vertex api-key <key> | adc <project> <location> | service-account <project> <location> <credentials-path>")
		}
		if _, err := auth.Open(path).Modify("google-vertex", func(*auth.Credential) (*auth.Credential, error) {
			return &credential, nil
		}); err != nil {
			return true, err
		}
		_, err := fmt.Fprintln(stdout, "Logged in: google-vertex")
		return true, err
	case text == "/login github-copilot" || strings.HasPrefix(text, "/login github-copilot "):
		path := strings.TrimSpace(os.Getenv("YEN_AUTH_FILE"))
		if path == "" {
			return true, fmt.Errorf("YEN_AUTH_FILE is required for /login")
		}
		domain := strings.TrimSpace(strings.TrimPrefix(text, "/login github-copilot"))
		if domain == "" {
			domain = "github.com"
		}
		enterpriseURL := ""
		if domain != "github.com" {
			enterpriseURL = domain
		}
		credential, err := auth.LoginGitHubCopilotForDomain(context.Background(), domain, enterpriseURL, func(message string) {
			_, _ = fmt.Fprintln(stdout, message)
		})
		if err != nil {
			return true, err
		}
		if _, err := auth.Open(path).Modify("github-copilot", func(*auth.Credential) (*auth.Credential, error) {
			return &credential, nil
		}); err != nil {
			return true, err
		}
		_, err = fmt.Fprintln(stdout, "Logged in: github-copilot")
		return true, err
	case text == "/logout" || strings.HasPrefix(text, "/logout "):
		path := strings.TrimSpace(os.Getenv("YEN_AUTH_FILE"))
		if path == "" {
			return true, fmt.Errorf("YEN_AUTH_FILE is required for /logout")
		}
		store := auth.Open(path)
		providerID := strings.TrimSpace(strings.TrimPrefix(text, "/logout"))
		if providerID == "" {
			credentials, err := store.List()
			if err != nil {
				return true, err
			}
			sort.Slice(credentials, func(i, j int) bool { return credentials[i].Provider < credentials[j].Provider })
			data, err := json.Marshal(credentials)
			if err != nil {
				return true, err
			}
			_, err = fmt.Fprintf(stdout, "%s\n", data)
			return true, err
		}
		if err := store.Delete(providerID); err != nil {
			return true, err
		}
		_, err := fmt.Fprintf(stdout, "Logged out: %s\n", providerID)
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
	case text == "/changelog":
		path := filepath.Join(current.Header().CWD, "CHANGELOG.md")
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			_, err = fmt.Fprintln(stdout, "No changelog entries found.")
			return true, err
		}
		if err != nil {
			return true, err
		}
		_, err = fmt.Fprintln(stdout, strings.TrimSpace(string(data)))
		return true, err
	case text == "/hotkeys":
		_, err := fmt.Fprintln(stdout, "Keyboard Shortcuts\n\n↑/↓ history or cursor\nEnter submit\nCtrl+C interrupt\nCtrl+D exit\n! bash\n!! bash (excluded from context)")
		return true, err
	case text == "/debug":
		_, err := fmt.Fprintf(stdout, "Debug\n\nSession: %s\nMessages: %d\n", current.Path(), len(current.Messages()))
		return true, err
	case text == "/arminsayshi":
		_, err := fmt.Fprintln(stdout, "Hi, Armin!")
		return true, err
	case text == "/dementedelves":
		return true, nil
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
	case text == "/copy":
		content := lastAssistantText(current)
		if content == "" {
			_, err := fmt.Fprintln(stdout, "No agent messages to copy yet.")
			return true, err
		}
		if err := copyToClipboard(content, stdout); err != nil {
			_, writeErr := fmt.Fprintf(stdout, "Copy failed: %v\n", err)
			return true, writeErr
		}
		_, err := fmt.Fprintln(stdout, "Copied last agent message to clipboard")
		return true, err
	case text == "/tree":
		lines := tui.RenderTree(current.Tree(), current.LeafID())
		_, err := fmt.Fprintf(stdout, "%s\n", strings.Join(lines, "\n"))
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
		instructions := strings.TrimSpace(strings.TrimPrefix(text, "/compact"))
		if err := runner.Compact(context.Background(), link.ConversationID, 2, instructions); err != nil {
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

func lastAssistantText(current *session.Session) string {
	messages := current.Messages()
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Role != "assistant" || (message.StopReason == "aborted" && contentTextForCopy(message.Content) == "") {
			continue
		}
		if text := strings.TrimSpace(contentTextForCopy(message.Content)); text != "" {
			return text
		}
	}
	return ""
}

func contentTextForCopy(content any) string {
	if text, ok := content.(string); ok {
		return text
	}
	if parts, ok := content.([]session.ContentPart); ok {
		var builder strings.Builder
		for _, part := range parts {
			if part.Type == "text" {
				builder.WriteString(part.Text)
			}
		}
		return builder.String()
	}
	data, err := json.Marshal(content)
	if err != nil {
		return ""
	}
	var parts []session.ContentPart
	if json.Unmarshal(data, &parts) != nil {
		return ""
	}
	return contentTextForCopy(parts)
}

func copyToClipboard(text string, stdout io.Writer) error {
	remote := os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_CLIENT") != "" || os.Getenv("MOSH_CONNECTION") != ""
	if remote {
		encoded := base64.StdEncoding.EncodeToString([]byte(text))
		if len(encoded) <= 100000 {
			_, err := fmt.Fprintf(stdout, "\x1b]52;c;%s\a", encoded)
			return err
		}
	}
	candidates := [][]string{{"wl-copy", "--type", "text/plain"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}, {"pbcopy"}, {"clip.exe"}}
	for _, candidate := range candidates {
		if _, err := exec.LookPath(candidate[0]); err != nil {
			continue
		}
		command := exec.Command(candidate[0], candidate[1:]...)
		command.Stdin = strings.NewReader(text)
		if err := command.Run(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("no supported clipboard command found")
}
