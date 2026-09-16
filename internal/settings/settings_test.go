package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMergesGlobalAndProjectSettings(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global.json")
	workspace := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(filepath.Join(workspace, ".theoses"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(global, []byte(`{"model":"global-model","reasoningEffort":"low"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".theoses", "settings.json"), []byte(`{"model":"project-model","trusted":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YEN_SETTINGS_FILE", global)
	got, err := Load(workspace)
	if err != nil || got.Model != "project-model" || got.Reasoning != "low" || got.Trusted == nil || !*got.Trusted {
		t.Fatalf("settings=%#v err=%v", got, err)
	}
}

func TestLoadDeepMergesNestedSettings(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global.json")
	workspace := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(filepath.Join(workspace, ".theoses"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(global, []byte(`{"compaction":{"reserveTokens":1000,"keepRecentTokens":2000},"retry":{"maxRetries":2,"baseDelayMs":25}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".theoses", "settings.json"), []byte(`{"compaction":{"enabled":false,"keepRecentTokens":3000},"retry":{"enabled":false}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YEN_SETTINGS_FILE", global)
	got, err := Load(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if got.Compaction == nil || got.Compaction.ReserveTokens != 1000 || got.Compaction.KeepRecentTokens != 3000 || got.Compaction.Enabled == nil || *got.Compaction.Enabled {
		t.Fatalf("compaction=%#v", got.Compaction)
	}
	if got.Retry == nil || got.Retry.MaxRetries != 2 || got.Retry.BaseDelayMs != 25 || got.Retry.Enabled == nil || *got.Retry.Enabled {
		t.Fatalf("retry=%#v", got.Retry)
	}
}

func TestLoadPreservesExplicitZeroSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	t.Setenv("YEN_SETTINGS_FILE", path)
	if err := os.WriteFile(path, []byte(`{"compaction":{"maxHistoryTurns":0},"retry":{"provider":{"timeoutMs":0,"maxRetries":0,"maxRetryDelayMs":0}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil || got.Compaction == nil || got.Compaction.MaxHistoryTurns != 0 || got.Retry == nil || got.Retry.Provider == nil || !got.Retry.Provider.Has("timeoutMs") || !got.Retry.Provider.Has("maxRetries") || !got.Retry.Provider.Has("maxRetryDelayMs") {
		t.Fatalf("settings=%#v err=%v", got, err)
	}
}

func TestQueueModesDefaultAndValidation(t *testing.T) {
	steering, followUp := QueueModes(Settings{SteeringMode: "invalid", FollowUpMode: "all"})
	if steering != "one-at-a-time" || followUp != "all" {
		t.Fatalf("modes=%q,%q", steering, followUp)
	}
}

func TestLoadMigratesLegacyQueueMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	t.Setenv("YEN_SETTINGS_FILE", path)
	if err := os.WriteFile(path, []byte(`{"queueMode":"all"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil || got.SteeringMode != "all" {
		t.Fatalf("settings=%#v err=%v", got, err)
	}
}

func TestSaveRoundTripsQueueModes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	t.Setenv("YEN_SETTINGS_FILE", path)
	if err := Save(dir, Settings{SteeringMode: "all", FollowUpMode: "one-at-a-time"}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil || got.SteeringMode != "all" || got.FollowUpMode != "one-at-a-time" {
		t.Fatalf("settings=%#v err=%v", got, err)
	}
}

func TestTrustWritesScopedProjectDecision(t *testing.T) {
	dir := t.TempDir()
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	if err := Trust(dir); err != nil {
		t.Fatal(err)
	}
	if !IsTrusted(dir) {
		t.Fatal("project was not trusted")
	}
}
