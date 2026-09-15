package codingagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/H4fizWasabie/yen/internal/agent"
)

type webSearchTool struct {
	client   *http.Client
	endpoint string
	keys     []string
}

type tavilyResponse struct {
	Answer  string `json:"answer"`
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
	} `json:"results"`
}

func NewWebSearchTool() agent.Tool {
	endpoint := os.Getenv("YEN_TAVILY_ENDPOINT")
	if endpoint == "" {
		endpoint = "https://api.tavily.com/search"
	}
	return webSearchTool{
		client:   http.DefaultClient,
		endpoint: endpoint,
		keys:     configuredTavilyKeys(),
	}
}

func configuredTavilyKeys() []string {
	keys := []string{os.Getenv("YEN_TAVILY_API_KEY"), os.Getenv("YEN_TAVILY_API_KEY_2")}
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		if key != "" {
			result = append(result, key)
		}
	}
	return result
}

func (webSearchTool) Name() string { return "web_search" }

func (t webSearchTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	query, ok := args["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("query is required")
	}
	keys := t.keys
	if keys == nil {
		keys = configuredTavilyKeys()
	}
	var last error
	for _, key := range keys {
		body, _ := json.Marshal(map[string]any{"query": query, "max_results": 5})
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+key)
		response, err := t.client.Do(request)
		if err != nil {
			last = err
			continue
		}
		data, readErr := readLimited(response.Body, 2<<20)
		response.Body.Close()
		if readErr != nil {
			last = readErr
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			last = fmt.Errorf("Tavily request failed: %s: %s", response.Status, strings.TrimSpace(string(data)))
			continue
		}
		var result tavilyResponse
		if err := json.Unmarshal(data, &result); err != nil {
			return "", err
		}
		return formatWebResults(result), nil
	}
	if last != nil {
		return "", last
	}
	return "", fmt.Errorf("web_search requires YEN_TAVILY_API_KEY (and optionally YEN_TAVILY_API_KEY_2) to be set")
}

func formatWebResults(response tavilyResponse) string {
	parts := make([]string, 0, len(response.Results)+1)
	if response.Answer != "" {
		parts = append(parts, response.Answer)
	}
	for _, result := range response.Results {
		parts = append(parts, result.Title+"\n"+result.URL+"\n"+result.Content)
	}
	if len(parts) == 0 {
		return "No results found."
	}
	return strings.Join(parts, "\n\n")
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(reader, limit))
}

var _ agent.Tool = webSearchTool{}
