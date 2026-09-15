package codingagent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const operationalNotesMaxBytes = 32 * 1024

func appendOperationalNote(path, section, content string) (string, error) {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if len(existing) > operationalNotesMaxBytes {
		return fmt.Sprintf("operational-notes.md is full (32KB cap). Promote durable items to save_note, then trim %s.", path), nil
	}
	stamp := time.Now().UTC().Format("2006-01-02 15:04")
	entry := fmt.Sprintf("- %s | %s", stamp, content)
	text := string(existing)
	if strings.Contains(text, entry) {
		return fmt.Sprintf("operational-notes.md already contains [%s]: %s", section, content), nil
	}
	header := "## " + section
	if !strings.Contains(text, header) {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += "\n" + header + "\n"
	}
	lines := strings.Split(text, "\n")
	index := 0
	for i, line := range lines {
		if line == header {
			index = i + 1
			break
		}
	}
	for index < len(lines) && !strings.HasPrefix(lines[index], "## ") {
		index++
	}
	lines = append(lines[:index], append([]string{entry}, lines[index:]...)...)
	text = strings.TrimSpace(strings.Join(lines, "\n")) + "\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return "", err
	}
	return fmt.Sprintf("Added to operational notes [%s]: %s (%s).", section, content, path), nil
}
