package totp

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryStore struct {
	mu        sync.Mutex
	values    map[string]string
	setErr    error
	getErr    error
	deleteErr error
}

func (store *memoryStore) Set(_ string, account, secret string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.setErr != nil {
		return store.setErr
	}
	if store.values == nil {
		store.values = map[string]string{}
	}
	store.values[account] = secret
	return nil
}

func (store *memoryStore) Get(_ string, account string) (string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.getErr != nil {
		return "", store.getErr
	}
	value, ok := store.values[account]
	if !ok {
		return "", errors.New("not found")
	}
	return value, nil
}

func (store *memoryStore) Delete(_ string, account string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.deleteErr != nil {
		return store.deleteErr
	}
	delete(store.values, account)
	return nil
}

func TestVaultAddsMetadataWithoutPersistingSecret(t *testing.T) {
	store := &memoryStore{}
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	vault, err := NewWithStore(t.TempDir(), store, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	entry, err := vault.Add(ImportInput{URI: "otpauth://totp/XIASS:alice?secret=JBSWY3DPEHPK3PXP&issuer=XIASS"})
	if err != nil {
		t.Fatal(err)
	}
	if entry.ID == "" || entry.Label != "XIASS:alice" || entry.Issuer != "XIASS" || entry.Account != "alice" {
		t.Fatalf("entry = %#v", entry)
	}
	entries, err := vault.List()
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries = %#v, %v", entries, err)
	}
	data, exists, err := readRegularFile(vault.index)
	if err != nil || !exists || !strings.Contains(string(data), entry.ID) || strings.Contains(string(data), "JBSWY3DPEHPK3PXP") {
		t.Fatalf("metadata data = %q, exists=%t, %v", data, exists, err)
	}
	if store.values[entry.ID] != "JBSWY3DPEHPK3PXP" {
		t.Fatal("secret was not stored in secure-store seam")
	}
}

func TestVaultGeneratesRFC6238Code(t *testing.T) {
	store := &memoryStore{}
	vault, err := NewWithStore(t.TempDir(), store, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := vault.Add(ImportInput{Secret: "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", Label: "RFC", Algorithm: "SHA1", Digits: 8, Period: 30})
	if err != nil {
		t.Fatal(err)
	}
	code, err := vault.Generate(entry.ID, time.Unix(59, 0))
	if err != nil {
		t.Fatal(err)
	}
	if code.Value != "94287082" {
		t.Fatalf("code = %q", code.Value)
	}
	if !code.ValidFrom.Equal(time.Unix(30, 0).UTC()) || !code.ValidUntil.Equal(time.Unix(60, 0).UTC()) {
		t.Fatalf("window = %#v", code)
	}
}

func TestVaultRejectsInvalidURIAndKeepsIndexClean(t *testing.T) {
	vault, err := NewWithStore(t.TempDir(), &memoryStore{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Add(ImportInput{URI: "otpauth://hotp/example?secret=JBSWY3DPEHPK3PXP"}); err == nil {
		t.Fatal("HOTP URI accepted")
	}
	entries, err := vault.List()
	if err != nil || len(entries) != 0 {
		t.Fatalf("entries = %#v, %v", entries, err)
	}
}

func TestVaultDeleteRestoresMetadataWhenSecretDeletionFails(t *testing.T) {
	store := &memoryStore{}
	vault, err := NewWithStore(t.TempDir(), store, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := vault.Add(ImportInput{Secret: "JBSWY3DPEHPK3PXP", Label: "test"})
	if err != nil {
		t.Fatal(err)
	}
	store.deleteErr = errors.New("credential store unavailable")
	if err := vault.Delete(entry.ID); err == nil {
		t.Fatal("delete unexpectedly succeeded")
	}
	entries, err := vault.List()
	if err != nil || len(entries) != 1 || entries[0].ID != entry.ID {
		t.Fatalf("metadata was not restored: %#v, %v", entries, err)
	}
}

func TestVaultEncryptedExportDoesNotExposeSecretInCleartext(t *testing.T) {
	store := &memoryStore{}
	vault, err := NewWithStore(t.TempDir(), store, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Add(ImportInput{Secret: "JBSWY3DPEHPK3PXP", Label: "export"}); err != nil {
		t.Fatal(err)
	}
	data, err := vault.ExportEncrypted("long-enough-export-password")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "JBSWY3DPEHPK3PXP") || !strings.Contains(string(data), "ciphertext") {
		t.Fatalf("encrypted export = %s", data)
	}
}
