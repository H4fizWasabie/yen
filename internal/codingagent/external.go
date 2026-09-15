package codingagent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/H4fizWasabie/yen/internal/agent"
)

type externalTool struct {
	name        string
	description string
	schema      map[string]any
	execute     func(context.Context, map[string]any) (string, error)
	close       func() error
}

func (t externalTool) Name() string { return t.name }

func (t externalTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	return t.execute(ctx, args)
}

func (t externalTool) Close() error {
	if t.close == nil {
		return nil
	}
	return t.close()
}

// CloseTools releases optional resources owned by external tools. Built-in
// tools are unaffected; cleanup is best-effort so one failed close does not
// hide a completed agent turn.
func CloseTools(tools []agent.Tool) error {
	var first error
	for _, tool := range tools {
		if closer, ok := tool.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

type externalCatalogEntry struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Schema      map[string]any `json:"schema"`
	InputSchema map[string]any `json:"inputSchema"`
}

func loadExternalTools() []agent.Tool {
	var result []agent.Tool
	if url := strings.TrimRight(os.Getenv("YEN_HTTP_SIDECAR_URL"), "/"); url != "" {
		result = append(result, loadHTTPSidecar(url)...)
	}
	if url := strings.TrimSpace(os.Getenv("YEN_MCP_HTTP_URL")); url != "" {
		result = append(result, loadMCPHTTP(url)...)
	}
	if command := strings.TrimSpace(os.Getenv("YEN_MCP_STDIO_COMMAND")); command != "" {
		result = append(result, loadMCPStdio(command, strings.Fields(os.Getenv("YEN_MCP_STDIO_ARGS")))...)
	}
	return result
}

type deferredExternalTool struct {
	name  string
	tools []agent.Tool
}

func (t deferredExternalTool) Name() string { return t.name }

func (t deferredExternalTool) Close() error { return CloseTools(t.tools) }

func (t deferredExternalTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.name == "tool_search" {
		query, _ := args["query"].(string)
		query = strings.ToLower(strings.TrimSpace(query))
		var matches []string
		for _, tool := range t.tools {
			if query == "" || strings.Contains(strings.ToLower(tool.Name()), query) {
				matches = append(matches, tool.Name())
			}
		}
		return strings.Join(matches, "\n"), nil
	}
	name, _ := args["name"].(string)
	toolArgs, _ := args["args"].(map[string]any)
	for _, tool := range t.tools {
		if tool.Name() == name {
			return tool.Execute(ctx, toolArgs)
		}
	}
	return "", fmt.Errorf("unknown deferred tool %q", name)
}

func deferExternalTools(tools []agent.Tool) []agent.Tool {
	if len(tools) == 0 {
		return nil
	}
	return []agent.Tool{deferredExternalTool{name: "tool_search", tools: tools}, deferredExternalTool{name: "tool_call", tools: tools}}
}

func loadHTTPSidecar(baseURL string) []agent.Tool {
	client := &http.Client{}
	headers := externalHeaders()
	var catalog any
	if !getJSON(client, baseURL+"/tools", headers, &catalog) {
		return nil
	}
	entries := parseExternalCatalog(catalog)
	result := make([]agent.Tool, 0, len(entries))
	for _, entry := range entries {
		if entry.Name == "" {
			continue
		}
		name := entry.Name
		description := entry.Description
		result = append(result, externalTool{name: name, description: description, schema: externalSchema(entry), execute: func(ctx context.Context, args map[string]any) (string, error) {
			return executeSidecar(ctx, client, baseURL, headers, name, args)
		}})
	}
	return result
}

func executeSidecar(ctx context.Context, client *http.Client, baseURL string, headers map[string]string, name string, args map[string]any) (string, error) {
	body, err := json.Marshal(map[string]any{"tool": name, "args": args})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/execute", strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 16<<10))
		return "", fmt.Errorf("sidecar returned %s: %s", response.Status, strings.TrimSpace(string(message)))
	}
	var value any
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&value); err != nil {
		return "", err
	}
	if object, ok := value.(map[string]any); ok {
		if message, ok := object["error"].(string); ok && message != "" {
			return "", fmt.Errorf("sidecar: %s", message)
		}
		if result, ok := object["result"]; ok {
			value = result
		}
	}
	encoded, _ := json.Marshal(value)
	return "[UNTRUSTED EXTERNAL CONTENT]\n" + string(encoded), nil
}

type mcpClient struct {
	client    *http.Client
	url       string
	headers   map[string]string
	mu        sync.Mutex
	nextID    atomic.Int64
	sessionID string
}

