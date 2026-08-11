// Package diagnostics owns credential-safe local logging and support exports.
package diagnostics

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	applicationLogName      = "wf-assistant.log"
	maxApplicationLogBytes  = int64(4 << 20)
	maxExportedFileBytes    = int64(2 << 20)
	maxAntigravityLogBytes  = int64(12 << 20)
	diagnosticArchiveFormat = 1
)

var (
	logMu           sync.Mutex
	logFile         *os.File
	logStorageDir   string
	logHomeDir      string
	jsonSecret      = regexp.MustCompile(`(?i)("(?:api[_-]?key|authorization|access[_-]?token|refresh[_-]?token|id[_-]?token|client[_-]?secret|password|cookie|set-cookie|credential)"\s*:\s*")[^"]*(")`)
	jsonArraySecret = regexp.MustCompile(`(?i)("(?:authorization|x-api-key|api-key|access[_-]?token|refresh[_-]?token|id[_-]?token|client[_-]?secret|password|cookie|set-cookie)"\s*:\s*\[\s*")[^"]*(")`)
	jsonImageData   = regexp.MustCompile(`(?i)("(?:b64_json|b64Json|image_base64|imageBase64|base64)"\s*:\s*")[^"]*(")`)
	headerSecret    = regexp.MustCompile(`(?i)((?:authorization|x-api-key|api-key|access[_ -]?token|refresh[_ -]?token|client[_ -]?secret|password|cookie|set-cookie)\s*[:=]\s*)(?:Bearer\s+)?[^\s,;]+`)
	oauthCodeSecret = regexp.MustCompile(`(?i)((?:oauth|authorization)\s+code\s*[:=]\s*)[^\s,;]+`)
	urlSecret       = regexp.MustCompile(`(?i)([?&](?:key|api_key|token|access_token|refresh_token|id_token|code|client_secret)=)[^&\s"']+`)
	knownSecret     = regexp.MustCompile(`\b(?:(?:sk|ghp|github_pat|xox[baprs])[-_A-Za-z0-9]{12,}|AIza[A-Za-z0-9_-]{20,})\b`)
	jwtSecret       = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{12,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)
	longBase64Data  = regexp.MustCompile(`\b[A-Za-z0-9+/]{256,}={0,2}\b`)
)

type applicationLogWriter struct{}

func (applicationLogWriter) Write(data []byte) (int, error) {
	logMu.Lock()
	defer logMu.Unlock()
	originalLength := len(data)
	if logFile == nil {
		return originalLength, nil
	}
	data = redact(data, logHomeDir)
	if int64(len(data)) > maxApplicationLogBytes {
		marker := []byte("[oversized log record truncated]\n")
		data = append(marker, data[len(data)-int(maxApplicationLogBytes)+len(marker):]...)
	}
	if info, err := logFile.Stat(); err == nil && info.Size()+int64(len(data)) > maxApplicationLogBytes {
		if err := rotateApplicationLogLocked(); err != nil {
			return 0, err
		}
	}
	if _, err := logFile.Write(data); err != nil {
		return 0, err
	}
	return originalLength, nil
}

type Options struct {
	Version             string
	Snapshot            any
	HomeDir             string
	AntigravityLogRoots []string
}

type archiveSummary struct {
	Format       int    `json:"format"`
	ExportedAt   string `json:"exportedAt"`
	AppVersion   string `json:"appVersion"`
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Snapshot     any    `json:"snapshot,omitempty"`
}

