package diagnostics

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExportCreatesRedactedLatestSessionArchive(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "Users", "customer")
	storageDir := filepath.Join(home, ".antigravity-wf")
	logRoot := filepath.Join(home, "AppData", "Roaming", "Antigravity", "logs")
	oldSession := filepath.Join(logRoot, "20260810T090000")
	newSession := filepath.Join(logRoot, "20260811T100000")
	githubSecret := "ghp_" + strings.Repeat("a", 32)
	googleSecret := "AI" + "za" + strings.Repeat("B", 32)
	apiSecret := "sk-" + "test-secret-1234567890"
	jwtValue := "eyJ" + "abcdefghijklmnop.qrstuvwxyz12.abcdefghijklmno"

	writeDiagnosticFixture(t, filepath.Join(storageDir, applicationLogName), strings.Join([]string{
		`normal application event`,
		`Authorization: Bearer bearer-secret-value`,
		`OAuth code=manual-oauth-code-secret`,
		`Args: --csrf_token csrf-secret-one --extension_server_csrf_token csrf-secret-two`,
		`callback=https://localhost/callback?code=query-oauth-code-secret&state=ok`,
		`github=` + githubSecret,
		`google=` + googleSecret,
		`jwt=` + jwtValue,
		`home=` + home,
	}, "\n"))
	writeDiagnosticFixture(t, filepath.Join(storageDir, "proxy-trace.jsonl"), strings.Join([]string{
		`{"event":"upstream-error","api_key":"` + apiSecret + `","refresh_token":"refresh-secret","client_secret":"client-secret"}`,
		`{"Authorization":["Bearer array-bearer-secret"],"b64_json":"` + strings.Repeat("A", 512) + `"}`,
	}, "\n"))
	writeDiagnosticFixture(t, filepath.Join(storageDir, "proxy_runtime.json"), `{"schemaVersion":1,"port":50999}`)
	writeDiagnosticFixture(t, filepath.Join(storageDir, "custom_models.json"), `{"apiKey":"must-never-be-exported"}`)
	writeDiagnosticFixture(t, filepath.Join(storageDir, "upstream_accounts.json"), `{"refresh_token":"must-never-be-exported"}`)

	writeDiagnosticFixture(t, filepath.Join(oldSession, "ls-main.log"), "old-session-marker")
	writeDiagnosticFixture(t, filepath.Join(newSession, "ls-main.log"), "latest-session-marker\nx-api-key: latest-secret-key\npath="+home)
	largeLog := bytes.Repeat([]byte("ordinary diagnostic line 0123456789\n"), 70000)
	largeLog = append(largeLog, []byte("latest-renderer-tail-marker\n")...)
	writeDiagnosticBytesFixture(t, filepath.Join(newSession, "window1", "renderer.log"), largeLog)
	writeDiagnosticFixture(t, filepath.Join(newSession, "terminal.log"), "terminal-content-must-not-be-exported")

	oldTime := time.Now().Add(-2 * time.Hour)
	newTime := time.Now().Add(-time.Minute)
	if err := os.Chtimes(oldSession, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newSession, newTime, newTime); err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(root, "support.zip")
	err := Export(destination, storageDir, Options{
		Version:             "1.5.5",
		HomeDir:             home,
		AntigravityLogRoots: []string{logRoot},
		Snapshot: map[string]any{
			"proxyListening": true,
			"path":           home,
			"authorization":  "Bearer summary-secret",
		},
	})
	if err != nil {
		t.Fatalf("export diagnostics: %v", err)
	}

	entries := readDiagnosticArchive(t, destination)
	for _, required := range []string{
		"diagnostic-summary.json",
		"wf/wf-assistant.log",
		"wf/proxy-trace.jsonl",
		"wf/proxy_runtime.json",
	} {
		if _, exists := entries[required]; !exists {
			t.Fatalf("archive missing %s; entries=%v", required, archiveEntryNames(entries))
		}
	}

	var latestLSName, rendererName string
	for name := range entries {
		if strings.Contains(name, "custom_models") || strings.Contains(name, "upstream_accounts") || strings.HasSuffix(name, "/terminal.log") {
			t.Fatalf("private or unsupported file was exported: %s", name)
		}
		if strings.HasSuffix(name, "/20260810T090000/ls-main.log") {
			t.Fatalf("older Antigravity session was exported: %s", name)
		}
		if strings.HasSuffix(name, "/20260811T100000/ls-main.log") {
			latestLSName = name
		}
		if strings.HasSuffix(name, "/20260811T100000/window1/renderer.log") {
			rendererName = name
		}
	}
	if latestLSName == "" || rendererName == "" {
		t.Fatalf("latest Antigravity logs missing; entries=%v", archiveEntryNames(entries))
	}
	if !strings.Contains(string(entries[latestLSName]), "latest-session-marker") {
		t.Fatalf("latest session content missing from %s", latestLSName)
	}
	if len(entries[rendererName]) > int(maxExportedFileBytes) {
		t.Fatalf("renderer log exceeds per-file limit: %d", len(entries[rendererName]))
	}
	if !bytes.HasPrefix(entries[rendererName], []byte("[earlier content truncated]\n")) || !bytes.Contains(entries[rendererName], []byte("latest-renderer-tail-marker")) {
		t.Fatalf("large renderer log was not safely tail-truncated")
	}

	combined := string(bytes.Join(archiveEntryValues(entries), []byte("\n")))
	for _, secret := range []string{
		"bearer-secret-value",
		"manual-oauth-code-secret",
		"csrf-secret-one",
		"csrf-secret-two",
		"query-oauth-code-secret",
		githubSecret,
		googleSecret,
		jwtValue,
		apiSecret,
		"refresh-secret",
		"client-secret",
		"array-bearer-secret",
		"latest-secret-key",
		"summary-secret",
		strings.Repeat("A", 512),
		home,
		"old-session-marker",
		"terminal-content-must-not-be-exported",
		"must-never-be-exported",
	} {
		if strings.Contains(combined, secret) {
			t.Fatalf("diagnostic archive leaked %q", secret)
		}
	}
	if !strings.Contains(combined, "[REDACTED]") || !strings.Contains(combined, "~") {
		t.Fatalf("archive did not contain expected redaction markers")
	}
}

