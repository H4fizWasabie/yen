package extensions

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/settings"
)

type LoadError struct{ Path, Error string }
type LoadedExtension struct{ Path, Name string }
type LoadResult struct {
	Registry   *Registry
	Extensions []LoadedExtension
	Errors     []LoadError
	bridges    []*bridge
}

func (r *LoadResult) Close() error {
	var first error
	for _, b := range r.bridges {
		if err := b.close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func DiscoverAndLoad(workspace, agentDir string, configured []string) (*LoadResult, error) {
	if workspace == "" {
		return nil, errors.New("workspace is required")
	}
	workspace, _ = filepath.Abs(workspace)
	paths := discover(filepath.Join(workspace, ".theoses", "extensions"))
	paths = append(paths, discover(filepath.Join(agentDir, "extensions"))...)
	disabled := map[string]bool{}
	explicit := map[string]bool{}
	for _, raw := range configured {
		enabled := !strings.HasPrefix(raw, "!") && !strings.HasPrefix(raw, "-")
		raw = strings.TrimLeft(raw, "!-")
		if !filepath.IsAbs(raw) {
			raw = filepath.Join(workspace, raw)
		}
		raw, _ = filepath.Abs(raw)
		if !enabled {
			disabled[raw] = true
			continue
		}
		if info, err := os.Stat(raw); err == nil && info.IsDir() {
			for _, path := range extensionEntries(raw) {
				paths = append(paths, path)
				explicit[path] = true
			}
		} else {
			paths = append(paths, raw)
			explicit[raw] = true
		}
	}
	trusted := settings.IsTrusted(workspace)
	result := &LoadResult{Registry: New()}
	seen := map[string]bool{}
	for _, path := range paths {
		path, _ = filepath.Abs(path)
		if seen[path] || isDisabled(path, disabled) {
			continue
		}
		seen[path] = true
		if !trusted && !explicit[path] && isUnder(path, filepath.Join(workspace, ".theoses", "extensions")) {
			continue
		}
		b, err := startBridge(path)
		if err != nil {
			result.Errors = append(result.Errors, LoadError{Path: path, Error: err.Error()})
			continue
		}
		result.bridges = append(result.bridges, b)
		result.Extensions = append(result.Extensions, LoadedExtension{Path: path, Name: b.name})
		registerBridge(result.Registry, b)
	}
	return result, nil
}

func registerBridge(registry *Registry, b *bridge) {
	for _, command := range b.commands {
		command := command
		_ = registry.RegisterCommand(Command{Name: command.Name, Description: command.Description, Handler: func(ctx context.Context, args string) error {
			var ignored string
			return b.call(ctx, "command", command.Name, args, &ignored)
		}})
	}
	_ = registry.RegisterHooks(Hooks{
		BeforeTool: func(ctx context.Context, message agent.Message, call agent.ToolCall) (bool, string, error) {
			var result struct {
				Block  bool   `json:"block"`
				Reason string `json:"reason"`
			}
			err := b.call(ctx, "event", "tool_call", map[string]any{"message": message, "toolCall": call}, &result)
			return result.Block, result.Reason, err
		},
		AfterTool: func(ctx context.Context, message agent.Message, call agent.ToolCall, tool agent.ToolResult, isError bool) (agent.ToolResult, bool, error) {
			var result struct {
				Result  agent.ToolResult `json:"result"`
				IsError bool             `json:"isError"`
			}
			err := b.call(ctx, "event", "tool_result", map[string]any{"message": message, "toolCall": call, "result": tool, "isError": isError}, &result)
			if result.Result.Text != "" || result.Result.Images != nil {
				tool = result.Result
			}
			return tool, result.IsError, err
		},
		BeforeProvider: func(ctx context.Context, messages []agent.Message, tools []string) ([]agent.Message, error) {
			var result struct {
				Messages []agent.Message `json:"messages"`
			}
			err := b.call(ctx, "event", "before_provider_request", map[string]any{"messages": messages, "tools": tools}, &result)
			if result.Messages != nil {
				messages = result.Messages
			}
			return messages, err
		},
		AfterProvider: func(ctx context.Context, response agent.Response) error {
			return b.call(ctx, "event", "after_provider_response", response, nil)
		},
		ProviderHeaders: func(ctx context.Context, headers map[string][]string) {
			var result map[string][]string
			if b.call(ctx, "event", "before_provider_headers", map[string]any{"headers": headers}, &result) == nil && result != nil {
				for key := range headers {
					delete(headers, key)
				}
				for key, values := range result {
					headers[key] = values
				}
			}
		},
	})
	for customType := range b.messageRenderers {
		customType := customType
		_ = registry.RegisterMessageRenderer(customType, func(value any, options RenderOptions) (any, bool) {
			var result any
			if err := b.call(context.Background(), "render", customType, value, &result); err != nil {
				return value, false
			}
			return result, true
		})
	}
	for customType := range b.entryRenderers {
		customType := customType
		_ = registry.RegisterEntryRenderer(customType, func(value any, options RenderOptions) (any, bool) {
			var result any
			if err := b.call(context.Background(), "render", "entry:"+customType, value, &result); err != nil {
				return value, false
			}
			return result, true
		})
	}
}

func isUnder(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func isDisabled(path string, roots map[string]bool) bool {
	for root := range roots {
		if isUnder(path, root) {
			return true
		}
	}
	return false
}
func discover(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var result []string
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			result = append(result, extensionEntries(path)...)
		} else if strings.HasSuffix(entry.Name(), ".ts") || strings.HasSuffix(entry.Name(), ".js") {
			result = append(result, path)
		}
	}
	sort.Strings(result)
	return result
}
func extensionEntries(dir string) []string {
	if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		var pkg struct {
			Theoses struct {
				Extensions []string `json:"extensions"`
			} `json:"theoses"`
		}
		if json.Unmarshal(data, &pkg) == nil && len(pkg.Theoses.Extensions) > 0 {
			var result []string
			for _, name := range pkg.Theoses.Extensions {
				path := filepath.Join(dir, name)
				if _, err := os.Stat(path); err == nil {
					result = append(result, path)
				}
			}
			if len(result) > 0 {
				return result
			}
		}
	}
	for _, name := range []string{"index.ts", "index.js"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return []string{path}
		}
	}
	return nil
}

