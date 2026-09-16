package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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
	return LoginGitHubCopilotForDomain(ctx, "github.com", "", notify)
}

// LoginGitHubCopilotForDomain performs device login against github.com or a
// validated GitHub Enterprise domain.
func LoginGitHubCopilotForDomain(ctx context.Context, domain, enterpriseURL string, notify func(string)) (Credential, error) {
	domain, err := normalizeCopilotDomain(domain)
	if err != nil {
		return Credential{}, err
	}
	return loginGitHubCopilot(ctx, &http.Client{Timeout: 10 * time.Second}, notify, "https://"+domain+"/login/device/code", "https://"+domain+"/login/oauth/access_token", "https://api."+domain+"/copilot_internal/v2/token", "https://api."+domain, enterpriseURL)
}

func loginGitHubCopilot(ctx context.Context, client *http.Client, notify func(string), deviceURL, accessURL, tokenURL, modelsBaseURL, enterpriseURL string) (Credential, error) {
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
		credential, err := copilotToken(ctx, client, tokenURL, githubToken)
		if err != nil {
			return Credential{}, err
		}
		credential.EnterpriseURL = enterpriseURL
		models, err := fetchCopilotModels(ctx, client, modelsBaseURL, credential.Access)
		if err != nil {
			return Credential{}, err
		}
		credential.AvailableModelIDs = models
		return credential, nil
	}
}

func normalizeCopilotDomain(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "github.com", nil
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("invalid GitHub Enterprise domain")
	}
	if parsed.Scheme == "" {
		parsed, err = url.Parse("https://" + value)
	}
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Path != "" && parsed.Path != "/" {
		return "", fmt.Errorf("invalid GitHub Enterprise domain")
	}
	return parsed.Hostname(), nil
}

func NormalizeGitHubCopilotDomain(value string) (string, error) {
	return normalizeCopilotDomain(value)
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

func RefreshGitHubCopilot(ctx context.Context, refresh string) (Credential, error) {
	return RefreshGitHubCopilotForDomain(ctx, refresh, "github.com", "")
}

func RefreshGitHubCopilotForDomain(ctx context.Context, refresh, domain, enterpriseURL string) (Credential, error) {
	domain, err := normalizeCopilotDomain(domain)
	if err != nil {
		return Credential{}, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	credential, err := copilotToken(ctx, client, "https://api."+domain+"/copilot_internal/v2/token", refresh)
	if err != nil {
		return Credential{}, err
	}
	credential.EnterpriseURL = enterpriseURL
	models, err := fetchCopilotModels(ctx, client, "https://api."+domain, credential.Access)
	if err != nil {
		return Credential{}, err
	}
	credential.AvailableModelIDs = models
	return credential, nil
}

func fetchCopilotModels(ctx context.Context, client *http.Client, baseURL, token string) ([]string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	for name, value := range copilotHeaders {
		request.Header.Set(name, value)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("github copilot model catalog returned %s", response.Status)
	}
	var payload struct {
		Data []struct {
			ID           string `json:"id"`
			Picker       bool   `json:"model_picker_enabled"`
			Capabilities struct {
				Supports struct {
					ToolCalls *bool `json:"tool_calls"`
				} `json:"supports"`
			} `json:"capabilities"`
			Policy struct {
				State string `json:"state"`
			} `json:"policy"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	var picker, enabled []string
	for _, model := range payload.Data {
		if model.ID == "" || model.Capabilities.Supports.ToolCalls != nil && !*model.Capabilities.Supports.ToolCalls || model.Policy.State == "disabled" {
			continue
		}
		if model.Picker {
			picker = append(picker, model.ID)
		} else if model.Policy.State == "enabled" {
			enabled = append(enabled, model.ID)
		}
	}
	if len(picker) > 0 {
		return picker, nil
	}
	return enabled, nil
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
