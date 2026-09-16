package provider

import (
	"context"
	"net/http"

	"github.com/H4fizWasabie/yen/internal/agent"
)

func applyProviderHeaderHook(ctx context.Context, headers http.Header) {
	hook := agent.ProviderHeaderHookFromContext(ctx)
	if hook == nil {
		return
	}
	values := make(map[string][]string, len(headers))
	for name, entries := range headers {
		values[name] = append([]string(nil), entries...)
	}
	hook(ctx, values)
	for name := range headers {
		delete(headers, name)
	}
	for name, entries := range values {
		for _, value := range entries {
			headers.Add(name, value)
		}
	}
}

func applyProviderResponseHook(ctx context.Context, response *http.Response) {
	hook := agent.ProviderResponseHookFromContext(ctx)
	if hook == nil || response == nil {
		return
	}
	headers := make(map[string][]string, len(response.Header))
	for name, values := range response.Header {
		headers[name] = append([]string(nil), values...)
	}
	hook(ctx, response.StatusCode, headers)
}
