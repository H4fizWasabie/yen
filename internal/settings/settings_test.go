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

func TestQueueModesDefaultAndValidation(t *testing.T) {
	steering, followUp := QueueModes(Settings{SteeringMode: "invalid", FollowUpMode: "all"})
	if steering != "one-at-a-time" || followUp != "all" {
		t.Fatalf("modes=%q,%q", steering, followUp)
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
