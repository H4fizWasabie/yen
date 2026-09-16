package memory

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

type Checkpoint struct {
	LastEntryID   string `json:"lastEntryId"`
	LastFailureAt string `json:"lastFailureAt,omitempty"`
}

type Checkpoints struct {
	path   string
	values map[string]Checkpoint
	mu     sync.Mutex
}

func OpenCheckpoints(path string) (*Checkpoints, error) {
	c := &Checkpoints{path: path, values: map[string]Checkpoint{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(b, &c.values) != nil || c.values == nil {
		c.values = map[string]Checkpoint{}
	}
	return c, nil
}

func (c *Checkpoints) Get(conversationID string) Checkpoint {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.reload()
	return c.values[conversationID]
}

func (c *Checkpoints) Set(conversationID string, checkpoint Checkpoint) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	lock, err := os.OpenFile(c.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := lockFile(lock); err != nil {
		return err
	}
	defer unlockFile(lock)
	if err := c.reload(); err != nil {
		return err
	}
	c.values[conversationID] = checkpoint
	b, err := json.MarshalIndent(c.values, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(c.path), ".checkpoint-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(b); err == nil {
		err = tmp.Close()
	} else {
		_ = tmp.Close()
	}
	if err != nil {
		return err
	}
	return os.Rename(name, c.path)
}

func (c *Checkpoints) reload() error {
	b, err := os.ReadFile(c.path)
	if errors.Is(err, os.ErrNotExist) {
		c.values = map[string]Checkpoint{}
		return nil
	}
	if err != nil {
		return err
	}
	var values map[string]Checkpoint
	if err := json.Unmarshal(b, &values); err != nil {
		c.values = map[string]Checkpoint{}
		return nil
	}
	if values == nil {
		values = map[string]Checkpoint{}
	}
	c.values = values
	return nil
}
