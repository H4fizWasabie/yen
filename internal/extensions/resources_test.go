package extensions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResourceLoaderKeepsPrecedenceAndReportsConfiguredMisses(t *testing.T) {
	workspace, agentDir := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, ".theoses", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(agentDir, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"skillDirs":["configured-skills","missing-skills"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspace, "configured-skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YEN_SETTINGS_FILE", settingsPath)
	t.Setenv("YEN_TRUST_PROJECT", "1")
	loader := NewResourceLoader(workspace, agentDir)
	if err := loader.Reload(); err != nil {
		t.Fatal(err)
	}
	resources := loader.Resources(ResourceSkills)
	if len(resources) != 3 || resources[0].Scope != "project" || resources[1].Scope != "user" || resources[2].Source != "configured" {
		t.Fatalf("resources=%#v", resources)
	}
	if len(loader.Diagnostics()) != 1 || loader.Diagnostics()[0].Message != "configured resource not found" {
		t.Fatalf("diagnostics=%#v", loader.Diagnostics())
	}
}
