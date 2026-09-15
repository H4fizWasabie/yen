package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type Settings struct {
	Provider       string   `json:"provider,omitempty"`
	Model          string   `json:"model,omitempty"`
	BaseURL        string   `json:"baseUrl,omitempty"`
	Reasoning      string   `json:"reasoningEffort,omitempty"`
	ContextFiles   []string `json:"contextFiles,omitempty"`
	SkillDirs      []string `json:"skillDirs,omitempty"`
	Trusted        *bool    `json:"trusted,omitempty"`
	AutoCompaction *bool    `json:"autoCompaction,omitempty"`
}

func Load(workspace string) (Settings, error) {
	var result Settings
	paths := []string{}
	if path := os.Getenv("YEN_SETTINGS_FILE"); path != "" {
		paths = append(paths, path)
	}
	if configDir, err := os.UserConfigDir(); err == nil {
		paths = append(paths, filepath.Join(configDir, "yen", "settings.json"))
	}
	paths = append(paths, filepath.Join(workspace, ".theoses", "settings.json"))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return result, err
		}
		var current Settings
		if err := json.Unmarshal(data, &current); err != nil {
			return result, err
		}
		merge(&result, current)
	}
	return result, nil
}

func TrustPath(workspace string) string {
	if configDir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(configDir, "yen", "trusted-projects.json")
	}
	return filepath.Join(workspace, ".theoses", "trusted-projects.json")
}

func IsTrusted(workspace string) bool {
	if os.Getenv("YEN_TRUST_PROJECT") == "1" {
		return true
	}
	settings, err := Load(workspace)
	if err == nil && settings.Trusted != nil {
		return *settings.Trusted
	}
	data, err := os.ReadFile(TrustPath(workspace))
	if err != nil {
		return false
	}
	var projects map[string]bool
	return json.Unmarshal(data, &projects) == nil && projects[workspace]
}

func Trust(workspace string) error {
	path := TrustPath(workspace)
	data, err := os.ReadFile(path)
	projects := make(map[string]bool)
	if err == nil {
		_ = json.Unmarshal(data, &projects)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	projects[workspace] = true
	encoded, err := json.MarshalIndent(projects, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o600)
}

func merge(target *Settings, source Settings) {
	if source.Provider != "" {
		target.Provider = source.Provider
	}
	if source.Model != "" {
		target.Model = source.Model
	}
	if source.BaseURL != "" {
		target.BaseURL = source.BaseURL
	}
	if source.Reasoning != "" {
		target.Reasoning = source.Reasoning
	}
	if source.ContextFiles != nil {
		target.ContextFiles = source.ContextFiles
	}
	if source.SkillDirs != nil {
		target.SkillDirs = source.SkillDirs
	}
	if source.Trusted != nil {
		target.Trusted = source.Trusted
	}
	if source.AutoCompaction != nil {
		target.AutoCompaction = source.AutoCompaction
	}
}