func Init(storageDir string) error {
	logMu.Lock()
	defer logMu.Unlock()
	if logFile != nil {
		return nil
	}
	if err := os.MkdirAll(storageDir, 0o700); err != nil {
		return err
	}
	logStorageDir = storageDir
	logHomeDir, _ = os.UserHomeDir()
	path := filepath.Join(storageDir, applicationLogName)
	if info, err := os.Stat(path); err == nil && info.Size() > maxApplicationLogBytes {
		previous := path + ".1"
		_ = os.Remove(previous)
		if err := os.Rename(path, previous); err != nil {
			return err
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_ = os.Chmod(path, 0o600)
	logFile = file
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.LUTC)
	log.SetOutput(io.MultiWriter(applicationLogWriter{}, os.Stderr))
	return nil
}

func rotateApplicationLogLocked() error {
	if logFile == nil || strings.TrimSpace(logStorageDir) == "" {
		return nil
	}
	path := filepath.Join(logStorageDir, applicationLogName)
	previous := path + ".1"
	if err := logFile.Close(); err != nil {
		return err
	}
	logFile = nil
	_ = os.Remove(previous)
	if err := os.Rename(path, previous); err != nil && !os.IsNotExist(err) {
		file, reopenErr := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if reopenErr == nil {
			logFile = file
		}
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_ = os.Chmod(path, 0o600)
	logFile = file
	return nil
}

func Export(destination, storageDir string, options Options) error {
	destination = strings.TrimSpace(destination)
	if destination == "" {
		return fmt.Errorf("诊断日志保存位置为空")
	}
	if !strings.EqualFold(filepath.Ext(destination), ".zip") {
		destination += ".zip"
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("无法创建诊断日志目录: %w", err)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("无法创建诊断日志: %w", err)
	}
	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(destination)
		}
	}()

	writer := zip.NewWriter(file)
	summary := archiveSummary{
		Format: diagnosticArchiveFormat, ExportedAt: time.Now().UTC().Format(time.RFC3339),
		AppVersion: strings.TrimSpace(options.Version), OS: goruntime.GOOS,
		Architecture: goruntime.GOARCH, Snapshot: options.Snapshot,
	}
	summaryData, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("无法生成诊断摘要: %w", err)
	}
	if err := addArchiveBytes(writer, "diagnostic-summary.json", boundTail(redact(summaryData, options.HomeDir), maxExportedFileBytes)); err != nil {
		return err
	}
	for _, name := range []string{applicationLogName + ".1", applicationLogName, "proxy-trace.jsonl", "proxy_runtime.json"} {
		path := filepath.Join(storageDir, name)
		if err := addSanitizedFile(writer, filepath.ToSlash(filepath.Join("wf", name)), path, maxExportedFileBytes, options.HomeDir); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("无法收集 %s: %w", name, err)
		}
	}
	roots := options.AntigravityLogRoots
	if len(roots) == 0 {
		roots = defaultAntigravityLogRoots(options.HomeDir)
	}
	if err := addAntigravityLogs(writer, roots, options.HomeDir); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("无法完成诊断日志压缩: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("无法保存诊断日志: %w", err)
	}
	_ = os.Chmod(destination, 0o600)
	success = true
	return nil
}

func defaultAntigravityLogRoots(home string) []string {
	configDir, _ := os.UserConfigDir()
	if configDir == "" && home != "" {
		if goruntime.GOOS == "darwin" {
			configDir = filepath.Join(home, "Library", "Application Support")
		} else {
			configDir = home
		}
	}
	var roots []string
	for _, name := range []string{"Antigravity", "Antigravity 2", "Antigravity2"} {
		if configDir != "" {
			roots = append(roots, filepath.Join(configDir, name, "logs"))
		}
	}
	return roots
}

func addAntigravityLogs(writer *zip.Writer, roots []string, home string) error {
	seen := map[string]struct{}{}
	var total int64
	for rootIndex, root := range roots {
		root = filepath.Clean(strings.TrimSpace(root))
		if root == "." || root == "" {
			continue
		}
		key := strings.ToLower(root)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		session := latestLogSession(root)
		if session == "" {
			continue
		}
		err := filepath.WalkDir(session, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() || !allowedAntigravityLog(entry.Name()) || total >= maxAntigravityLogBytes {
				return nil
			}
			remaining := maxAntigravityLogBytes - total
			limit := maxExportedFileBytes
			if remaining < limit {
				limit = remaining
			}
			data, err := readTail(path, limit)
			if err != nil {
				return nil
			}
			data = boundTail(redact(data, home), limit)
			relative, err := filepath.Rel(session, path)
			if err != nil || strings.HasPrefix(relative, "..") {
				return nil
			}
			name := filepath.ToSlash(filepath.Join("antigravity", fmt.Sprintf("source-%d", rootIndex+1), filepath.Base(session), relative))
			if err := addArchiveBytes(writer, name, data); err != nil {
				return err
			}
			total += int64(len(data))
			return nil
		})
		if err != nil {
			return fmt.Errorf("无法收集 Antigravity 日志: %w", err)
		}
	}
	return nil
}

