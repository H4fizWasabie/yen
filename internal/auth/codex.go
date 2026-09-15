package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	codexClientID       = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexAuthBase       = "https://auth.openai.com"
	codexDeviceUserCode = codexAuthBase + "/api/accounts/deviceauth/usercode"
	codexDeviceToken    = codexAuthBase + "/api/accounts/deviceauth/token"
	codexTokenURL       = codexAuthBase + "/oauth/token"
	codexDeviceURL      = codexAuthBase + "/codex/device"
)

type codexDeviceAuth struct {
	ID       string
	UserCode string
	Interval float64
}

// LoginOpenAICodexDevice performs the headless Codex device login flow.
// notify receives the URL and user code the operator must enter.
func LoginOpenAICodexDevice(ctx context.Context, notify func(string)) (Credential, error) {
	return loginOpenAICodexDevice(ctx, http.DefaultClient, notify, codexDeviceUserCode, codexDeviceToken, codexTokenURL, codexDeviceURL)
}

// RefreshOpenAICodex exchanges a stored refresh token for a new OAuth credential.
func RefreshOpenAICodex(ctx context.Context, refresh string) (Credential, error) {
	return refreshOpenAICodex(ctx, &http.Client{Timeout: 10 * time.Second}, codexTokenURL, refresh)
}

func loginOpenAICodexDevice(ctx context.Context, client *http.Client, notify func(string), userCodeURL, deviceTokenURL, tokenURL, verificationURL string) (Credential, error) {
	device, err := requestCodexDevice(ctx, client, userCodeURL)
	if err != nil {
		return Credential{}, err
	}
	if notify != nil {
		notify(fmt.Sprintf("Open %s and enter %s", verificationURL, device.UserCode))
	}
	deadline := time.NewTimer(15 * time.Minute)
	defer deadline.Stop()
	for {
		select {
		case <-ctx.Done():
			return Credential{}, ctx.Err()
		case <-deadline.C:
			return Credential{}, fmt.Errorf("openai codex device login timed out")
		default:
		}
		if device.Interval > 0 {
			timer := time.NewTimer(time.Duration(device.Interval * float64(time.Second)))
			select {
			case <-ctx.Done():
				timer.Stop()
				return Credential{}, ctx.Err()
			case <-timer.C:
			}
		}
		response, err := postJSON(ctx, client, deviceTokenURL, map[string]string{"device_auth_id": device.ID, "user_code": device.UserCode})
		if err != nil {
			return Credential{}, err
		}
		if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusNotFound {
			response.Body.Close()
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
			response.Body.Close()
			var pending struct {
				Error json.RawMessage `json:"error"`
			}
			var errorCode string
			if json.Unmarshal(body, &pending) == nil {
				_ = json.Unmarshal(pending.Error, &errorCode)
				var nested struct {
					Code string `json:"code"`
				}
				if errorCode == "" {
					_ = json.Unmarshal(pending.Error, &nested)
					errorCode = nested.Code
				}
			}
			if errorCode == "deviceauth_authorization_pending" {
				continue
			}
			return Credential{}, fmt.Errorf("openai codex device auth failed with status %s", response.Status)
		}
		var authorization struct {
			Code     string `json:"authorization_code"`
			Verifier string `json:"code_verifier"`
		}
		err = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&authorization)
		response.Body.Close()
		if err != nil || authorization.Code == "" || authorization.Verifier == "" {
			return Credential{}, fmt.Errorf("invalid openai codex device auth response")
		}
		return exchangeCodexCode(ctx, client, tokenURL, authorization.Code, authorization.Verifier)
	}
}

func requestCodexDevice(ctx context.Context, client *http.Client, endpoint string) (codexDeviceAuth, error) {
	response, err := postJSON(ctx, client, endpoint, map[string]string{"client_id": codexClientID})
	if err != nil {
		return codexDeviceAuth{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return codexDeviceAuth{}, fmt.Errorf("openai codex device code request failed with status %s", response.Status)
	}
	var payload struct {
		ID       string          `json:"device_auth_id"`
		UserCode string          `json:"user_code"`
		Interval json.RawMessage `json:"interval"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&payload); err != nil {
		return codexDeviceAuth{}, err
	}
	var interval float64
	if len(payload.Interval) == 0 {
		return codexDeviceAuth{}, fmt.Errorf("invalid openai codex device code response")
	}
	if err := json.Unmarshal(payload.Interval, &interval); err != nil {
		var value string
		if json.Unmarshal(payload.Interval, &value) != nil {
			return codexDeviceAuth{}, fmt.Errorf("invalid openai codex device code response")
		}
		interval, err = strconv.ParseFloat(value, 64)
		if err != nil {
			return codexDeviceAuth{}, fmt.Errorf("invalid openai codex device code response")
		}
	}
	if payload.ID == "" || payload.UserCode == "" || interval < 0 {
		return codexDeviceAuth{}, fmt.Errorf("invalid openai codex device code response")
	}
	return codexDeviceAuth{ID: payload.ID, UserCode: payload.UserCode, Interval: interval}, nil
}

func exchangeCodexCode(ctx context.Context, client *http.Client, endpoint, code, verifier string) (Credential, error) {
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {codexClientID}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {codexAuthBase + "/deviceauth/callback"}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return Credential{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.Do(request)
	if err != nil {
		return Credential{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Credential{}, fmt.Errorf("openai codex token exchange failed with status %s", response.Status)
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
		return Credential{}, fmt.Errorf("invalid openai codex token response")
	}
	return Credential{Type: "oauth", Access: token.Access, Refresh: token.Refresh, Expires: time.Now().UnixMilli() + token.Expires*1000}, nil
}

func refreshOpenAICodex(ctx context.Context, client *http.Client, endpoint, refresh string) (Credential, error) {
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {codexClientID}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return Credential{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.Do(request)
	if err != nil {
		return Credential{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Credential{}, fmt.Errorf("openai codex token refresh failed with status %s", response.Status)
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
		return Credential{}, fmt.Errorf("invalid openai codex token refresh response")
	}
	return Credential{Type: "oauth", Access: token.Access, Refresh: token.Refresh, Expires: time.Now().UnixMilli() + token.Expires*1000}, nil
}

func postJSON(ctx context.Context, client *http.Client, endpoint string, payload any) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	return client.Do(request)
}
