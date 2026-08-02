package proxy

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const ProxyPort = 50999

var (
	serverMu sync.Mutex
	srv      *http.Server
)

// Start starts the proxy server on ProxyPort.
// If the port is already in use, only another current BYOK proxy can be reused.
func Start(storageDir string) error {
	serverMu.Lock()
	defer serverMu.Unlock()

	if srv != nil {
		return nil // already running
	}

	InitTrace(storageDir)

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleRequest)

	// Test if port is available
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", ProxyPort))
	if err != nil {
		if IsManagedListener() {
			log.Printf("[wf] 代理端口 %d 已由另一个当前 WF助手实例监听", ProxyPort)
			return nil
		}
		return fmt.Errorf("代理端口 %d 已被旧版助手或其他程序占用；请退出旧程序后重试", ProxyPort)
	}

	srv = &http.Server{
		Handler: mux,
	}

	go func() {
		log.Printf("[byok] 代理运行在 http://127.0.0.1:%d", ProxyPort)
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("[byok] 代理服务器错误: %v", err)
		}
		serverMu.Lock()
		srv = nil
		serverMu.Unlock()
	}()

	trace("proxy-started", map[string]any{"port": ProxyPort})
	return nil
}

// Stop gracefully shuts down the proxy server.
func Stop() error {
	serverMu.Lock()
	defer serverMu.Unlock()
	if srv == nil {
		if IsListening() && !IsManagedListener() {
			return fmt.Errorf("端口 %d 的监听器不属于当前 WF助手，无法从这里停止", ProxyPort)
		}
		return nil
	}
	err := srv.Shutdown(context.Background())
	srv = nil
	return err
}

// IsManagedListener verifies that the listener implements the current BYOK
// health contract instead of trusting any process that happens to own 50999.
func IsManagedListener() bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/_antigravity-byok/health", ProxyPort))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK && resp.Header.Get("X-Antigravity-BYOK") == "go-proxy"
}

func OwnsListener() bool {
	serverMu.Lock()
	defer serverMu.Unlock()
	return srv != nil
}

// IsListening returns true if the proxy port is currently accepting connections.
func IsListening() bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", ProxyPort), 400e6)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// readBody reads and returns the request body.
func readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}

// handleRequest is the top-level HTTP handler.
func handleRequest(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/_antigravity-byok/health" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Antigravity-BYOK", "go-proxy")
		_, _ = io.WriteString(w, `{"ok":true,"proxy":"antigravity-byok"}`)
		return
	}

	cleanPath := cleanPatchedPath(path)

	trace("request", map[string]any{
		"method":    r.Method,
		"path":      path,
		"cleanPath": cleanPath,
	})

	if strings.Contains(cleanPath, ":fetchAvailableModels") {
		handleFetchAvailableModels(w, r)
		return
	}

	if strings.Contains(cleanPath, ":streamGenerateContent") ||
		strings.Contains(cleanPath, ":generateContent") {
		handleGenerate(w, r, cleanPath)
		return
	}

	// Everything else: passthrough
	body, err := readBody(r)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	passthroughRequest(w, r, body, cleanPath)
}

// cleanPatchedPath strips the path prefix inserted by the text and fixed-size
// binary patch variants before a request is routed or passed through.
func cleanPatchedPath(path string) string {
	for _, prefix := range []string{
		"/v1internal/antigravity-byok/",
		"/v1internal/byokxxx/",
		"/v1internal/byokxxx-sandbox/",
	} {
		if strings.HasPrefix(path, prefix) {
			cleanPath := strings.TrimPrefix(path, prefix)
			if strings.HasPrefix(cleanPath, "v1internal:") || strings.HasPrefix(cleanPath, "v1internal/") {
				return "/" + cleanPath
			}
		}
	}
	return path
}
