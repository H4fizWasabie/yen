package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoginOpenAICodexDevice(t *testing.T) {
	var tokenPolls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/usercode":
			w.Write([]byte(`{"device_auth_id":"device-1","user_code":"ABCD-EFGH","interval":0}`))
		case "/device":
			tokenPolls++
			if tokenPolls == 1 {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.Write([]byte(`{"authorization_code":"auth-code","code_verifier":"verifier"}`))
		case "/token":
			if err := r.ParseForm(); err != nil || r.Form.Get("code") != "auth-code" || r.Form.Get("code_verifier") != "verifier" || r.Form.Get("redirect_uri") == "" {
				t.Fatalf("form=%v err=%v", r.Form, err)
			}
			w.Write([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":60}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var message string
	credential, err := loginOpenAICodexDevice(context.Background(), server.Client(), func(value string) { message = value }, server.URL+"/usercode", server.URL+"/device", server.URL+"/token", server.URL+"/verify")
	if err != nil {
		t.Fatal(err)
	}
	if credential.Type != "oauth" || credential.Access != "access" || credential.Refresh != "refresh" || credential.Expires <= 0 || !strings.Contains(message, "ABCD-EFGH") {
		t.Fatalf("credential=%#v message=%q", credential, message)
	}
}

func TestRefreshOpenAICodex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "old-refresh" {
			t.Fatalf("form=%v err=%v", r.Form, err)
		}
		_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":60}`))
	}))
	defer server.Close()
	credential, err := refreshOpenAICodex(context.Background(), server.Client(), server.URL, "old-refresh")
	if err != nil || credential.Access != "new-access" || credential.Refresh != "new-refresh" || credential.Expires <= 0 {
		t.Fatalf("credential=%#v err=%v", credential, err)
	}
}
