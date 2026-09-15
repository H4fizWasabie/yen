package codingagent

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateImageSavesOpenRouterArtifact(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString([]byte{0xff, 0xd8, 0xff}) + `"}]}`))
	}))
	defer server.Close()
	dir := t.TempDir()
	t.Setenv("YEN_OPENROUTER_API_KEY", "key")
	t.Setenv("YEN_OPENROUTER_IMAGE_ENDPOINT", server.URL)
	t.Setenv("YEN_DATA_DIR", dir)
	_, err := (generateImageTool{client: server.Client()}).Execute(context.Background(), map[string]any{"prompt": "a test image"})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "generated-images"))
	if err != nil || len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".jpg") {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
}

func TestGenerateImageRichResultOmitsUndecodableImage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString([]byte("not an image")) + `"}]}`))
	}))
	defer server.Close()
	t.Setenv("YEN_OPENROUTER_API_KEY", "key")
	t.Setenv("YEN_OPENROUTER_IMAGE_ENDPOINT", server.URL)
	t.Setenv("YEN_DATA_DIR", t.TempDir())
	result, err := (generateImageTool{client: server.Client()}).ExecuteRich(context.Background(), map[string]any{"prompt": "invalid"})
	if err != nil || len(result.Images) != 0 || !strings.Contains(result.Text, "Image omitted") {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestGeneratedImageRejectsOversizedInlinePayload(t *testing.T) {
	if generatedImageIsDecodable(make([]byte, int(generatedImageMaxInlineBytes)), "image/webp") {
		t.Fatal("oversized image accepted for inline attachment")
	}
}
