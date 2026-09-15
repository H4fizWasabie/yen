package codingagent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	generators := []func(context.Context, *http.Client, string) ([]byte, string, string, error){
		generateOpenRouter,
		generateCloudflare,
		generatePollinations,
	}
	var lastErr error
	for _, generate := range generators {
		data, mimeType, provider, err := generate(ctx, client, prompt)
		if err == nil {
			return data, mimeType, provider, nil
		}
		lastErr = err
	}
	return nil, "", "", fmt.Errorf("image generation failed: %w", lastErr)
}

func generateOpenRouter(ctx context.Context, client *http.Client, prompt string) ([]byte, string, string, error) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		return nil, "", "", fmt.Errorf("OPENROUTER_API_KEY not set")
	}
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

func generateCloudflare(ctx context.Context, client *http.Client, prompt string) ([]byte, string, string, error) {
	accountID, token := os.Getenv("CLOUDFLARE_ACCOUNT_ID"), os.Getenv("CLOUDFLARE_API_TOKEN")
	if accountID == "" || token == "" {
		return nil, "", "", fmt.Errorf("CLOUDFLARE_ACCOUNT_ID/CLOUDFLARE_API_TOKEN not set")
	}
	model := os.Getenv("THEOSES_IMAGE_MODEL")
	if model == "" {
		model = "@cf/black-forest-labs/flux-1-schnell"
	}
	body, _ := json.Marshal(map[string]string{"prompt": prompt})
	endpoint := "https://api.cloudflare.com/client/v4/accounts/" + url.PathEscape(accountID) + "/ai/run/" + url.PathEscape(model)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, "", "", err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, "", "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 16<<10))
		return nil, "", "", fmt.Errorf("Cloudflare Workers AI %s: %s", response.Status, strings.TrimSpace(string(message)))
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return nil, "", "", err
	}
	if strings.HasPrefix(response.Header.Get("Content-Type"), "image/") {
		return data, sniffImageMIME(data), "Cloudflare Workers AI (" + model + ")", nil
	}
	var envelope struct {
		Result struct {
			Image  string   `json:"image"`
			Images []string `json:"images"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, "", "", err
	}
	encoded := envelope.Result.Image
	if encoded == "" && len(envelope.Result.Images) > 0 {
		encoded = envelope.Result.Images[0]
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(decoded) == 0 {
		return nil, "", "", fmt.Errorf("Cloudflare Workers AI: no image in response")
	}
	return decoded, sniffImageMIME(decoded), "Cloudflare Workers AI (" + model + ")", nil
}

func generatePollinations(ctx context.Context, client *http.Client, prompt string) ([]byte, string, string, error) {
	endpoint := "https://image.pollinations.ai/prompt/" + url.PathEscape(prompt) + "?width=1024&height=1024&nologo=true&model=flux-realism"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, "", "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", "", fmt.Errorf("Pollinations.ai %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return nil, "", "", err
	}
	if len(data) < 100 {
		return nil, "", "", fmt.Errorf("Pollinations.ai: response too small to be an image")
	}
	return data, sniffImageMIME(data), "Pollinations.ai", nil
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
