// Package auth owns Yen's provider credential boundary.
package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Credential struct {
	Type    string            `json:"type"`
	Key     string            `json:"key,omitempty"`
	Access  string            `json:"access,omitempty"`
	Refresh string            `json:"refresh,omitempty"`
	Expires int64             `json:"expires,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

type Info struct {
	Provider string `json:"providerId"`
	Type     string `json:"type"`
}

type Store struct {
	path string
	mu   sync.Mutex
}

func Open(path string) *Store { return &Store{path: path} }

func (s *Store) Read(provider string) (Credential, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	credentials, err := s.load()
	if err != nil {
		return Credential{}, false, err
	}
	credential, ok := credentials[provider]
	return credential, ok, nil
}

func (s *Store) List() ([]Info, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	credentials, err := s.load()
	if err != nil {
		return nil, err
	}
	result := make([]Info, 0, len(credentials))
	for provider, credential := range credentials {
		result = append(result, Info{Provider: provider, Type: credential.Type})
	}
	return result, nil
}

func (s *Store) Modify(provider string, fn func(*Credential) (*Credential, error)) (Credential, error) {
	if provider == "" {
		return Credential{}, errors.New("provider is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	credentials, err := s.load()
	if err != nil {
		return Credential{}, err
	}
	current, exists := credentials[provider]
	if !exists {
		current = Credential{}
	}
	next, err := fn(func() *Credential {
		if !exists {
			return nil
		}
		copy := current
		return &copy
	}())
	if err != nil {
		return Credential{}, err
	}
	if next == nil {
		delete(credentials, provider)
	} else {
		if next.Type != "api_key" && next.Type != "oauth" {
			return Credential{}, fmt.Errorf("unsupported credential type %q", next.Type)
		}
		credentials[provider] = *next
	}
	if err := s.save(credentials); err != nil {
		return Credential{}, err
	}
	if next == nil {
		return Credential{}, nil
	}
	return *next, nil
}

func (s *Store) Delete(provider string) error {
	_, err := s.Modify(provider, func(*Credential) (*Credential, error) { return nil, nil })
	return err
}

func (s *Store) load() (map[string]Credential, error) {
	result := make(map[string]Credential)
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return result, nil
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) save(credentials map[string]Credential) error {
	data, err := json.MarshalIndent(credentials, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.path), ".auth-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, s.path)
}
