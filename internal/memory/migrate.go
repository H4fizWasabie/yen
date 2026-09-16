package memory

import (
	"bufio"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// MigrateSemantic copies existing Markdown nodes without changing the source.
// Scope is required because the old format has no ownership boundary.
func MigrateSemantic(sourceDir string, target *Store, scope Scope, ctx Context) (int, error) {
	if scope != ScopeOwner && scope != ScopeWorkspace && scope != ScopeConversation && scope != ScopeEngine {
		return 0, errors.New("explicit semantic migration scope is required")
	}
	info, err := os.Stat(sourceDir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return migrateLegacyJSONL(sourceDir, target, scope, ctx)
	}
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(sourceDir, entry.Name()))
		if err != nil {
			return count, err
		}
		node, err := parseNode(string(raw))
		if err != nil {
			continue
		}
		node.Scope, node.OwnerID, node.WorkspaceID, node.ConversationID = scope, ctx.OwnerID, ctx.WorkspaceID, ctx.ConversationID
		if err := target.Write(node); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

type legacyMemoryRecord struct {
	ID        string `json:"id"`
	CreatedAt string `json:"createdAt"`
	Text      string `json:"text"`
}

func migrateLegacyJSONL(sourcePath string, target *Store, scope Scope, ctx Context) (int, error) {
	file, err := os.Open(sourcePath)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	count := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	for scanner.Scan() {
		var legacy legacyMemoryRecord
		if json.Unmarshal(scanner.Bytes(), &legacy) != nil || strings.TrimSpace(legacy.Text) == "" {
			continue
		}
		node := NewNode(legacy.Text)
		if safeID(legacy.ID) {
			node.ID = legacy.ID
		} else {
			digest := sha256.Sum256([]byte(legacy.CreatedAt + "\x00" + legacy.Text))
			node.ID = "legacy-" + hex.EncodeToString(digest[:8])
		}
		if legacy.CreatedAt != "" {
			node.At = legacy.CreatedAt
		}
		node.Scope, node.OwnerID, node.WorkspaceID, node.ConversationID = scope, ctx.OwnerID, ctx.WorkspaceID, ctx.ConversationID
		if err := target.Write(node); err != nil {
			return count, err
		}
		count++
	}
	return count, scanner.Err()
}

// MigrateEpisodes copies the pinned legacy table and requires the caller to map
// channel-keyed history to one canonical conversation.
func MigrateEpisodes(sourcePath string, target *EpisodicStore, conversationID, workspaceID string) (int, error) {
	if strings.TrimSpace(conversationID) == "" {
		return 0, errors.New("conversation ID is required for episode migration")
	}
	db, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT * FROM episodes`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	count := 0
	columns, err := rows.Columns()
	if err != nil {
		return 0, err
	}
	indexes := make(map[string]int, len(columns))
	for index, column := range columns {
		indexes[strings.ToLower(column)] = index
	}
	for _, required := range []string{"id", "started_at", "ended_at", "summary", "created_at", "related_semantic_node_ids"} {
		if _, ok := indexes[required]; !ok {
			return 0, fmt.Errorf("episodes table is missing %s", required)
		}
	}
	for rows.Next() {
		var e Episode
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			return count, err
		}
		e.ID = migrationString(values[indexes["id"]])
		e.StartedAt = migrationString(values[indexes["started_at"]])
		e.EndedAt = migrationString(values[indexes["ended_at"]])
		e.Summary = migrationString(values[indexes["summary"]])
		e.CreatedAt = migrationString(values[indexes["created_at"]])
		related := migrationString(values[indexes["related_semantic_node_ids"]])
		_ = json.Unmarshal([]byte(related), &e.RelatedSemanticNodeIDs)
		e.ConversationID, e.WorkspaceID = conversationID, workspaceID
		if index, ok := indexes["workspace_id"]; ok && e.WorkspaceID == "" {
			e.WorkspaceID = migrationString(values[index])
		}
		if index, ok := indexes["channel"]; ok {
			e.Channel = migrationString(values[index])
		}
		if index, ok := indexes["turn_id"]; ok {
			e.TurnID = migrationString(values[index])
		}
		if err := target.Record(e); err != nil {
			return count, err
		}
		count++
	}
	return count, rows.Err()
}

func migrationString(value any) string {
	switch value := value.(type) {
	case nil:
		return ""
	case []byte:
		return string(value)
	default:
		return fmt.Sprint(value)
	}
}
