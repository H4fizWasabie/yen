package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreArtifactPersistsBoundedCatalog(t *testing.T) {
	dir := t.TempDir()
	s := New(filepath.Join(dir, "session.jsonl"), Header{ID: "session-1", CWD: dir})
	artifact, err := s.StoreArtifact("test image", "image.png", []byte("png-data"))
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Size != 8 || !strings.HasPrefix(artifact.Path, filepath.Join(dir, "artifacts", "session-1")) {
		t.Fatalf("artifact=%#v", artifact)
	}
	info, err := os.Stat(artifact.Path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("stat=%v info=%v", err, info)
	}
	reopened, err := Open(filepath.Join(dir, "session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.ArtifactCatalog(1000); !strings.Contains(got, "test image") || !strings.Contains(got, "8 bytes") {
		t.Fatalf("catalog=%q", got)
	}
	if _, err := reopened.StoreArtifact("empty", "empty", nil); err == nil {
		t.Fatal("empty artifact unexpectedly accepted")
	}
}
