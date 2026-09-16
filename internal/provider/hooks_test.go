package provider

import (
	"context"
	"net/http"
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
)

func TestApplyProviderHeaderHookPreservesAndMutatesHeaders(t *testing.T) {
	headers := http.Header{"Authorization": []string{"Bearer secret"}, "X-Existing": []string{"old"}}
	ctx := agent.WithProviderHeaderHook(context.Background(), func(_ context.Context, values map[string][]string) {
		values["X-Existing"] = []string{"new"}
		values["X-Added"] = []string{"value"}
		delete(values, "Authorization")
	})
	applyProviderHeaderHook(ctx, headers)
	if headers.Get("Authorization") != "" || headers.Get("X-Existing") != "new" || headers.Get("X-Added") != "value" {
		t.Fatalf("headers=%v", headers)
	}
}
