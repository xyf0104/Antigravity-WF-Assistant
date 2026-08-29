package totp

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/scrypt"
)

// ExportEncrypted returns a password-encrypted portable backup. It is an
// explicitly requested operation: normal diagnostics and local metadata never
// include a secret or a generated code.
func (vault *Vault) ExportEncrypted(password string) ([]byte, error) {
	if vault == nil {
		return nil, errors.New("TOTP vault is unavailable")
	}
	password = strings.TrimSpace(password)
	if len(password) < 10 || len(password) > 1024 {
		return nil, errors.New("export password must contain at least 10 characters")
	}
	vault.mu.Lock()
	defer vault.mu.Unlock()
	document, err := vault.loadLocked()
	if err != nil {
		return nil, err
	}
	export := exportDocument{Version: exportVersion, Entries: make([]exportEntry, 0, len(document.Entries))}
	for _, entry := range document.Entries {
		secret, err := vault.secrets.Get(ServiceName, entry.ID)
		if err != nil {
			return nil, fmt.Errorf("read a TOTP secret for encrypted export: %w", err)
		}
		export.Entries = append(export.Entries, exportEntry{Entry: entry, Secret: secret})
	}
	plain, err := json.Marshal(export)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	key, err := scrypt.Key([]byte(password), salt, 32768, 8, 1, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nil, nonce, plain, []byte("XIASS Tools TOTP export v1"))
	encoded, err := json.MarshalIndent(encryptedExport{
		Version: exportVersion, KDF: "scrypt-N32768-r8-p1-AES-256-GCM",
		Salt: base64.RawStdEncoding.EncodeToString(salt), Nonce: base64.RawStdEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawStdEncoding.EncodeToString(ciphertext),
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}
