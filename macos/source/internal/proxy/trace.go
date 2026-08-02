package proxy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	traceMu   sync.Mutex
	traceFile string
)

func InitTrace(dir string) {
	traceFile = filepath.Join(dir, "proxy-trace.jsonl")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.Chmod(dir, 0o700)
	_ = os.Chmod(traceFile, 0o600)
}

func trace(event string, fields map[string]any) {
	if traceFile == "" {
		return
	}
	fields["time"] = time.Now().UTC().Format(time.RFC3339Nano)
	fields["event"] = event
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
