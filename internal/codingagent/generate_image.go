package codingagent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/session"
)

type generateImageTool struct {
	client  *http.Client
	session *session.Session
}

func (generateImageTool) Name() string { return "generate_image" }

func (t generateImageTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	prompt, ok := args["prompt"].(string)
	if !ok || strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("prompt is required")
	}
	data, mimeType, provider, err := t.generate(ctx, prompt)
	if err != nil {
		return "", err
	}
	dir := os.Getenv("THEOSES_GENERATED_IMAGES_DIR")
	if dir == "" {
		dataDir := os.Getenv("THEOSES_DATA_DIR")
		if dataDir == "" {
			dataDir = ".theoses-go"
		}
		dir = filepath.Join(dataDir, "generated-images")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	extension := ".jpg"
	if strings.Contains(mimeType, "png") {
		extension = ".png"
	} else if strings.Contains(mimeType, "webp") {
		extension = ".webp"
	} else if strings.Contains(mimeType, "gif") {
		extension = ".gif"
	}
	path := filepath.Join(dir, fmt.Sprintf("%d%s", time.Now().UnixNano(), extension))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	if t.session != nil {
		artifact, err := t.session.StoreArtifact("generated image", filepath.Base(path), data)
		if err != nil {
			return "", err
		}
		path = artifact.Path
	}
	return fmt.Sprintf("Image saved to %s (via %s)", path, provider), nil
}

func (t generateImageTool) ExecuteRich(ctx context.Context, args map[string]any) (agent.ToolResult, error) {
	text, err := t.Execute(ctx, args)
	if err != nil {
		return agent.ToolResult{}, err
	}
	path := strings.TrimPrefix(strings.SplitN(strings.TrimPrefix(text, "Image saved to "), " (via ", 2)[0], " ")
	data, err := os.ReadFile(path)
	if err != nil {
		return agent.ToolResult{Text: text}, nil
	}
	mimeType := sniffImageMIME(data)
	return agent.ToolResult{Text: text, Images: []string{"data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)}}, nil
}

func (t generateImageTool) generate(ctx context.Context, prompt string) ([]byte, string, string, error) {
	client := t.client
	if client == nil {
		client = http.DefaultClient
	}
	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
		endpoint := os.Getenv("THEOSES_OPENROUTER_IMAGE_ENDPOINT")
		if endpoint == "" {
			endpoint = "https://openrouter.ai/api/v1/images"
		}
		model := os.Getenv("THEOSES_OPENROUTER_IMAGE_MODEL")
		if model == "" {
			model = "meta/muse-image"
		}
		body, _ := json.Marshal(map[string]string{"model": model, "prompt": prompt})
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, "", "", err
		}
		request.Header.Set("Authorization", "Bearer "+key)
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil {
			return nil, "", "", err
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			message, _ := io.ReadAll(io.LimitReader(response.Body, 16<<10))
			return nil, "", "", fmt.Errorf("OpenRouter %s: %s", response.Status, strings.TrimSpace(string(message)))
		}
		var result struct {
			Data []struct {
				Base64 string `json:"b64_json"`
			} `json:"data"`
		}
		if err := json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(&result); err != nil {
			return nil, "", "", err
		}
		if len(result.Data) == 0 || result.Data[0].Base64 == "" {
			return nil, "", "", fmt.Errorf("OpenRouter: no image in response")
		}
		data, err := base64.StdEncoding.DecodeString(result.Data[0].Base64)
		if err != nil {
			return nil, "", "", err
		}
		return data, sniffImageMIME(data), "OpenRouter (" + model + ")", nil
	}
	return nil, "", "", fmt.Errorf("image generation requires OPENROUTER_API_KEY")
}

func sniffImageMIME(data []byte) string {
	switch {
	case len(data) >= 2 && data[0] == 0xff && data[1] == 0xd8:
		return "image/jpeg"
	case len(data) >= 4 && bytes.Equal(data[:4], []byte{0x89, 'P', 'N', 'G'}):
		return "image/png"
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	case len(data) >= 3 && string(data[:3]) == "GIF":
		return "image/gif"
	default:
		return "image/jpeg"
	}
}

var _ agent.Tool = generateImageTool{}
