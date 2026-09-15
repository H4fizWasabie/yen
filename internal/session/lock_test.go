package session

import (
	"path/filepath"
	"sync"
	"testing"
)

func TestWithPathLockKeepsConcurrentSessionWritersConsistent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	seed := New(path, Header{ID: "session-1"})
	if err := seed.Save(); err != nil {
		t.Fatal(err)
	}
	left, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	right, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	var wait sync.WaitGroup
	for _, current := range []*Session{left, right} {
		current := current
		wait.Add(1)
		go func() {
			defer wait.Done()
			for i := 0; i < 20; i++ {
				if err := WithPathLock(path, func() error {
					if err := current.Reload(); err != nil {
						return err
					}
					_, err := current.Append(Message{Role: "user", Content: "message"})
					return err
				}); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wait.Wait()

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(reopened.Messages()); got != 40 {
		t.Fatalf("messages=%d, want 40", got)
	}
	seen := make(map[string]bool)
	for _, entry := range reopened.Tree() {
		if seen[entry.ID] {
			t.Fatalf("duplicate entry ID %q", entry.ID)
		}
		seen[entry.ID] = true
	}
}
