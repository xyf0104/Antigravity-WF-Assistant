package main

import (
	"os"
	"path/filepath"
	"testing"

	"antigravity-byok/internal/totp"
)

type appTOTPStore struct{ values map[string]string }

func (store *appTOTPStore) Set(_ string, account, secret string) error {
	if store.values == nil {
		store.values = map[string]string{}
	}
	store.values[account] = secret
	return nil
}
func (store *appTOTPStore) Get(_ string, account string) (string, error) {
	return store.values[account], nil
}
func (store *appTOTPStore) Delete(_ string, account string) error {
	delete(store.values, account)
	return nil
}

func TestAppTOTPStatusAndCodeRemainSecretFree(t *testing.T) {
	vault, err := totp.NewWithStore(t.TempDir(), &appTOTPStore{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	app := &App{totpVault: vault}
	status := app.AddTOTPEntry(totp.ImportInput{Secret: "JBSWY3DPEHPK3PXP", Label: "XIASS"})
	if !status.OK || len(status.Entries) != 1 {
		t.Fatalf("status = %#v", status)
	}
	if status.Entries[0].ID == "" || status.Entries[0].Label != "XIASS" {
		t.Fatalf("entry = %#v", status.Entries[0])
	}
	code := app.GenerateTOTPCode(status.Entries[0].ID)
	if !code.OK || len(code.Code.Value) != 6 {
		t.Fatalf("code = %#v", code)
	}
}

func TestWriteNewSensitiveExportDoesNotOverwriteExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup.json")
	if err := writeNewSensitiveExport(path, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := writeNewSensitiveExport(path, []byte("second")); err == nil {
		t.Fatal("existing export was overwritten")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "first" {
		t.Fatalf("data = %q, %v", data, err)
	}
}
