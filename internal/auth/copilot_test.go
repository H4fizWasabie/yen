package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoginGitHubCopilot(t *testing.T) {
	var polls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/device":
			_, _ = w.Write([]byte(`{"device_code":"device","user_code":"ABCD-EFGH","verification_uri":"https://github.com/login/device","interval":0,"expires_in":60}`))
		case "/access":
			polls++
			if polls == 1 {
				_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"github-access"}`))
		case "/copilot":
			if r.Header.Get("Authorization") != "Bearer github-access" || r.Header.Get("Copilot-Integration-Id") != "vscode-chat" {
				t.Fatalf("authorization=%q integration=%q", r.Header.Get("Authorization"), r.Header.Get("Copilot-Integration-Id"))
			}
			_, _ = w.Write([]byte(`{"token":"copilot-token","expires_at":4102444800}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var message string
	credential, err := loginGitHubCopilot(context.Background(), server.Client(), func(value string) { message = value }, server.URL+"/device", server.URL+"/access", server.URL+"/copilot")
	if err != nil {
		t.Fatal(err)
	}
	if credential.Type != "oauth" || credential.Access != "copilot-token" || credential.Refresh != "github-access" || credential.Expires <= 0 || !strings.Contains(message, "ABCD-EFGH") {
		t.Fatalf("credential=%#v message=%q", credential, message)
	}
}