func latestLogSession(root string) string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	type candidate struct {
		path string
		mod  time.Time
	}
	var sessions []candidate
	hasDirectLogs := false
	for _, entry := range entries {
		if !entry.IsDir() {
			hasDirectLogs = hasDirectLogs || allowedAntigravityLog(entry.Name())
			continue
		}
		info, err := entry.Info()
		if err == nil {
			sessions = append(sessions, candidate{path: filepath.Join(root, entry.Name()), mod: info.ModTime()})
		}
	}
	if len(sessions) == 0 {
		if hasDirectLogs {
			return root
		}
		return ""
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].mod.Equal(sessions[j].mod) {
			return sessions[i].path > sessions[j].path
		}
		return sessions[i].mod.After(sessions[j].mod)
	})
	return sessions[0].path
}

func allowedAntigravityLog(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "ls-main.log", "cloudcode.log", "main.log", "auth.log", "artifacts.log", "renderer.log", "views.log", "antigravity.log", "antigravity cockpit.log":
		return true
	default:
		return false
	}
}

func addSanitizedFile(writer *zip.Writer, archiveName, path string, limit int64, home string) error {
	data, err := readTail(path, limit)
	if err != nil {
		return err
	}
	return addArchiveBytes(writer, archiveName, boundTail(redact(data, home), limit))
}

func readTail(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, nil
	}
	marker := []byte("[earlier content truncated]\n")
	truncated := info.Size() > limit
	readLimit := limit
	if truncated {
		readLimit -= int64(len(marker))
		if readLimit < 0 {
			readLimit = 0
		}
		if _, err := file.Seek(-readLimit, io.SeekEnd); err != nil {
			return nil, err
		}
	}
	data, err := io.ReadAll(io.LimitReader(file, readLimit))
	if err != nil {
		return nil, err
	}
	if truncated {
		data = append(marker, data...)
	}
	return data, nil
}

func boundTail(data []byte, limit int64) []byte {
	if limit <= 0 || int64(len(data)) <= limit {
		return data
	}
	marker := []byte("[earlier content truncated]\n")
	keep := int(limit) - len(marker)
	if keep <= 0 {
		return marker[:min(len(marker), int(limit))]
	}
	return append(marker, data[len(data)-keep:]...)
}

func addArchiveBytes(writer *zip.Writer, name string, data []byte) error {
	header := &zip.FileHeader{Name: filepath.ToSlash(name), Method: zip.Deflate}
	header.SetMode(0o600)
	header.SetModTime(time.Now())
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(entry, bytes.NewReader(data))
	return err
}

func redact(data []byte, home string) []byte {
	value := string(data)
	value = jsonSecret.ReplaceAllString(value, `${1}[REDACTED]${2}`)
	value = jsonArraySecret.ReplaceAllString(value, `${1}[REDACTED]${2}`)
	value = jsonImageData.ReplaceAllString(value, `${1}[REDACTED]${2}`)
	value = headerSecret.ReplaceAllString(value, `${1}[REDACTED]`)
	value = oauthCodeSecret.ReplaceAllString(value, `${1}[REDACTED]`)
	value = urlSecret.ReplaceAllString(value, `${1}[REDACTED]`)
	value = knownSecret.ReplaceAllString(value, `[REDACTED]`)
	value = jwtSecret.ReplaceAllString(value, `[REDACTED]`)
	value = longBase64Data.ReplaceAllString(value, `[REDACTED_BINARY]`)
	home = strings.TrimSpace(home)
	if home != "" {
		value = strings.ReplaceAll(value, home, "~")
		value = strings.ReplaceAll(value, strings.ReplaceAll(home, `\`, `\\`), `~`)
	}
	return []byte(value)
}
