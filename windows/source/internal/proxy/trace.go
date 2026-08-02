package proxy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	traceMu   sync.Mutex
	traceFile string
	diagMu    sync.RWMutex
	diag      Diagnostics
)

type Diagnostics struct {
	LastRequestAt            string   `json:"lastRequestAt"`
	LastRequestPath          string   `json:"lastRequestPath"`
	LastModelFetchAt         string   `json:"lastModelFetchAt"`
	LastModelInjectionAt     string   `json:"lastModelInjectionAt"`
	LastInjectedModelCount   int      `json:"lastInjectedModelCount"`
	LastInjectedModelNames   []string `json:"lastInjectedModelNames"`
	LastInjectedModelSlugs   []string `json:"lastInjectedModelSlugs"`
	LastModelShape           string   `json:"lastModelShape"`
	LastModelIndexes         string   `json:"lastModelIndexes"`
	LastModelStatusCode      int      `json:"lastModelStatusCode"`
	LastModelContentEncoding string   `json:"lastModelContentEncoding"`
	LastError                string   `json:"lastError"`
}

func InitTrace(dir string) {
	traceFile = filepath.Join(dir, "proxy-trace.jsonl")
	diagMu.Lock()
	diag = Diagnostics{}
	diagMu.Unlock()
	_ = os.MkdirAll(dir, 0o700)
	_ = os.Chmod(dir, 0o700)
	_ = os.Chmod(traceFile, 0o600)
}

func trace(event string, fields map[string]any) {
	if traceFile == "" {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	fields["time"] = now
	fields["event"] = event
	updateDiagnostics(event, fields, now)
	data, err := json.Marshal(fields)
	if err != nil {
		return
	}
	traceMu.Lock()
	defer traceMu.Unlock()
	f, err := os.OpenFile(traceFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(data)
	f.WriteString("\n")
}

func updateDiagnostics(event string, fields map[string]any, now string) {
	diagMu.Lock()
	defer diagMu.Unlock()
	switch event {
	case "request":
		diag.LastRequestAt = now
		diag.LastRequestPath, _ = fields["cleanPath"].(string)
		if strings.Contains(diag.LastRequestPath, ":fetchAvailableModels") {
			diag.LastModelFetchAt = now
		}
	case "models-injected":
		diag.LastModelInjectionAt = now
		diag.LastError = ""
		switch count := fields["customCount"].(type) {
		case int:
			diag.LastInjectedModelCount = count
		case float64:
			diag.LastInjectedModelCount = int(count)
		}
		if containers, ok := fields["containers"].([]string); ok {
			diag.LastModelShape = strings.Join(containers, ", ")
		}
		diag.LastInjectedModelNames = traceStringSlice(fields["customNames"])
		diag.LastInjectedModelSlugs = traceStringSlice(fields["customSlugs"])
		diag.LastModelIndexes = strings.Join(traceStringSlice(fields["indexPaths"]), ", ")
	default:
		if strings.Contains(event, "error") {
			if message, ok := fields["message"].(string); ok {
				diag.LastError = message
			}
			switch code := fields["statusCode"].(type) {
			case int:
				diag.LastModelStatusCode = code
			case float64:
				diag.LastModelStatusCode = int(code)
			}
			if encoding, ok := fields["encoding"].(string); ok {
				diag.LastModelContentEncoding = encoding
			}
		}
	}
}

func traceStringSlice(value any) []string {
	switch items := value.(type) {
	case []string:
		return append([]string(nil), items...)
	case []any:
		result := make([]string, 0, len(items))
		for _, item := range items {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}

func GetDiagnostics() Diagnostics {
	diagMu.RLock()
	defer diagMu.RUnlock()
	return diag
}
