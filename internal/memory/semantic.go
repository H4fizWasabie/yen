package memory

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Scope string

const (
	ScopeEngine       Scope = "engine"
	ScopeOwner        Scope = "owner"
	ScopeWorkspace    Scope = "workspace"
	ScopeConversation Scope = "conversation"
)

type Edge struct {
	Target string `yaml:"target"`
	Rel    string `yaml:"rel"`
}

type Node struct {
	ID             string `yaml:"id"`
	Type           string `yaml:"type"`
	Subject        string `yaml:"subject"`
	At             string `yaml:"at"`
	Edges          []Edge `yaml:"edges"`
	Body           string `yaml:"-"`
	Scope          Scope  `yaml:"scope,omitempty"`
	OwnerID        string `yaml:"ownerId,omitempty"`
	WorkspaceID    string `yaml:"workspaceId,omitempty"`
	ConversationID string `yaml:"conversationId,omitempty"`
	Channel        string `yaml:"channel,omitempty"`
	TurnID         string `yaml:"turnId,omitempty"`
}

type Context struct {
	OwnerID            string
	WorkspaceID        string
	ConversationID     string
	ConversationScoped bool
}

type Store struct{ dir string }

func NewStore(dir string) *Store { return &Store{dir: dir} }

func (s *Store) Write(node Node) error {
	if err := validateNode(node); err != nil {
		return err
	}
	if node.Type == "" {
		node.Type = "semantic"
	}
	if node.At == "" {
		node.At = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	front := node
	front.Body = ""
	b, err := yaml.Marshal(front)
	if err != nil {
		return err
	}
	raw := "---\n" + string(b) + "---\n"
	if node.Body != "" {
		raw += node.Body + "\n"
	}
	tmp, err := os.CreateTemp(s.dir, ".memory-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.WriteString(raw); err == nil {
		err = tmp.Close()
	} else {
		_ = tmp.Close()
	}
	if err != nil {
		return err
	}
	return os.Rename(tmpName, filepath.Join(s.dir, node.ID+".md"))
}

func (s *Store) Get(id string) (Node, bool, error) {
	if !safeID(id) {
		return Node{}, false, errors.New("invalid memory node id")
	}
	b, err := os.ReadFile(filepath.Join(s.dir, id+".md"))
	if errors.Is(err, os.ErrNotExist) {
		return Node{}, false, nil
	}
	if err != nil {
		return Node{}, false, err
	}
	n, err := parseNode(string(b))
	return n, err == nil, err
}

func (s *Store) List() ([]Node, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var nodes []Node
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		b, readErr := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if readErr != nil {
			return nil, readErr
		}
		n, parseErr := parseNode(string(b))
		if parseErr == nil {
			nodes = append(nodes, n)
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].At > nodes[j].At })
	return nodes, nil
}

func (s *Store) Remember(query string, ctx Context) ([]Node, error) {
	nodes, err := s.List()
	if err != nil {
		return nil, err
	}
	terms := terms(query)
	if len(terms) == 0 {
		return nil, nil
	}
	visibleNodes := nodes[:0]
	for _, node := range nodes {
		if visible(node, ctx) {
			visibleNodes = append(visibleNodes, node)
		}
	}
	byID := make(map[string]Node, len(visibleNodes))
	superseded := make(map[string]bool)
	for _, node := range visibleNodes {
		byID[node.ID] = node
		for _, edge := range node.Edges {
			if edge.Rel == "supersedes" {
				superseded[edge.Target] = true
			}
		}
	}
	scores := make(map[string]int, len(visibleNodes))
	var queue []struct {
		id    string
		depth int
	}
	for _, node := range visibleNodes {
		if matches(node, terms) {
			scores[node.ID] = matchScore(node, terms)
			queue = append(queue, struct {
				id    string
				depth int
			}{node.ID, 0})
		}
	}
	depths := make(map[string]int, len(queue))
	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		if _, seen := depths[item.id]; seen {
			continue
		}
		depths[item.id] = item.depth
		if item.depth >= 2 {
			continue
		}
		for _, node := range visibleNodes {
			connected := false
			if node.ID == item.id {
				for _, edge := range node.Edges {
					if _, ok := byID[edge.Target]; ok {
						queue = append(queue, struct {
							id    string
							depth int
						}{edge.Target, item.depth + 1})
					}
				}
			}
			for _, edge := range node.Edges {
				if edge.Target == item.id {
					connected = true
					break
				}
			}
			if connected {
				queue = append(queue, struct {
					id    string
					depth int
				}{node.ID, item.depth + 1})
			}
		}
	}
	type hit struct {
		node         Node
		depth, score int
	}
	hits := make([]hit, 0, len(depths))
	for id, depth := range depths {
		if !superseded[id] {
			hits = append(hits, hit{byID[id], depth, scores[id]})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].depth != hits[j].depth {
			return hits[i].depth < hits[j].depth
		}
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].node.At > hits[j].node.At
	})
	result := make([]Node, 0, min(8, len(hits)))
	for _, item := range hits {
		result = append(result, item.node)
		if len(result) == 8 {
			break
		}
	}
	return result, nil
}

