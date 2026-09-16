package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	copilotClientID       = "Iv1.b507a08c87ecfe98"
	copilotDeviceCodeURL  = "https://github.com/login/device/code"
	copilotAccessTokenURL = "https://github.com/login/oauth/access_token"
	copilotTokenURL       = "https://api.github.com/copilot_internal/v2/token"
)

var copilotHeaders = map[string]string{
	"User-Agent":             "GitHubCopilotChat/0.35.0",
	"Editor-Version":         "vscode/1.107.0",
	"Editor-Plugin-Version":  "copilot-chat/0.35.0",
	"Copilot-Integration-Id": "vscode-chat",
}

// LoginGitHubCopilot performs the default github.com device login flow.
// notify receives the verification URL and user code.
func LoginGitHubCopilot(ctx context.Context, notify func(string)) (Credential, error) {
	return loginGitHubCopilot(ctx, &http.Client{Timeout: 10 * time.Second}, notify, copilotDeviceCodeURL, copilotAccessTokenURL, copilotTokenURL)
}

func loginGitHubCopilot(ctx context.Context, client *http.Client, notify func(string), deviceURL, accessURL, tokenURL string) (Credential, error) {
	device, err := copilotDeviceCode(ctx, client, deviceURL)
	if err != nil {
		return Credential{}, err
	}
	if notify != nil {
		notify(fmt.Sprintf("Open %s and enter %s", device.VerificationURL, device.UserCode))
	}
	deadline := time.NewTimer(time.Duration(device.ExpiresIn) * time.Second)
	defer deadline.Stop()
	for {
		timer := time.NewTimer(time.Duration(device.Interval) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Credential{}, ctx.Err()
		case <-deadline.C:
			timer.Stop()
			return Credential{}, fmt.Errorf("github copilot device login timed out")
		case <-timer.C:
		}
		githubToken, pending, err := copilotAccessToken(ctx, client, accessURL, device.Code)
		if err != nil {
			return Credential{}, err
		}
		if pending {
			continue
		}
		return copilotToken(ctx, client, tokenURL, githubToken)
	}
}

type copilotDevice struct {
	Code            string
	UserCode        string
	VerificationURL string
	Interval        int
	ExpiresIn       int
}

func copilotDeviceCode(ctx context.Context, client *http.Client, endpoint string) (copilotDevice, error) {
	form := url.Values{"client_id": {copilotClientID}, "scope": {"read:user"}}
	response, err := copilotFormPost(ctx, client, endpoint, form, true)
	if err != nil {
		return copilotDevice{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return copilotDevice{}, fmt.Errorf("github device code request failed with status %s", response.Status)
	}
	var payload struct {
		Code            string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURL string `json:"verification_uri"`
		Interval        int    `json:"interval"`
		ExpiresIn       int    `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&payload); err != nil {
		return copilotDevice{}, err
	}
	if payload.Code == "" || payload.UserCode == "" || payload.VerificationURL == "" || payload.Interval < 0 || payload.ExpiresIn <= 0 {
		return copilotDevice{}, fmt.Errorf("invalid github copilot device code response")
	}
	verification, err := url.Parse(payload.VerificationURL)
	if err != nil || (verification.Scheme != "https" && verification.Scheme != "http") || verification.Host == "" {
		return copilotDevice{}, fmt.Errorf("untrusted github copilot verification URL")
	}
	return copilotDevice{Code: payload.Code, UserCode: payload.UserCode, VerificationURL: verification.String(), Interval: payload.Interval, ExpiresIn: payload.ExpiresIn}, nil
}

func copilotAccessToken(ctx context.Context, client *http.Client, endpoint, deviceCode string) (string, bool, error) {
	form := url.Values{"client_id": {copilotClientID}, "device_code": {deviceCode}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}
	response, err := copilotFormPost(ctx, client, endpoint, form, false)
	if err != nil {
		return "", false, err
	}
	defer response.Body.Close()
	var payload struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&payload); err != nil {
		return "", false, err
	}
	if payload.AccessToken != "" {
		return payload.AccessToken, false, nil
	}
	if payload.Error == "authorization_pending" {
		return "", true, nil
	}
	if payload.Error == "slow_down" {
		return "", true, nil
	}
	if payload.Error != "" {
		return "", false, fmt.Errorf("github device login failed: %s", payload.Error)
	}
	return "", false, fmt.Errorf("invalid github access token response")
}

func copilotToken(ctx context.Context, client *http.Client, endpoint, githubToken string) (Credential, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Credential{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+githubToken)
	for name, value := range copilotHeaders {
		request.Header.Set(name, value)
	}
	response, err := client.Do(request)
	if err != nil {
		return Credential{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Credential{}, fmt.Errorf("github copilot token request failed with status %s", response.Status)
	}
	var payload struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&payload); err != nil {
		return Credential{}, err
	}
	if payload.Token == "" || payload.ExpiresAt <= 0 {
		return Credential{}, fmt.Errorf("invalid github copilot token response")
	}
	return Credential{Type: "oauth", Access: payload.Token, Refresh: githubToken, Expires: payload.ExpiresAt*1000 - 5*60*1000}, nil
}

func copilotFormPost(ctx context.Context, client *http.Client, endpoint string, form url.Values, acceptJSON bool) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if acceptJSON {
		request.Header.Set("Accept", "application/json")
	}
	for name, value := range copilotHeaders {
		request.Header.Set(name, value)
	}
	return client.Do(request)
}
