package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Engine struct {
	Semantic    *Store
	Episodic    *EpisodicStore
	Checkpoints *Checkpoints
	mu          sync.Mutex
	active      map[string]bool
}

type ConsolidatedFact struct {
	ID      string
	Subject string
	Body    string
	At      string
}

type ConsolidatedEdge struct {
	From string
	To   string
	Rel  string
}

type ConsolidatedEpisode struct {
	Summary                string
	StartedAt              string
	EndedAt                string
	RelatedSemanticNodeIDs []string
}

func OpenEngine(dir string) (*Engine, error) {
	episodic, err := OpenEpisodicStore(filepath.Join(dir, "episodes.db"))
	if err != nil {
		return nil, err
	}
	checkpoints, err := OpenCheckpoints(filepath.Join(dir, "consolidation-checkpoints.json"))
	if err != nil {
		_ = episodic.Close()
		return nil, err
	}
	return NewEngine(NewStore(filepath.Join(dir, "semantic")), episodic, checkpoints), nil
}

func NewEngine(semantic *Store, episodic *EpisodicStore, checkpoints *Checkpoints) *Engine {
	return &Engine{Semantic: semantic, Episodic: episodic, Checkpoints: checkpoints, active: make(map[string]bool)}
}

func (e *Engine) SaveNote(text string, ctx Context) (Node, error) {
	if e == nil || e.Semantic == nil {
		return Node{}, errors.New("semantic memory is not configured")
	}
	node := NewNode(text)
	node.OwnerID, node.WorkspaceID, node.ConversationID = ctx.OwnerID, ctx.WorkspaceID, ctx.ConversationID
	switch {
	case ctx.OwnerID != "":
		node.Scope = ScopeOwner
	case ctx.WorkspaceID != "":
		node.Scope = ScopeWorkspace
	default:
		node.Scope = ScopeEngine
	}
	if err := e.Semantic.Write(node); err != nil {
		return Node{}, err
	}
	return node, nil
}

func (e *Engine) Remember(query string, ctx Context) ([]Node, error) {
	if e == nil || e.Semantic == nil {
		return nil, errors.New("semantic memory is not configured")
	}
	return e.Semantic.Remember(query, ctx)
}

func (e *Engine) RecordTurn(turnID, conversationID, workspaceID, adapter, prompt, response string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return e.withConversation(conversationID, func() error {
		return e.applyConsolidation(turnID, conversationID, workspaceID, adapter, nil, nil, ConsolidatedEpisode{Summary: prompt + " -> " + response, StartedAt: now, EndedAt: now})
	})
}

func (e *Engine) ApplyConsolidation(turnID, conversationID, workspaceID, adapter string, facts []ConsolidatedFact, edges []ConsolidatedEdge, episode ConsolidatedEpisode) error {
	return e.withConversation(conversationID, func() error {
		return e.applyConsolidation(turnID, conversationID, workspaceID, adapter, facts, edges, episode)
	})
}

func (e *Engine) withConversation(conversationID string, fn func() error) error {
	if e == nil || e.Semantic == nil || e.Episodic == nil || e.Checkpoints == nil {
		return errors.New("memory engine is not configured")
	}
	e.mu.Lock()
	if e.active[conversationID] {
		e.mu.Unlock()
		return errors.New("memory consolidation already active")
	}
	e.active[conversationID] = true
	e.mu.Unlock()
	defer func() { e.mu.Lock(); delete(e.active, conversationID); e.mu.Unlock() }()
	lock, err := e.lockConversation(conversationID)
	if err != nil {
		return err
	}
	defer lock()
	return fn()
}

func (e *Engine) applyConsolidation(turnID, conversationID, workspaceID, adapter string, facts []ConsolidatedFact, edges []ConsolidatedEdge, episode ConsolidatedEpisode) error {
	ids := make(map[string]string, len(facts))
	nodes := make(map[string]Node, len(facts))
	for _, fact := range facts {
		if strings.TrimSpace(fact.Subject) == "" {
			continue
		}
		localID := fact.ID
		digest := sha256.Sum256([]byte(conversationID + "\x00" + turnID + "\x00" + localID + "\x00" + fact.Subject))
		node := Node{ID: "fact-" + hex.EncodeToString(digest[:8]), Type: "semantic", Subject: strings.TrimSpace(fact.Subject), Body: fact.Body, At: fact.At, Scope: ScopeWorkspace, WorkspaceID: workspaceID, ConversationID: conversationID, Channel: adapter, TurnID: turnID}
		if workspaceID == "" {
			node.Scope = ScopeEngine
		}
		if node.At == "" {
			node.At = time.Now().UTC().Format(time.RFC3339Nano)
		}
		if localID != "" {
			ids[localID] = node.ID
		}
		nodes[node.ID] = node
	}
	for _, edge := range edges {
		from, fromNew := ids[edge.From]
		if !fromNew && safeID(edge.From) {
			from = edge.From
		}
		to, toNew := ids[edge.To]
		if !toNew && safeID(edge.To) {
			to = edge.To
		}
		if from == "" || to == "" || edge.Rel == "" {
			continue
		}
		if node, ok := nodes[from]; ok {
			node.Edges = appendUniqueEdge(node.Edges, Edge{Target: to, Rel: edge.Rel})
			nodes[from] = node
			continue
		}
		node, ok, err := e.Semantic.Get(from)
		if err != nil {
			return err
		}
		if ok {
			node.Edges = appendUniqueEdge(node.Edges, Edge{Target: to, Rel: edge.Rel})
			if err := e.Semantic.Write(node); err != nil {
				return err
			}
		}
	}
	for _, node := range nodes {
		if err := e.Semantic.Write(node); err != nil {
			return err
		}
	}
	if episode.StartedAt == "" {
		episode.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if episode.EndedAt == "" {
		episode.EndedAt = episode.StartedAt
	}
	for index, id := range episode.RelatedSemanticNodeIDs {
		if mapped, ok := ids[id]; ok {
			episode.RelatedSemanticNodeIDs[index] = mapped
		}
	}
	if err := e.Episodic.Record(Episode{ID: "turn-" + turnID, StartedAt: episode.StartedAt, EndedAt: episode.EndedAt, Summary: episode.Summary, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), RelatedSemanticNodeIDs: episode.RelatedSemanticNodeIDs, ConversationID: conversationID, WorkspaceID: workspaceID, Channel: adapter, TurnID: turnID}); err != nil {
		return err
	}
	return e.Checkpoints.Set(conversationID, Checkpoint{LastEntryID: turnID})
}

func appendUniqueEdge(edges []Edge, edge Edge) []Edge {
	for _, existing := range edges {
		if existing == edge {
			return edges
		}
	}
	return append(edges, edge)
}

func (e *Engine) lockConversation(conversationID string) (func(), error) {
	digest := sha256.Sum256([]byte(conversationID))
	path := e.Checkpoints.path + "." + hex.EncodeToString(digest[:]) + ".lock"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}

func (e *Engine) Close() error {
	if e == nil || e.Episodic == nil {
		return nil
	}
	return e.Episodic.Close()
}
