package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestRefreshAnthropic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("request=%s content-type=%q", r.Method, r.Header.Get("Content-Type"))
		}
		_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`))
	}))
	defer server.Close()

	credential, err := refreshAnthropic(context.Background(), server.Client(), server.URL, "old-refresh")
	if err != nil || credential.Type != "oauth" || credential.Access != "new-access" || credential.Refresh != "new-refresh" || credential.Expires <= 0 {
		t.Fatalf("credential=%#v err=%v", credential, err)
	}
}

func TestLoginAnthropicUsesLocalCallbackAndPKCE(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload["grant_type"] != "authorization_code" || payload["code"] != "auth-code" || payload["state"] == "" || payload["code_verifier"] == "" {
			t.Fatalf("payload=%v err=%v", payload, err)
		}
		_, _ = w.Write([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":3600}`))
	}))
	defer tokenServer.Close()

	loginURL := make(chan string, 1)
	result := make(chan struct {
		credential Credential
		err        error
	}, 1)
	go func() {
		credential, err := loginAnthropic(context.Background(), tokenServer.Client(), func(value string) { loginURL <- strings.TrimSpace(strings.TrimPrefix(value, "Open ")) }, "https://authorize.example/oauth/authorize", tokenServer.URL)
		result <- struct {
			credential Credential
			err        error
		}{credential, err}
	}()

	var authorize *url.URL
	select {
	case value := <-loginURL:
		var err error
		authorize, err = url.Parse(value)
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("login did not publish authorization URL")
	}
	callback := "http://localhost:53692/callback?code=auth-code&state=" + url.QueryEscape(authorize.Query().Get("state"))
	response, err := http.Get(callback)
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