type bridge struct {
	name                             string
	commands                         []struct{ Name, Description string }
	messageRenderers, entryRenderers map[string]bool
	in                               io.WriteCloser
	out                              *bufio.Reader
	cmd                              *exec.Cmd
	mu                               sync.Mutex
}

func startBridge(path string) (*bridge, error) {
	cmd := exec.Command("node", "--input-type=module", "-e", bridgeScript, path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	b := &bridge{in: stdin, out: bufio.NewReader(stdout), cmd: cmd, messageRenderers: map[string]bool{}, entryRenderers: map[string]bool{}}
	var hello struct {
		Name             string                               `json:"name"`
		Commands         []struct{ Name, Description string } `json:"commands"`
		MessageRenderers []string                             `json:"messageRenderers"`
		EntryRenderers   []string                             `json:"entryRenderers"`
	}
	if err := b.read(&hello); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("load extension: %w", err)
	}
	b.name, b.commands = hello.Name, hello.Commands
	for _, name := range hello.MessageRenderers {
		b.messageRenderers[name] = true
	}
	for _, name := range hello.EntryRenderers {
		b.entryRenderers[name] = true
	}
	return b, nil
}
func (b *bridge) read(value any) error {
	line, err := b.out.ReadBytes('\n')
	if err != nil {
		return err
	}
	return json.Unmarshal(line, value)
}
func (b *bridge) call(ctx context.Context, kind, name string, payload, result any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	request, _ := json.Marshal(map[string]any{"kind": kind, "name": name, "payload": payload})
	if _, err := b.in.Write(append(request, '\n')); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() {
		var response struct {
			Error  string          `json:"error"`
			Result json.RawMessage `json:"result"`
		}
		if err := b.read(&response); err != nil {
			done <- err
		} else if response.Error != "" {
			done <- errors.New(response.Error)
		} else if result != nil {
			done <- json.Unmarshal(response.Result, result)
		} else {
			done <- nil
		}
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (b *bridge) close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	_ = b.in.Close()
	return b.cmd.Wait()
}

const bridgeScript = `import readline from "node:readline"; import { pathToFileURL } from "node:url";
const mod = await import(pathToFileURL(process.argv[1]).href); const factory = mod.default ?? mod;
const handlers = new Map(), commands = [], commandHandlers = new Map(), renderers = new Map(); const api = { on: (name, fn) => { const list = handlers.get(name) ?? []; list.push(fn); handlers.set(name, list); }, registerCommand: (name, opts) => { commands.push({name, description: opts?.description ?? ""}); commandHandlers.set(name, opts?.handler); }, registerMessageRenderer: (name, fn) => renderers.set("message:" + name, fn), registerEntryRenderer: (name, fn) => renderers.set("entry:" + name, fn), registerTool: () => {} };
await factory(api); console.log(JSON.stringify({name: process.argv[1], commands, messageRenderers: [...renderers.keys()].filter(k => k.startsWith("message:")).map(k => k.slice(8)), entryRenderers: [...renderers.keys()].filter(k => k.startsWith("entry:")).map(k => k.slice(6))}));
const rl = readline.createInterface({input: process.stdin}); for await (const line of rl) { try { const req = JSON.parse(line); let value = req.payload; if (req.kind === "render") { const fn = renderers.get(req.name === "entry:" + req.name ? req.name : "message:" + req.name) ?? renderers.get(req.name); value = fn ? await fn(value, {}) : value; } else if (req.kind === "command") { const fn = commandHandlers.get(req.name); value = fn ? await fn(value, {}) : ""; } else { for (const fn of handlers.get(req.name) ?? []) { const next = await fn({type:req.name, ...value}); if (next !== undefined) value = next; } if (req.name === "before_provider_headers") value = value.headers; } console.log(JSON.stringify({result:value})); } catch (e) { console.log(JSON.stringify({error:String(e?.message ?? e)})); } }`
