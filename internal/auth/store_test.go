package auth

import (
	"os"
	"path/filepath"
	"strings"
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
