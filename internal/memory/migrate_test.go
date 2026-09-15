package memory

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrationIsExplicitAdditiveAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	target := NewStore(filepath.Join(dir, "target"))
	node := NewNode("legacy preference")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, node.ID+".md"), []byte("---\nid: "+node.ID+"\ntype: semantic\nsubject: legacy preference\nat: 2026-01-01T00:00:00Z\nedges: []\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateSemantic(source, target, "", Context{}); err == nil {
		t.Fatal("missing scope accepted")
	}
	count, err := MigrateSemantic(source, target, ScopeOwner, Context{OwnerID: "owner-1"})
	if err != nil || count != 1 {
		t.Fatalf("semantic migration = %d, %v", count, err)
	}
	count, err = MigrateSemantic(source, target, ScopeOwner, Context{OwnerID: "owner-1"})
	if err != nil || count != 1 {
		t.Fatalf("repeat migration = %d, %v", count, err)
	}
	got, ok, err := target.Get(node.ID)
	if err != nil || !ok || got.OwnerID != "owner-1" {
		t.Fatalf("migrated node = %#v, %v, %v", got, ok, err)
	}

	legacyDB := filepath.Join(dir, "legacy.db")
	db, err := sql.Open("sqlite", legacyDB)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE episodes (id TEXT PRIMARY KEY, started_at TEXT NOT NULL, ended_at TEXT NOT NULL, summary TEXT NOT NULL, created_at TEXT NOT NULL, related_semantic_node_ids TEXT NOT NULL DEFAULT '[]'); INSERT INTO episodes VALUES ('episode-1','2026-01-01','2026-01-02','legacy episode','2026-01-02','[]')`)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	dst, err := OpenEpisodicStore(filepath.Join(dir, "new.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	count, err = MigrateEpisodes(legacyDB, dst, "conv-1", "work-1")
	if err != nil || count != 1 {
		t.Fatalf("episode migration = %d, %v", count, err)
	}
	count, err = MigrateEpisodes(legacyDB, dst, "conv-1", "work-1")
	if err != nil || count != 1 {
		t.Fatalf("repeat episode migration = %d, %v", count, err)
	}
	hits, err := dst.Recent("conv-1", 8)
	if err != nil || len(hits) != 1 {
		t.Fatalf("migrated episodes = %v, %v", hits, err)
	}
	richDB := filepath.Join(dir, "rich.db")
	rich, err := sql.Open("sqlite", richDB)
	if err != nil {
		t.Fatal(err)
	}
	_, err = rich.Exec(`CREATE TABLE episodes (id TEXT PRIMARY KEY, started_at TEXT, ended_at TEXT, summary TEXT, created_at TEXT, related_semantic_node_ids TEXT, workspace_id TEXT, conversation_id TEXT, channel TEXT, turn_id TEXT); INSERT INTO episodes VALUES ('episode-rich','2026-02-01','2026-02-02','rich episode','2026-02-02','["fact-1"]','legacy-work','legacy-conv','telegram','turn-1')`)
	if err != nil {
		t.Fatal(err)
	}
	_ = rich.Close()
	count, err = MigrateEpisodes(richDB, dst, "conv-2", "")
	if err != nil || count != 1 {
		t.Fatalf("rich episode migration = %d, %v", count, err)
	}
	hits, err = dst.Recent("conv-2", 8)
	if err != nil || len(hits) != 1 || hits[0].WorkspaceID != "legacy-work" || hits[0].Channel != "telegram" || hits[0].TurnID != "turn-1" {
		t.Fatalf("rich migrated episodes = %#v, %v", hits, err)
	}
}

func TestMigrationImportsLegacySemanticJSONLAdditively(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "memory.jsonl")
	raw := "{\"id\":\"legacy-1\",\"createdAt\":\"2026-01-01T00:00:00Z\",\"text\":\"legacy preference\"}\n{\"createdAt\":\"2026-01-02T00:00:00Z\",\"text\":\"legacy workspace\"}\nnot-json\n"
	if err := os.WriteFile(source, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	target := NewStore(filepath.Join(dir, "target"))
	count, err := MigrateSemantic(source, target, ScopeWorkspace, Context{WorkspaceID: "work-1"})
	if err != nil || count != 2 {
		t.Fatalf("migration count=%d err=%v", count, err)
	}
	node, ok, err := target.Get("legacy-1")
	if err != nil || !ok || node.Scope != ScopeWorkspace || node.WorkspaceID != "work-1" {
		t.Fatalf("node=%#v ok=%v err=%v", node, ok, err)
	}
	count, err = MigrateSemantic(source, target, ScopeWorkspace, Context{WorkspaceID: "work-1"})
	if err != nil || count != 2 {
		t.Fatalf("repeat count=%d err=%v", count, err)
	}
	if got, err := os.ReadFile(source); err != nil || string(got) != raw {
		t.Fatalf("source changed: %q err=%v", got, err)
	}
}
