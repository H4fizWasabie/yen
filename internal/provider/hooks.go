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