func NewNode(subject string) Node {
	subject = strings.TrimSpace(subject)
	parts := strings.FieldsFunc(strings.ToLower(subject), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
	id := strings.Join(parts, "_")
	if len(id) > 48 {
		id = id[:48]
	}
	if id == "" {
		id = "note"
	}
	return Node{ID: id + "_" + newID()[:8], Type: "semantic", Subject: subject, At: time.Now().UTC().Format(time.RFC3339Nano)}
}

func validateNode(n Node) error {
	if !safeID(n.ID) {
		return errors.New("invalid memory node id")
	}
	if n.Subject == "" {
		return errors.New("memory subject is required")
	}
	if n.Type != "" && n.Type != "semantic" {
		return errors.New("memory type must be semantic")
	}
	return nil
}

func parseNode(raw string) (Node, error) {
	if !strings.HasPrefix(raw, "---\n") {
		return Node{}, errors.New("missing memory front matter")
	}
	rest := strings.TrimPrefix(raw, "---\n")
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return Node{}, errors.New("unterminated memory front matter")
	}
	var n Node
	if err := yaml.Unmarshal([]byte(rest[:end]), &n); err != nil {
		return Node{}, err
	}
	n.Body = strings.TrimSuffix(rest[end+5:], "\n")
	if n.Type == "" {
		n.Type = "semantic"
	}
	if err := validateNode(n); err != nil {
		return Node{}, err
	}
	return n, nil
}

func visible(n Node, c Context) bool {
	switch n.Scope {
	case ScopeEngine, "":
		return true
	case ScopeOwner:
		return n.OwnerID != "" && n.OwnerID == c.OwnerID
	case ScopeWorkspace:
		return n.WorkspaceID != "" && n.WorkspaceID == c.WorkspaceID
	case ScopeConversation:
		return n.ConversationID != "" && n.ConversationID == c.ConversationID
	default:
		return false
	}
}

func terms(query string) []string {
	var all, significant []string
	for _, term := range strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	}) {
		if len(term) < 3 {
			continue
		}
		all = append(all, term)
		if !queryStopwords[term] {
			significant = append(significant, term)
		}
	}
	if len(significant) > 0 {
		return significant
	}
	return all
}

var queryStopwords = map[string]bool{
	"a": true, "about": true, "am": true, "an": true, "and": true, "are": true,
	"do": true, "does": true, "did": true, "for": true, "he": true, "her": true,
	"him": true, "his": true, "how": true, "i": true, "is": true, "it": true,
	"its": true, "me": true, "my": true, "of": true, "or": true, "our": true,
	"remember": true, "she": true, "that": true, "the": true, "their": true,
	"them": true, "they": true, "this": true, "to": true, "us": true, "was": true,
	"we": true, "were": true, "what": true, "when": true, "where": true, "who": true,
	"why": true, "with": true, "you": true, "your": true,
}

func matches(n Node, ts []string) bool {
	if len(ts) == 0 {
		return false
	}
	haystack := strings.ToLower(n.Subject + " " + n.Body)
	for _, term := range ts {
		if containsTerm(haystack, term) {
			return true
		}
	}
	return false
}

func matchScore(n Node, ts []string) int {
	haystack := strings.ToLower(n.Subject + " " + n.Body)
	score := 0
	for _, term := range ts {
		if containsTerm(haystack, term) {
			score++
		}
	}
	return score
}

func MatchScore(query, text string) int {
	ts := terms(query)
	if len(ts) == 0 {
		return 0
	}
	lower := strings.ToLower(text)
	score := 0
	for _, term := range ts {
		if containsTerm(lower, term) {
			score++
		}
	}
	return score
}

func containsTerm(haystack, term string) bool {
	for start := 0; ; {
		i := strings.Index(haystack[start:], term)
		if i < 0 {
			return false
		}
		i += start
		before := i == 0 || !isWordByte(haystack[i-1])
		after := i+len(term) == len(haystack) || !isWordByte(haystack[i+len(term)])
		if before && after {
			return true
		}
		start = i + 1
	}
}

func isWordByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= '0' && value <= '9'
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func safeID(id string) bool {
	return id != "" && filepath.Base(id) == id && !strings.ContainsAny(id, `/\\`)
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("memory id entropy: %v", err))
	}
	return fmt.Sprintf("%x", b)
}
