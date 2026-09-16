package memory

import "testing"

func TestEngineRecordsCanonicalTurnBeforeCheckpoint(t *testing.T) {
	engine, err := OpenEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	if err := engine.RecordTurn("turn-1", "conv-1", "work-1", "telegram", "remember this", "done"); err != nil {
		t.Fatal(err)
	}
	hits, err := engine.Episodic.Search("conv-1", "remember", 8)
	if err != nil || len(hits) != 1 || hits[0].TurnID != "turn-1" {
		t.Fatalf("episodes=%#v err=%v", hits, err)
	}
	if engine.Checkpoints.Get("conv-1").LastEntryID != "turn-1" {
		t.Fatalf("checkpoint=%#v", engine.Checkpoints.Get("conv-1"))
	}
	node, err := engine.SaveNote("user prefers concise replies", Context{OwnerID: "owner-1", WorkspaceID: "work-1", ConversationID: "conv-1"})
	if err != nil || node.Scope != ScopeOwner {
		t.Fatalf("node=%#v err=%v", node, err)
	}
}

func TestEngineAppliesConsolidationFactsEdgesAndEpisodeIdempotently(t *testing.T) {
	engine, err := OpenEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	inputFacts := []ConsolidatedFact{
		{ID: "f1", Subject: "User prefers concise replies", Body: "Keep answers short."},
		{ID: "f2", Subject: "Theoses2 is the active project", Body: "The Go rewrite is called yen."},
	}
	inputEdges := []ConsolidatedEdge{{From: "f2", To: "f1", Rel: "depends_on"}, {From: "f1", To: "f2", Rel: "not-a-memory-relation"}}
	episode := ConsolidatedEpisode{Summary: "Captured project and response preferences.", StartedAt: "2026-01-01T00:00:00Z", EndedAt: "2026-01-01T00:01:00Z", RelatedSemanticNodeIDs: []string{"f1", "f2"}}
	if err := engine.ApplyConsolidation("turn-9", "conv-9", "work-9", "cli", inputFacts, inputEdges, episode); err != nil {
		t.Fatal(err)
	}
	if err := engine.ApplyConsolidation("turn-9", "conv-9", "work-9", "cli", inputFacts, inputEdges, episode); err != nil {
		t.Fatal(err)
	}
	hits, err := engine.Remember("concise replies", Context{WorkspaceID: "work-9", ConversationID: "other"})
	if err != nil || len(hits) != 2 {
		t.Fatalf("facts=%#v err=%v", hits, err)
	}
	seenPreference := false
	for _, hit := range hits {
		if hit.Subject == "User prefers concise replies" {
			seenPreference = true
		}
	}
	if !seenPreference {
		t.Fatalf("preference fact missing: %#v", hits)
	}
	nodes, err := engine.Semantic.List()
	if err != nil || len(nodes) != 2 || len(nodes[1].Edges) != 1 && len(nodes[0].Edges) != 1 {
		t.Fatalf("nodes=%#v err=%v", nodes, err)
	}
	for _, node := range nodes {
		for _, edge := range node.Edges {
			if edge.Rel == "not-a-memory-relation" {
				t.Fatalf("invalid edge persisted: %#v", node)
			}
		}
	}
	episodes, err := engine.Episodic.Recent("conv-9", 8)
	if err != nil || len(episodes) != 1 || len(episodes[0].RelatedSemanticNodeIDs) != 2 {
		t.Fatalf("episodes=%#v err=%v", episodes, err)
	}
	if got := engine.Checkpoints.Get("conv-9").LastEntryID; got != "turn-9" {
		t.Fatalf("checkpoint=%q", got)
	}
}

func TestSharedConversationConsolidationIsVisibleAcrossWorkspaces(t *testing.T) {
	engine, err := OpenEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	engine.ConversationScoped = true
	if err := engine.ApplyConsolidation("turn-shared", "conv-shared", "telegram-cwd", "telegram", []ConsolidatedFact{{ID: "f1", Subject: "Shared pilot fact", Body: "Visible across adapters."}}, nil, ConsolidatedEpisode{Summary: "shared", StartedAt: "2026-01-01T00:00:00Z", EndedAt: "2026-01-01T00:01:00Z"}); err != nil {
		t.Fatal(err)
	}
	hits, err := engine.Remember("shared pilot fact", Context{WorkspaceID: "dashboard-cwd", ConversationID: "conv-shared", ConversationScoped: true})
	if err != nil || len(hits) != 1 || hits[0].Scope != ScopeConversation {
		t.Fatalf("hits=%#v err=%v", hits, err)
	}
}

func TestEngineApplyConsolidationRejectsMissingEpisodeTimestamps(t *testing.T) {
	engine, err := OpenEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	err = engine.ApplyConsolidation("turn-missing-time", "conv-missing-time", "work", "cli", nil, nil, ConsolidatedEpisode{Summary: "missing"})
	if err == nil || err.Error() != "consolidation episode timestamps are required" {
		t.Fatalf("err=%v", err)
	}
	if got := engine.Checkpoints.Get("conv-missing-time").LastEntryID; got != "" {
		t.Fatalf("checkpoint=%q", got)
	}
}
