package conversation

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Link struct {
	Adapter        string `json:"adapter"`
	AdapterKey     string `json:"adapterKey"`
	ConversationID string `json:"conversationId"`
	WorkspaceID    string `json:"workspaceId,omitempty"`
	CreatedAt      string `json:"createdAt"`
}

type Registry struct {
	path  string
	mu    sync.Mutex
	links map[string]Link
}

func OpenRegistry(path string) (*Registry, error) {
	r := &Registry{path: path, links: make(map[string]Link)}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var link Link
		if json.Unmarshal(scanner.Bytes(), &link) == nil && validLink(link) {
			r.links[linkKey(link.Adapter, link.AdapterKey)] = link
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Registry) Resolve(adapter, adapterKey, workspaceID string) (Link, error) {
	if adapter == "" || adapterKey == "" {
		return Link{}, errors.New("adapter and adapter key are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var result Link
	err := r.withFileLock(func() error {
		if err := r.reload(); err != nil {
			return err
		}
		if link, ok := r.links[linkKey(adapter, adapterKey)]; ok {
			result = link
			return nil
		}
		result = Link{Adapter: adapter, AdapterKey: adapterKey, ConversationID: newID("conv"), WorkspaceID: workspaceID, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
		if err := r.append(result); err != nil {
			return err
		}
		r.links[linkKey(adapter, adapterKey)] = result
		return nil
	})
	return result, err
}

// ResolveShared binds an adapter identity to one durable conversation.
// The latest link wins, so an earlier adapter-local link is migrated additively.
func (r *Registry) ResolveShared(adapter, adapterKey, workspaceID, conversationID string) (Link, error) {
	if adapter == "" || adapterKey == "" || conversationID == "" {
		return Link{}, errors.New("adapter, adapter key, and conversation ID are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var result Link
	err := r.withFileLock(func() error {
		if err := r.reload(); err != nil {
			return err
		}
		if link, ok := r.links[linkKey(adapter, adapterKey)]; ok && link.ConversationID == conversationID {
			result = link
			return nil
		}
		result = Link{Adapter: adapter, AdapterKey: adapterKey, ConversationID: conversationID, WorkspaceID: workspaceID, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
		if err := r.append(result); err != nil {
			return err
		}
		r.links[linkKey(adapter, adapterKey)] = result
		return nil
	})
	return result, err
}

func (r *Registry) Link(adapter, adapterKey, conversationID, workspaceID string) (Link, error) {
	if adapter == "" || adapterKey == "" || conversationID == "" {
		return Link{}, errors.New("adapter, adapter key, and conversation ID are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	link := Link{Adapter: adapter, AdapterKey: adapterKey, ConversationID: conversationID, WorkspaceID: workspaceID, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	err := r.withFileLock(func() error {
		if err := r.reload(); err != nil {
			return err
		}
		if err := r.append(link); err != nil {
			return err
		}
		r.links[linkKey(adapter, adapterKey)] = link
		return nil
	})
	return link, err
}

func (r *Registry) Get(adapter, adapterKey string) (Link, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_ = r.withFileLock(r.reload)
	link, ok := r.links[linkKey(adapter, adapterKey)]
	return link, ok
}

func (r *Registry) FindConversation(conversationID string) (Link, bool) {
	return r.FindConversationFor("", conversationID)
}

func (r *Registry) FindConversationFor(adapter, conversationID string) (Link, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_ = r.withFileLock(r.reload)
	for _, link := range r.links {
		if link.ConversationID == conversationID && (adapter == "" || link.Adapter == adapter) {
			return link, true
		}
	}
	return Link{}, false
}

func (r *Registry) List(adapter string) []Link {
	r.mu.Lock()
	defer r.mu.Unlock()
	_ = r.withFileLock(r.reload)
	links := make([]Link, 0, len(r.links))
	for _, link := range r.links {
		if adapter == "" || link.Adapter == adapter {
			links = append(links, link)
		}
	}
	return links
}

func (r *Registry) reload() error {
	r.links = make(map[string]Link)
	file, err := os.Open(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var link Link
		if json.Unmarshal(scanner.Bytes(), &link) == nil && validLink(link) {
			r.links[linkKey(link.Adapter, link.AdapterKey)] = link
		}
	}
	return scanner.Err()
}

func (r *Registry) withFileLock(fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return err
	}
	lock, err := os.OpenFile(r.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := flock(lock); err != nil {
		return err
	}
	defer funlock(lock)
	return fn()
}

func (r *Registry) append(link Link) error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := json.Marshal(link)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = file.Write(data)
	return err
}

func validLink(link Link) bool {
	return link.Adapter != "" && link.AdapterKey != "" && link.ConversationID != ""
}

func linkKey(adapter, adapterKey string) string { return adapter + "\x00" + adapterKey }

type Turn struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversationId"`
	Adapter        string `json:"adapter"`
	AdapterKey     string `json:"adapterKey"`
	WorkspaceID    string `json:"workspaceId,omitempty"`
	Prompt         string `json:"prompt"`
	CreatedAt      string `json:"createdAt"`
	Status         string `json:"status"`
	LeaseOwner     string `json:"leaseOwner,omitempty"`
	LeaseUntil     string `json:"leaseUntil,omitempty"`
}

type queueEvent struct {
	Type   string `json:"type"`
	Turn   *Turn  `json:"turn,omitempty"`
	TurnID string `json:"turnId,omitempty"`
	Owner  string `json:"owner,omitempty"`
	Until  string `json:"until,omitempty"`
}

type Queue struct {
	path  string
	mu    sync.Mutex
	turns map[string]*Turn
	order []string
	owner string
}

const DefaultLeaseDuration = time.Minute

func OpenQueue(path string) (*Queue, error) {
	q := &Queue{path: path, turns: make(map[string]*Turn), owner: newID("worker")}
	if err := q.withFileLock(q.reload); err != nil {
		return nil, err
	}
	return q, nil
}

func (q *Queue) Enqueue(conversationID, adapter, adapterKey, workspaceID, prompt string) (Turn, error) {
	if conversationID == "" || prompt == "" {
		return Turn{}, errors.New("conversation ID and prompt are required")
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if err := q.withFileLock(q.reload); err != nil {
		return Turn{}, err
	}
	turn := Turn{ID: newID("turn"), ConversationID: conversationID, Adapter: adapter, AdapterKey: adapterKey, WorkspaceID: workspaceID, Prompt: prompt, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Status: "pending"}
	if err := q.write(queueEvent{Type: "enqueue", Turn: &turn}); err != nil {
		return Turn{}, err
	}
	q.turns[turn.ID] = &turn
	q.order = append(q.order, turn.ID)
	return turn, nil
}

func (q *Queue) Claim(conversationID string) (Turn, bool, error) {
	return q.ClaimFor(conversationID, q.owner, DefaultLeaseDuration)
}

func (q *Queue) ClaimFor(conversationID, owner string, lease time.Duration) (Turn, bool, error) {
	if owner == "" {
		return Turn{}, false, errors.New("queue owner is required")
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	var claimed Turn
	var found bool
	err := q.withFileLock(func() error {
		if err := q.reload(); err != nil {
			return err
		}
		busy := false
		for _, id := range q.order {
			turn := q.turns[id]
			if turn.ConversationID != conversationID || turn.Status != "active" {
				continue
			}
			if leaseExpired(turn) {
				if err := q.write(queueEvent{Type: "requeue", TurnID: id}); err != nil {
					return err
				}
				turn.Status, turn.LeaseOwner, turn.LeaseUntil = "pending", "", ""
			} else {
				busy = true
			}
		}
		if busy {
			return nil
		}
		for _, id := range q.order {
			turn := q.turns[id]
			if turn.ConversationID != conversationID || turn.Status != "pending" {
				continue
			}
			until := time.Now().UTC().Add(lease).Format(time.RFC3339Nano)
			if err := q.write(queueEvent{Type: "claim", TurnID: id, Owner: owner, Until: until}); err != nil {
				return err
			}
			turn.Status, turn.LeaseOwner, turn.LeaseUntil = "active", owner, until
			claimed, found = *turn, true
			break
		}
		return nil
	})
	return claimed, found, err
}

func (q *Queue) Complete(turnID string) error { return q.CompleteFor(turnID, q.owner) }

func (q *Queue) CompleteFor(turnID, owner string) error { return q.finish(turnID, "complete", owner) }

func (q *Queue) Cancel(turnID string) error { return q.CancelFor(turnID, q.owner) }

func (q *Queue) CancelFor(turnID, owner string) error { return q.finish(turnID, "canceled", owner) }

// CancelActive uses the durable lease owner so an adapter process can cancel
// a turn claimed by another runner process.
func (q *Queue) CancelActive(turnID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.withFileLock(func() error {
		if err := q.reload(); err != nil {
			return err
		}
		turn, ok := q.turns[turnID]
		if !ok {
			return fmt.Errorf("unknown turn: %s", turnID)
		}
		if turn.Status != "active" {
			return fmt.Errorf("turn %s is not active", turnID)
		}
		if err := q.write(queueEvent{Type: "canceled", TurnID: turnID, Owner: turn.LeaseOwner}); err != nil {
			return err
		}
		turn.Status, turn.LeaseOwner, turn.LeaseUntil = "canceled", "", ""
		return nil
	})
}

func (q *Queue) Renew(turnID string, lease time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.withFileLock(func() error {
		if err := q.reload(); err != nil {
			return err
		}
		turn := q.turns[turnID]
		if turn == nil || turn.Status != "active" || turn.LeaseOwner != q.owner {
			return fmt.Errorf("turn %s is not owned", turnID)
		}
		until := time.Now().UTC().Add(lease).Format(time.RFC3339Nano)
		if err := q.write(queueEvent{Type: "claim", TurnID: turnID, Owner: q.owner, Until: until}); err != nil {
			return err
		}
		turn.LeaseUntil = until
		return nil
	})
}

func (q *Queue) Pending(conversationID string) []Turn {
	q.mu.Lock()
	defer q.mu.Unlock()
	_ = q.withFileLock(q.reload)
	var result []Turn
	for _, id := range q.order {
		turn := q.turns[id]
		if turn.ConversationID == conversationID && turn.Status == "pending" {
			result = append(result, *turn)
		}
	}
	return result
}

func (q *Queue) Active(conversationID string) []Turn {
	q.mu.Lock()
	defer q.mu.Unlock()
	_ = q.withFileLock(q.reload)
	var result []Turn
	for _, id := range q.order {
		turn := q.turns[id]
		if turn.ConversationID == conversationID && turn.Status == "active" {
			result = append(result, *turn)
		}
	}
	return result
}

func (q *Queue) Get(turnID string) (Turn, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if err := q.withFileLock(q.reload); err != nil {
		return Turn{}, false
	}
	turn, ok := q.turns[turnID]
	if !ok {
		return Turn{}, false
	}
	return *turn, true
}

func (q *Queue) finish(turnID, status, owner string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.withFileLock(func() error {
		if err := q.reload(); err != nil {
			return err
		}
		turn, ok := q.turns[turnID]
		if !ok {
			return fmt.Errorf("unknown turn: %s", turnID)
		}
		if turn.Status != "active" || turn.LeaseOwner != owner {
			return fmt.Errorf("turn %s is not owned", turnID)
		}
		if err := q.write(queueEvent{Type: status, TurnID: turnID, Owner: owner}); err != nil {
			return err
		}
		turn.Status, turn.LeaseOwner, turn.LeaseUntil = status, "", ""
		return nil
	})
}

func (q *Queue) apply(event queueEvent) {
	switch event.Type {
	case "enqueue":
		if event.Turn != nil && event.Turn.ID != "" {
			turn := *event.Turn
			q.turns[turn.ID] = &turn
			q.order = append(q.order, turn.ID)
		}
	case "claim", "complete", "canceled":
		if turn := q.turns[event.TurnID]; turn != nil {
			if event.Type == "claim" {
				turn.Status, turn.LeaseOwner, turn.LeaseUntil = "active", event.Owner, event.Until
			} else {
				turn.Status, turn.LeaseOwner, turn.LeaseUntil = event.Type, "", ""
			}
		}
	case "requeue":
		if turn := q.turns[event.TurnID]; turn != nil {
			turn.Status, turn.LeaseOwner, turn.LeaseUntil = "pending", "", ""
		}
	}
}

func (q *Queue) reload() error {
	q.turns, q.order = make(map[string]*Turn), nil
	file, err := os.Open(q.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event queueEvent
		if json.Unmarshal(scanner.Bytes(), &event) == nil {
			q.apply(event)
		}
	}
	return scanner.Err()
}

func (q *Queue) withFileLock(fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(q.path), 0o755); err != nil {
		return err
	}
	lock, err := os.OpenFile(q.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := flock(lock); err != nil {
		return err
	}
	defer funlock(lock)
	return fn()
}

func leaseExpired(turn *Turn) bool {
	if turn.LeaseUntil == "" {
		return true
	}
	until, err := time.Parse(time.RFC3339Nano, turn.LeaseUntil)
	return err != nil || !until.After(time.Now().UTC())
}

func (q *Queue) write(event queueEvent) error {
	if err := os.MkdirAll(filepath.Dir(q.path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(q.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = file.Write(data)
	return err
}

func newID(prefix string) string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(err)
	}
	return prefix + "-" + hex.EncodeToString(raw[:])
}
