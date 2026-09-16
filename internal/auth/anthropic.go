package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	anthropicClientID  = "9d1c250a-e61b-44d9-88ed-594dd1962f5e"
	anthropicAuthorize = "https://claude.ai/oauth/authorize"
	anthropicToken     = "https://platform.claude.com/v1/oauth/token"
	anthropicCallback  = "http://localhost:53692/callback"
	anthropicScopes    = "org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"
)

// LoginAnthropic performs Anthropic's browser OAuth flow using a localhost callback.
func LoginAnthropic(ctx context.Context, notify func(string)) (Credential, error) {
	return loginAnthropic(ctx, http.DefaultClient, notify, anthropicAuthorize, anthropicToken)
}

// RefreshAnthropic exchanges a stored refresh token for a new OAuth credential.
func RefreshAnthropic(ctx context.Context, refresh string) (Credential, error) {
	return refreshAnthropic(ctx, &http.Client{Timeout: 30 * time.Second}, anthropicToken, refresh)
}

func loginAnthropic(ctx context.Context, client *http.Client, notify func(string), authorizeEndpoint, tokenEndpoint string) (Credential, error) {
	verifier, err := randomPKCE()
	if err != nil {
		return Credential{}, err
	}
	listener, err := net.Listen("tcp", callbackHost()+":53692")
	if err != nil {
		return Credential{}, fmt.Errorf("anthropic OAuth callback: %w", err)
	}
	defer listener.Close()

	state := verifier
	callback := make(chan struct{ code, state string }, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.Error(w, "callback route not found", http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("error") != "" {
			http.Error(w, "authentication did not complete", http.StatusBadRequest)
			return
		}
		code, receivedState := r.URL.Query().Get("code"), r.URL.Query().Get("state")
		if code == "" || receivedState == "" || receivedState != state {
			http.Error(w, "invalid OAuth callback", http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, "Authentication completed. You can close this window.")
		callback <- struct{ code, state string }{code, receivedState}
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Shutdown(context.Background())

	params := url.Values{
		"code": {"true"}, "client_id": {anthropicClientID}, "response_type": {"code"},
		"redirect_uri": {anthropicCallback}, "scope": {anthropicScopes},
		"code_challenge": {pkceChallenge(verifier)}, "code_challenge_method": {"S256"}, "state": {state},
	}
	if notify != nil {
		notify("Open " + strings.TrimRight(authorizeEndpoint, "?") + "?" + params.Encode())
	}
	select {
	case result := <-callback:
		return exchangeAnthropic(ctx, client, tokenEndpoint, result.code, result.state, verifier)
	case <-ctx.Done():
		return Credential{}, ctx.Err()
	}
}

func refreshAnthropic(ctx context.Context, client *http.Client, endpoint, refresh string) (Credential, error) {
	return anthropicTokenRequest(ctx, client, endpoint, map[string]string{"grant_type": "refresh_token", "client_id": anthropicClientID, "refresh_token": refresh})
}

func exchangeAnthropic(ctx context.Context, client *http.Client, endpoint, code, state, verifier string) (Credential, error) {
	return anthropicTokenRequest(ctx, client, endpoint, map[string]string{"grant_type": "authorization_code", "client_id": anthropicClientID, "code": code, "state": state, "redirect_uri": anthropicCallback, "code_verifier": verifier})
}

func anthropicTokenRequest(ctx context.Context, client *http.Client, endpoint string, payload map[string]string) (Credential, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return Credential{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Credential{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return Credential{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Credential{}, fmt.Errorf("anthropic OAuth token request failed with status %s", response.Status)
	}
	var token struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
		Expires int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&token); err != nil {
		return Credential{}, err
	}
	if token.Access == "" || token.Refresh == "" || token.Expires <= 0 {
		return Credential{}, fmt.Errorf("invalid anthropic OAuth token response")
	}
	return Credential{Type: "oauth", Access: token.Access, Refresh: token.Refresh, Expires: time.Now().UnixMilli() + token.Expires*1000 - 5*60*1000}, nil
}

func randomPKCE() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func pkceChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func callbackHost() string {
	if value := strings.TrimSpace(os.Getenv("THEOSES_OAUTH_CALLBACK_HOST")); value != "" {
		return value
	}
	return "127.0.0.1"
}
