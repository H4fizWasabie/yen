package extensions

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
)

func TestDiscoverAndLoadTypeScriptExtensionsIntoRegistry(t *testing.T) {
	workspace := t.TempDir()
	local := filepath.Join(workspace, ".theoses", "extensions")
	globalRoot := t.TempDir()
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	good := filepath.Join(local, "good.ts")
	write(good, `export default (api: any) => { api.on("before_provider_request", (event: any) => ({ messages: [...event.messages, { Role: "system", Content: "extension" }] })); api.on("tool_call", (event: any) => event.toolCall.Name === "blocked" ? ({ block: true, reason: "blocked by extension" }) : undefined); api.registerCommand("hello", { description: "hello", handler: async () => { await api.confirm("Continue?", "yes"); } }); api.registerMessageRenderer("text", (value: any) => "rendered:" + value); };`)
	write(filepath.Join(local, "broken.js"), `module.exports = () => { throw new Error("broken"); };`)
	global := filepath.Join(globalRoot, "extensions")
	if err := os.MkdirAll(global, 0o755); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(global, "global.js"), `module.exports = api => {};`)
	t.Setenv("YEN_TRUST_PROJECT", "1")
	loaded, err := DiscoverAndLoad(workspace, globalRoot, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Extensions) != 2 || len(loaded.Errors) != 1 {
		t.Fatalf("extensions=%#v errors=%#v", loaded.Extensions, loaded.Errors)
	}
	if len(loaded.Registry.Commands()) != 1 || loaded.Registry.Commands()[0].Name != "hello" {
		t.Fatalf("commands=%#v", loaded.Registry.Commands())
	}
	hooks := loaded.Registry.AgentHooks(nil)
	messages, err := hooks.ProviderBefore(context.Background(), []agent.Message{}, []string{})
	if err != nil || len(messages) != 1 || messages[0].Content != "extension" {
		t.Fatalf("provider hook=%#v err=%v", messages, err)
	}
	block, reason, err := hooks.Before(context.Background(), agent.Message{}, agent.ToolCall{Name: "blocked"})
	if err != nil || !block || reason != "blocked by extension" {
		t.Fatalf("tool hook=%v %q %v", block, reason, err)
	}
	renderer, ok := loaded.Registry.MessageRenderer("text")
	if !ok {
		t.Fatal("message renderer missing")
	}
	value, ok := renderer("hello", RenderOptions{})
	if !ok || value != "rendered:hello" {
		t.Fatalf("rendered=%#v ok=%v", value, ok)
	}
	uiCalled := false
	loaded.SetUIRequester(func(_ context.Context, request map[string]any) (map[string]any, error) {
		uiCalled = request["method"] == "confirm"
		return map[string]any{"id": request["id"], "confirmed": true}, nil
	})
	if err := loaded.Registry.Commands()[0].Handler(context.Background(), ""); err != nil || !uiCalled {
		t.Fatalf("extension command UI err=%v called=%v", err, uiCalled)
	}
	loaded.Close()
	t.Setenv("YEN_TRUST_PROJECT", "0")
	untrusted, err := DiscoverAndLoad(workspace, globalRoot, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer untrusted.Close()
	if len(untrusted.Extensions) != 1 || untrusted.Extensions[0].Path != filepath.Join(global, "global.js") {
		t.Fatalf("untrusted extensions=%#v", untrusted.Extensions)
	}
}

func TestConfiguredWorkspaceExtensionRequiresTrust(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "committed.ts")
	if err := os.WriteFile(path, []byte(`export default (api: any) => api.registerCommand("unsafe", {});`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YEN_TRUST_PROJECT", "0")
	untrusted, err := DiscoverAndLoadWithOperatorPaths(workspace, t.TempDir(), []string{path}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer untrusted.Close()
	if len(untrusted.Extensions) != 0 {
		t.Fatalf("untrusted extensions=%#v", untrusted.Extensions)
	}
	operator, err := DiscoverAndLoadWithOperatorPaths(workspace, t.TempDir(), nil, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	defer operator.Close()
	if len(operator.Extensions) != 1 || operator.Extensions[0].Path != path {
		t.Fatalf("operator extensions=%#v", operator.Extensions)
	}
}