func TestApplicationLogWriterRotatesAtBound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, applicationLogName)
	if err := os.WriteFile(path, []byte("old-log"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, maxApplicationLogBytes-4); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}

	logMu.Lock()
	previousFile, previousDir, previousHome := logFile, logStorageDir, logHomeDir
	logFile, logStorageDir, logHomeDir = file, dir, ""
	logMu.Unlock()
	t.Cleanup(func() {
		logMu.Lock()
		if logFile != nil && logFile != previousFile {
			_ = logFile.Close()
		}
		logFile, logStorageDir, logHomeDir = previousFile, previousDir, previousHome
		logMu.Unlock()
	})

	if _, err := (applicationLogWriter{}).Write([]byte("new-record\n")); err != nil {
		t.Fatalf("write rotating log: %v", err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("rotated log missing: %v", err)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != "new-record\n" {
		t.Fatalf("current log = %q", current)
	}
}

func writeDiagnosticFixture(t *testing.T, path, value string) {
	t.Helper()
	writeDiagnosticBytesFixture(t, path, []byte(value))
}

func writeDiagnosticBytesFixture(t *testing.T, path string, value []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, value, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readDiagnosticArchive(t *testing.T, path string) map[string][]byte {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	entries := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		stream, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(stream)
		_ = stream.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[file.Name] = data
	}
	return entries
}

func archiveEntryNames(entries map[string][]byte) []string {
	result := make([]string, 0, len(entries))
	for name := range entries {
		result = append(result, name)
	}
	return result
}

func archiveEntryValues(entries map[string][]byte) [][]byte {
	result := make([][]byte, 0, len(entries))
	for _, value := range entries {
		result = append(result, value)
	}
	return result
}
