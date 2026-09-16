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

func TestApplyProviderResponseHookCopiesStatusAndHeaders(t *testing.T) {
	var gotStatus int
	var gotHeaders map[string][]string
	ctx := agent.WithProviderResponseHook(context.Background(), func(_ context.Context, status int, headers map[string][]string) {
		gotStatus = status
		gotHeaders = headers
		headers["X-Hook"] = []string{"ignored-by-transport"}
	})
	response := &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"X-Request-ID": []string{"request-1"}}}
	applyProviderResponseHook(ctx, response)
	if gotStatus != http.StatusTooManyRequests || gotHeaders["X-Request-ID"][0] != "request-1" {
		t.Fatalf("status=%d headers=%v", gotStatus, gotHeaders)
	}
	if _, ok := response.Header["X-Hook"]; ok {
		t.Fatalf("hook mutated transport headers: %v", response.Header)
	}
}
