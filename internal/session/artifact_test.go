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

func TestArtifactCatalogUsesLiveNewestUniquePaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	s := New(path, Header{ID: "session-1", CWD: dir})
	livePath := filepath.Join(dir, "live.txt")
	if err := os.WriteFile(livePath, []byte("live"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.appendArtifact(Artifact{Label: "old label", Path: livePath, Size: 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.appendArtifact(Artifact{Label: "new label", Path: livePath, Size: 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.appendArtifact(Artifact{Label: "stale label", Path: filepath.Join(dir, "gone.txt"), Size: 4}); err != nil {
		t.Fatal(err)
	}

	catalog := s.ArtifactCatalog(1000)
	if !strings.Contains(catalog, "- new label (4 bytes): "+livePath) {
		t.Fatalf("catalog=%q", catalog)
	}
	if strings.Contains(catalog, "old label") || strings.Contains(catalog, "stale label") {
		t.Fatalf("catalog retained stale artifact entries: %q", catalog)
	}
}

func TestAppendArtifactNormalizesBlankLabel(t *testing.T) {
	dir := t.TempDir()
	s := New(filepath.Join(dir, "session.jsonl"), Header{ID: "session-1", CWD: dir})
	livePath := filepath.Join(dir, "live.txt")
	if err := os.WriteFile(livePath, []byte("live"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.appendArtifact(Artifact{Label: "  ", Path: livePath, Size: 4}); err != nil {
		t.Fatal(err)
	}
	if got := s.ArtifactCatalog(1000); !strings.Contains(got, "- document (4 bytes): "+livePath) {
		t.Fatalf("catalog=%q", got)
	}
}
