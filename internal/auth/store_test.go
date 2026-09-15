package auth

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestStorePersistsCredentialsAtomicallyAndListsWithoutSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	store := Open(path)
	if _, err := store.Modify("openrouter", func(_ *Credential) (*Credential, error) {
		return &Credential{Type: "api_key", Key: "secret", Env: map[string]string{"account": "yen"}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	credential, ok, err := Open(path).Read("openrouter")
	if err != nil || !ok || credential.Key != "secret" || credential.Env["account"] != "yen" {
		t.Fatalf("credential=%#v ok=%v err=%v", credential, ok, err)
	}
	entries, err := store.List()
	if err != nil || len(entries) != 1 || entries[0].Provider != "openrouter" || entries[0].Type != "api_key" {
		t.Fatalf("entries=%#v err=%v", entries, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) == "" || !strings.Contains(string(data), "secret") {
		t.Fatalf("stored data err=%v data=%s", err, data)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v err=%v", info.Mode().Perm(), err)
	}
	if err := store.Delete("openrouter"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Read("openrouter"); err != nil || ok {
		t.Fatalf("deleted credential ok=%v err=%v", ok, err)
	}
}

func TestStoreSerializesIndependentProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	a, b := Open(path), Open(path)
	var wait sync.WaitGroup
	var errorsFound []error
	var mu sync.Mutex
	for index, store := range []*Store{a, b} {
		wait.Add(1)
		go func(store *Store, index int) {
			defer wait.Done()
			_, err := store.Modify(fmt.Sprintf("provider-%d", index), func(_ *Credential) (*Credential, error) {
				return &Credential{Type: "api_key", Key: fmt.Sprintf("key-%d", index)}, nil
			})
			if err != nil {
				mu.Lock()
				errorsFound = append(errorsFound, err)
				mu.Unlock()
			}
		}(store, index)
	}
	wait.Wait()
	if len(errorsFound) != 0 {
		t.Fatal(errorsFound)
	}
	entries, err := Open(path).List()
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries=%#v err=%v", entries, err)
	}
}
