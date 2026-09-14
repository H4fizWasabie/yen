package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSemanticStoreWritesPinnedFrontMatterAndScopesRecall(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "semantic"))
	node := NewNode("User prefers concise replies")
	node.Scope, node.OwnerID = ScopeOwner, "owner-1"
	node.Body, node.Channel, node.TurnID = "stable preference", "cli", "turn-1"
	if err := store.Write(node); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "semantic", node.ID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw[:4]) != "---\n" {
		t.Fatalf("missing front matter: %q", raw)
	}
	got, ok, err := store.Get(node.ID)
	if err != nil || !ok {
		t.Fatalf("get = %#v, %v, %v", got, ok, err)
	}
	if got.Body != node.Body || got.Scope != ScopeOwner {
		t.Fatalf("round trip = %#v", got)
	}
	if hits, err := store.Remember("concise replies", Context{OwnerID: "other"}); err != nil || len(hits) != 0 {
		t.Fatalf("wrong owner recall = %v, %v", hits, err)
	}
	if hits, err := store.Remember("concise replies", Context{OwnerID: "owner-1"}); err != nil || len(hits) != 1 {
		t.Fatalf("owner recall = %v, %v", hits, err)
	}
}

func TestEpisodicStoreAndCheckpointSurviveReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "episodes.db")
	s, err := OpenEpisodicStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Record(Episode{ID: "episode-1", Summary: "discussed migration", ConversationID: "conv-1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenEpisodicStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	hits, err := s.Search("conv-1", "what migration", 8)
	if err != nil || len(hits) != 1 {
		t.Fatalf("episode search = %v, %v", hits, err)
	}
	if hits, err := s.AtTime("conv-1", "2026-01-01T12:00:00Z", 8); err != nil || len(hits) != 1 {
		t.Fatalf("episode at time = %v, %v", hits, err)
	}
	cp, err := OpenCheckpoints(filepath.Join(dir, "checkpoints.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := cp.Set("conv-1", Checkpoint{LastEntryID: "entry-4"}); err != nil {
		t.Fatal(err)
	}
	cp, err = OpenCheckpoints(filepath.Join(dir, "checkpoints.json"))
	if err != nil || cp.Get("conv-1").LastEntryID != "entry-4" {
		t.Fatalf("checkpoint = %#v, %v", cp.Get("conv-1"), err)
	}
}

func TestCheckpointsMergeWritesAcrossHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoints.json")
	first, err := OpenCheckpoints(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := OpenCheckpoints(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Set("conv-1", Checkpoint{LastEntryID: "turn-1"}); err != nil {
		t.Fatal(err)
	}
	if err := second.Set("conv-2", Checkpoint{LastEntryID: "turn-2"}); err != nil {
		t.Fatal(err)
	}
	if got := first.Get("conv-2"); got.LastEntryID != "turn-2" {
		t.Fatalf("first handle lost second write: %#v", got)
	}
	if got := second.Get("conv-1"); got.LastEntryID != "turn-1" {
		t.Fatalf("second handle lost first write: %#v", got)
	}
}

func TestSemanticRememberWalksEdgesAndHidesSupersededNodes(t *testing.T) {
	store := NewStore(t.TempDir())
	root := NewNode("project preference")
	root.ID = "root"
	child := NewNode("implementation detail")
	child.ID = "child"
	child.Edges = []Edge{{Target: root.ID, Rel: "depends_on"}}
	replacement := NewNode("new project preference")
	replacement.ID = "replacement"
	replacement.Edges = []Edge{{Target: root.ID, Rel: "supersedes"}}
	for _, node := range []Node{root, child, replacement} {
		if err := store.Write(node); err != nil {
			t.Fatal(err)
		}
	}
	hits, err := store.Remember("implementation", Context{})
	if err != nil || len(hits) != 2 {
		t.Fatalf("hits = %#v, %v", hits, err)
	}
	if hits[0].ID != "child" || hits[1].ID != "replacement" {
		t.Fatalf("edge hits = %#v", hits)
	}
	for _, hit := range hits {
		if hit.ID == "root" {
			t.Fatal("superseded root surfaced")
		}
	}
}

func TestSemanticRememberUsesPinnedStopwordsAndWordBoundaries(t *testing.T) {
	store := NewStore(t.TempDir())
	for _, node := range []Node{
		{ID: "preference", Subject: "User preference", Type: "semantic"},
		{ID: "superuser", Subject: "Superuser access", Type: "semantic"},
	} {
		if err := store.Write(node); err != nil {
			t.Fatal(err)
		}
	}
	hits, err := store.Remember("what do you know about my preference", Context{})
	if err != nil || len(hits) != 1 || hits[0].ID != "preference" {
		t.Fatalf("hits=%#v err=%v", hits, err)
	}
}
