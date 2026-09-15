package memory

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Episode struct {
	ID                     string
	StartedAt              string
	EndedAt                string
	Summary                string
	CreatedAt              string
	RelatedSemanticNodeIDs []string
	WorkspaceID            string
	ConversationID         string
	Channel                string
	TurnID                 string
}

type EpisodicStore struct{ db *sql.DB }

func OpenEpisodicStore(path string) (*EpisodicStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS episodes (
id TEXT PRIMARY KEY, started_at TEXT NOT NULL, ended_at TEXT NOT NULL,
summary TEXT NOT NULL, created_at TEXT NOT NULL,
related_semantic_node_ids TEXT NOT NULL DEFAULT '[]',
workspace_id TEXT NOT NULL DEFAULT '', conversation_id TEXT NOT NULL DEFAULT '',
channel TEXT NOT NULL DEFAULT '', turn_id TEXT NOT NULL DEFAULT ''
); CREATE INDEX IF NOT EXISTS idx_episodes_started_at ON episodes(started_at);`)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &EpisodicStore{db: db}, nil
}

func (s *EpisodicStore) Record(e Episode) error {
	if e.ID == "" {
		e.ID = newID()
	}
	if e.CreatedAt == "" {
		e.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if e.StartedAt == "" {
		e.StartedAt = e.CreatedAt
	}
	if e.EndedAt == "" {
		e.EndedAt = e.StartedAt
	}
	ids, err := json.Marshal(e.RelatedSemanticNodeIDs)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO episodes (id,started_at,ended_at,summary,created_at,related_semantic_node_ids,workspace_id,conversation_id,channel,turn_id) VALUES (?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET started_at=excluded.started_at,ended_at=excluded.ended_at,summary=excluded.summary,created_at=excluded.created_at,related_semantic_node_ids=excluded.related_semantic_node_ids,workspace_id=excluded.workspace_id,conversation_id=excluded.conversation_id,channel=excluded.channel,turn_id=excluded.turn_id`, e.ID, e.StartedAt, e.EndedAt, strings.TrimSpace(e.Summary), e.CreatedAt, string(ids), e.WorkspaceID, e.ConversationID, e.Channel, e.TurnID)
	return err
}

func (s *EpisodicStore) Recent(conversationID string, limit int) ([]Episode, error) {
	if limit <= 0 {
		limit = 8
	}
	rows, err := s.db.Query(`SELECT id,started_at,ended_at,summary,created_at,related_semantic_node_ids,workspace_id,conversation_id,channel,turn_id FROM episodes WHERE conversation_id=? ORDER BY started_at DESC LIMIT ?`, conversationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEpisodes(rows)
}

func (s *EpisodicStore) Search(conversationID, query string, limit int) ([]Episode, error) {
	if limit <= 0 {
		limit = 8
	}
	rows, err := s.db.Query(`SELECT id,started_at,ended_at,summary,created_at,related_semantic_node_ids,workspace_id,conversation_id,channel,turn_id FROM episodes WHERE conversation_id=? ORDER BY started_at DESC`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	all, err := scanEpisodes(rows)
	if err != nil {
		return nil, err
	}
	queryTerms := terms(query)
	if len(queryTerms) == 0 {
		return trimEpisodes(all, limit), nil
	}
	result := make([]Episode, 0, limit)
	for _, episode := range all {
		lower := strings.ToLower(episode.Summary)
		for _, term := range queryTerms {
			if strings.Contains(lower, term) {
				result = append(result, episode)
				break
			}
		}
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func (s *EpisodicStore) AtTime(conversationID, timestamp string, limit int) ([]Episode, error) {
	if limit <= 0 {
		limit = 8
	}
	rows, err := s.db.Query(`SELECT id,started_at,ended_at,summary,created_at,related_semantic_node_ids,workspace_id,conversation_id,channel,turn_id FROM episodes WHERE conversation_id=? AND started_at <= ? AND ended_at >= ? ORDER BY started_at DESC LIMIT ?`, conversationID, timestamp, timestamp, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	containing, err := scanEpisodes(rows)
	if err != nil {
		return nil, err
	}
	if len(containing) > 0 {
		return containing, nil
	}
	rows, err = s.db.Query(`SELECT id,started_at,ended_at,summary,created_at,related_semantic_node_ids,workspace_id,conversation_id,channel,turn_id FROM episodes WHERE conversation_id=? ORDER BY ABS(julianday(started_at) - julianday(?)) LIMIT ?`, conversationID, timestamp, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEpisodes(rows)
}

func (s *EpisodicStore) Close() error { return s.db.Close() }

func scanEpisodes(rows *sql.Rows) ([]Episode, error) {
	var out []Episode
	for rows.Next() {
		var e Episode
		var raw string
		if err := rows.Scan(&e.ID, &e.StartedAt, &e.EndedAt, &e.Summary, &e.CreatedAt, &raw, &e.WorkspaceID, &e.ConversationID, &e.Channel, &e.TurnID); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(raw), &e.RelatedSemanticNodeIDs)
		out = append(out, e)
	}
	return out, rows.Err()
}

func trimEpisodes(episodes []Episode, limit int) []Episode {
	if len(episodes) > limit {
		return episodes[:limit]
	}
	return episodes
}
