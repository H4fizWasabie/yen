package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
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

func TestLoginOpenAICodexBrowserUsesLocalCallbackAndPKCE(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "auth-code" || r.Form.Get("code_verifier") == "" || r.Form.Get("redirect_uri") != "http://localhost:1455/auth/callback" {
			t.Fatalf("form=%v err=%v", r.Form, err)
		}
		_, _ = w.Write([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":3600}`))
	}))
	defer tokenServer.Close()

	authURL := make(chan string, 1)
	result := make(chan struct {
		credential Credential
		err        error
	}, 1)
	go func() {
		credential, err := loginOpenAICodexBrowser(context.Background(), tokenServer.Client(), func(value string) { authURL <- value }, "https://authorize.example/oauth/authorize", tokenServer.URL)
		result <- struct {
			credential Credential
			err        error
		}{credential, err}
	}()

	var parsed *url.URL
	select {
	case value := <-authURL:
		var err error
		parsed, err = url.Parse(value)
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("login did not publish authorization URL")
	}
	response, err := http.Get("http://localhost:1455/auth/callback?code=auth-code&state=" + url.QueryEscape(parsed.Query().Get("state")))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("callback status=%s", response.Status)
	}

	select {
	case outcome := <-result:
		if outcome.err != nil || outcome.credential.Access != "access" || outcome.credential.Refresh != "refresh" {
			t.Fatalf("credential=%#v err=%v", outcome.credential, outcome.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("login did not complete")
	}
}
