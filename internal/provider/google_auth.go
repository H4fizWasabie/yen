package provider

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type vertexServiceAccount struct {
	Type        string `json:"type"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

func vertexServiceAccountSource(path string) func(context.Context) (string, error) {
	var cacheMu sync.Mutex
	var cachedToken string
	var cachedExpiry time.Time

	return func(ctx context.Context) (string, error) {
		cacheMu.Lock()
		if cachedToken != "" && time.Now().Before(cachedExpiry) {
			token := cachedToken
			cacheMu.Unlock()
			return token, nil
		}
		cacheMu.Unlock()

		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		var credentials vertexServiceAccount
		if err := json.Unmarshal(data, &credentials); err != nil {
			return "", fmt.Errorf("vertex credentials: %w", err)
		}
		if credentials.Type != "service_account" || credentials.ClientEmail == "" || credentials.PrivateKey == "" {
			return "", errors.New("vertex credentials are not a service account")
		}
		if credentials.TokenURI == "" {
			credentials.TokenURI = "https://oauth2.googleapis.com/token"
		}
		block, _ := pem.Decode([]byte(credentials.PrivateKey))
		if block == nil {
			return "", errors.New("vertex credentials contain no private key")
		}
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		}
		privateKey, ok := key.(*rsa.PrivateKey)
		if err != nil || !ok {
			return "", errors.New("vertex credentials contain an invalid RSA private key")
		}
		now := time.Now().Unix()
		encode := func(value any) string {
			data, _ := json.Marshal(value)
			return base64.RawURLEncoding.EncodeToString(data)
		}
		unsigned := encode(map[string]string{"alg": "RS256", "typ": "JWT"}) + "." + encode(map[string]any{
			"iss": credentials.ClientEmail, "scope": "https://www.googleapis.com/auth/cloud-platform",
			"aud": credentials.TokenURI, "iat": now, "exp": now + 3600,
		})
		digest := sha256.Sum256([]byte(unsigned))
		signature, err := rsa.SignPKCS1v15(nil, privateKey, crypto.SHA256, digest[:])
		if err != nil {
			return "", err
		}
		assertion := unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
		form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {assertion}}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, credentials.TokenURI, strings.NewReader(form.Encode()))
		if err != nil {
			return "", err
		}
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return "", err
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(response.Body, 16<<10))
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return "", fmt.Errorf("vertex token exchange returned %s: %s", response.Status, strings.TrimSpace(string(body)))
		}
		var token struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   int64  `json:"expires_in"`
		}
		if err := json.Unmarshal(body, &token); err != nil || token.AccessToken == "" {
			return "", errors.New("vertex token exchange returned no access token")
		}
		cacheMu.Lock()
		cachedToken = token.AccessToken
		cachedExpiry = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
		cacheMu.Unlock()
		return token.AccessToken, nil
	}
}