func loadMCPHTTP(endpoint string) []agent.Tool {
	client := &mcpClient{client: &http.Client{}, url: endpoint, headers: externalHeaders()}
	if _, err := client.request(context.Background(), "initialize", map[string]any{
		"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]string{"name": "yen", "version": "0.1"},
	}); err != nil {
		return nil
	}
	if _, err := client.notify(context.Background(), "notifications/initialized", map[string]any{}); err != nil {
		return nil
	}
	value, err := client.request(context.Background(), "tools/list", map[string]any{})
	if err != nil {
		return nil
	}
	result := make([]agent.Tool, 0)
	for _, entry := range parseExternalCatalog(value) {
		if entry.Name == "" {
			continue
		}
		name := entry.Name
		result = append(result, externalTool{name: name, description: entry.Description, schema: externalSchema(entry), execute: func(ctx context.Context, args map[string]any) (string, error) {
			value, err := client.request(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
			if err != nil {
				return "", err
			}
			encoded, _ := json.Marshal(value)
			return "[UNTRUSTED EXTERNAL CONTENT]\n" + string(encoded), nil
		}})
	}
	return result
}

type mcpStdioClient struct {
	command string
	args    []string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	mu      sync.Mutex
	nextID  int64
}

func (c *mcpStdioClient) closeLocked() error {
	if c.cmd == nil {
		return nil
	}
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	err := c.cmd.Wait()
	c.cmd, c.stdin = nil, nil
	return err
}

func (c *mcpStdioClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeLocked()
}

func loadMCPStdio(command string, args []string) []agent.Tool {
	client := &mcpStdioClient{command: command, args: args}
	if _, err := client.request(context.Background(), "initialize", map[string]any{
		"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]string{"name": "yen", "version": "0.1"},
	}); err != nil {
		_ = client.Close()
		return nil
	}
	if err := client.notify("notifications/initialized", map[string]any{}); err != nil {
		_ = client.Close()
		return nil
	}
	value, err := client.request(context.Background(), "tools/list", map[string]any{})
	if err != nil {
		_ = client.Close()
		return nil
	}
	result := make([]agent.Tool, 0)
	for _, entry := range parseExternalCatalog(value) {
		if entry.Name == "" {
			continue
		}
		name := entry.Name
		result = append(result, externalTool{name: name, description: entry.Description, schema: externalSchema(entry), close: client.Close, execute: func(ctx context.Context, args map[string]any) (string, error) {
			value, err := client.request(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
			if err != nil {
				return "", err
			}
			encoded, _ := json.Marshal(value)
			return "[UNTRUSTED EXTERNAL CONTENT]\n" + string(encoded), nil
		}})
	}
	return result
}

func (c *mcpStdioClient) start() error {
	if c.cmd != nil {
		return nil
	}
	c.cmd = exec.Command(c.command, c.args...)
	c.cmd.Stderr = io.Discard
	stdin, err := c.cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := c.cmd.Start(); err != nil {
		return err
	}
	c.stdin = stdin
	c.scanner = bufio.NewScanner(stdout)
	c.scanner.Buffer(make([]byte, 4096), 4<<20)
	return nil
}

func (c *mcpStdioClient) request(ctx context.Context, method string, params map[string]any) (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.start(); err != nil {
		return nil, err
	}
	c.nextID++
	id := c.nextID
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(c.stdin, "%s\n", payload); err != nil {
		return nil, err
	}
	responses := make(chan struct {
		value any
		err   error
	}, 1)
	go func() {
		for c.scanner.Scan() {
			var message struct {
				ID     float64 `json:"id"`
				Result any     `json:"result"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(c.scanner.Bytes(), &message) != nil || int64(message.ID) != id {
				continue
			}
			if message.Error != nil {
				responses <- struct {
					value any
					err   error
				}{err: fmt.Errorf("MCP stdio: %s", message.Error.Message)}
				return
			}
			responses <- struct {
				value any
				err   error
			}{value: message.Result}
			return
		}
		if err := c.scanner.Err(); err != nil {
			responses <- struct {
				value any
				err   error
			}{err: err}
			return
		}
		responses <- struct {
			value any
			err   error
		}{err: errors.New("MCP stdio process closed")}
	}()
	select {
	case response := <-responses:
		return response.value, response.err
	case <-ctx.Done():
		_ = c.closeLocked()
		return nil, ctx.Err()
	}
}

func (c *mcpStdioClient) notify(method string, params map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.start(); err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(c.stdin, "%s\n", payload)
	return err
}

func (c *mcpClient) request(ctx context.Context, method string, params map[string]any) (any, error) {
	c.mu.Lock()
	id := c.nextID.Add(1)
	requestBody, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	sessionID := c.sessionID
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, strings.NewReader(string(requestBody)))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		request.Header.Set("Mcp-Session-Id", sessionID)
	}
	for key, value := range c.headers {
		request.Header.Set(key, value)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("MCP returned %s", response.Status)
	}
	if value := response.Header.Get("Mcp-Session-Id"); value != "" {
		c.mu.Lock()
		c.sessionID = value
		c.mu.Unlock()
	}
	var message struct {
		Result any `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&message); err != nil {
		return nil, err
	}
	if message.Error != nil {
		return nil, errors.New(message.Error.Message)
	}
	return message.Result, nil
}

func (c *mcpClient) notify(ctx context.Context, method string, params map[string]any) (any, error) {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("MCP returned %s", response.Status)
	}
	return nil, nil
}

func externalHeaders() map[string]string {
	if token := os.Getenv("YEN_EXTERNAL_TOOL_TOKEN"); token != "" {
		return map[string]string{"Authorization": "Bearer " + token}
	}
	return nil
}

func getJSON(client *http.Client, endpoint string, headers map[string]string, target any) bool {
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode >= 200 && response.StatusCode < 300 && json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(target) == nil
}

func parseExternalCatalog(value any) []externalCatalogEntry {
	if object, ok := value.(map[string]any); ok {
		value = object["tools"]
	}
	encoded, _ := json.Marshal(value)
	var entries []externalCatalogEntry
	_ = json.Unmarshal(encoded, &entries)
	return entries
}

func externalSchema(entry externalCatalogEntry) map[string]any {
	if entry.InputSchema != nil {
		return entry.InputSchema
	}
	if entry.Schema != nil {
		return entry.Schema
	}
	return map[string]any{"type": "object"}
}
