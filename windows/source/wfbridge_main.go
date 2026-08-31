//go:build wfbridge

package main

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"antigravity-wf-assistant/internal/proxy"
)

// The WF bridge embeds the already-tested Vue application. XIASS Tools loads
// it inside the Antigravity workspace and supplies the per-process RPC token
// through the iframe name or a postMessage; the token never appears in a URL.
//
//go:embed all:frontend/dist
var wfBridgeAssets embed.FS

type wfBridgeRPCRequest struct {
	Method string            `json:"method"`
	Args   []json.RawMessage `json:"args"`
}

type wfBridgeRPCResponse struct {
	OK     bool   `json:"ok"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

type wfBridgeEvent struct {
	Sequence uint64 `json:"sequence"`
	Name     string `json:"name"`
	Payload  any    `json:"payload,omitempty"`
}

type wfBridgeEventHub struct {
	mu       sync.Mutex
	sequence uint64
	events   []wfBridgeEvent
}

func (h *wfBridgeEventHub) emit(name string, payload any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sequence++
	h.events = append(h.events, wfBridgeEvent{Sequence: h.sequence, Name: name, Payload: payload})
	if len(h.events) > 256 {
		h.events = append([]wfBridgeEvent(nil), h.events[len(h.events)-256:]...)
	}
}

func (h *wfBridgeEventHub) after(sequence uint64) (uint64, []wfBridgeEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make([]wfBridgeEvent, 0, len(h.events))
	for _, event := range h.events {
		if event.Sequence > sequence {
			result = append(result, event)
		}
	}
	return h.sequence, result
}

func wfBridgeParentExitSignal(parentPID string, input io.Reader) (<-chan struct{}, error) {
	parentPID = strings.TrimSpace(parentPID)
	if parentPID == "" {
		return nil, nil
	}
	parsedPID, err := strconv.Atoi(parentPID)
	if err != nil || parsedPID <= 0 {
		return nil, fmt.Errorf("invalid XIASS_PARENT_PID %q", parentPID)
	}
	exited := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, input)
		close(exited)
	}()
	return exited, nil
}

type wfBridgeServer struct {
	application *App
	token       string
	events      *wfBridgeEventHub
	assets      http.Handler
}

func main() {
	token := strings.TrimSpace(os.Getenv("XIASS_WF_RPC_TOKEN"))
	if len(token) < 32 {
		log.Fatal("XIASS_WF_RPC_TOKEN must contain at least 32 characters")
	}

	port := 0
	if rawPort := strings.TrimSpace(os.Getenv("XIASS_WF_RPC_PORT")); rawPort != "" {
		parsed, err := strconv.Atoi(rawPort)
		if err != nil || parsed < 0 || parsed > 65535 {
			log.Fatalf("invalid XIASS_WF_RPC_PORT %q", rawPort)
		}
		port = parsed
	}
	parentExit, err := wfBridgeParentExitSignal(os.Getenv("XIASS_PARENT_PID"), os.Stdin)
	if err != nil {
		log.Fatal(err)
	}

	application := newApp()
	application.ctx = context.Background()
	events := &wfBridgeEventHub{}
	application.eventSink = events.emit
	if err := proxy.Start(application.storageDir); err != nil {
		log.Printf("[xiass-wf-bridge] 本地代理启动失败，桥接界面仍可用于诊断和修复: %v", err)
	}
	go application.syncHistory()

	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		application.releaseExitResources()
		log.Fatalf("cannot bind XIASS WF bridge: %v", err)
	}

	assetRoot, err := fs.Sub(wfBridgeAssets, "frontend/dist")
	if err != nil {
		_ = listener.Close()
		application.releaseExitResources()
		log.Fatalf("cannot open embedded WF assets: %v", err)
	}
	bridge := &wfBridgeServer{
		application: application,
		token:       token,
		events:      events,
		assets:      http.FileServer(http.FS(assetRoot)),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", bridge.handleHealth)
	mux.HandleFunc("/rpc", bridge.handleRPC)
	mux.HandleFunc("/events", bridge.handleEvents)
	mux.HandleFunc("/wf-runtime.js", bridge.handleRuntime)
	mux.HandleFunc("/", bridge.handleAssets)
	server := &http.Server{
		Handler:           bridge.securityHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	address := listener.Addr().(*net.TCPAddr)
	ready := map[string]any{
		"event":         "ready",
		"service":       "xiass-wf-bridge",
		"host":          "127.0.0.1",
		"port":          address.Port,
		"schemaVersion": 1,
	}
	encoded, _ := json.Marshal(ready)
	fmt.Println(string(encoded))

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	select {
	case <-signals:
	case <-parentExit:
		log.Printf("[xiass-wf-bridge] XIASS Tools 父进程已退出，正在释放本地服务")
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("[xiass-wf-bridge] server stopped unexpectedly: %v", err)
		}
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownContext)
	application.releaseExitResources()
}

func (s *wfBridgeServer) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Access-Control-Allow-Origin", "*")
		response.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		response.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if request.Method == http.MethodOptions {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(response, request)
	})
}

func (s *wfBridgeServer) authorized(request *http.Request) bool {
	provided := strings.TrimSpace(strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer "))
	if len(provided) != len(s.token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) == 1
}

func (s *wfBridgeServer) handleHealth(response http.ResponseWriter, _ *http.Request) {
	writeWFBridgeJSON(response, http.StatusOK, map[string]any{
		"ok":             true,
		"service":        "xiass-wf-bridge",
		"proxyListening": proxy.IsListening(),
	})
}

func (s *wfBridgeServer) handleRPC(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeWFBridgeJSON(response, http.StatusMethodNotAllowed, wfBridgeRPCResponse{OK: false, Error: "POST required"})
		return
	}
	if !s.authorized(request) {
		writeWFBridgeJSON(response, http.StatusUnauthorized, wfBridgeRPCResponse{OK: false, Error: "unauthorized"})
		return
	}
	defer request.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(request.Body, 32<<20))
	decoder.DisallowUnknownFields()
	var rpcRequest wfBridgeRPCRequest
	if err := decoder.Decode(&rpcRequest); err != nil {
		writeWFBridgeJSON(response, http.StatusBadRequest, wfBridgeRPCResponse{OK: false, Error: "invalid request"})
		return
	}
	result, err := invokeWFBridgeMethod(s.application, rpcRequest)
	if err != nil {
		writeWFBridgeJSON(response, http.StatusBadRequest, wfBridgeRPCResponse{OK: false, Error: err.Error()})
		return
	}
	writeWFBridgeJSON(response, http.StatusOK, wfBridgeRPCResponse{OK: true, Result: result})
}

func (s *wfBridgeServer) handleEvents(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(request) {
		response.WriteHeader(http.StatusUnauthorized)
		return
	}
	after, _ := strconv.ParseUint(request.URL.Query().Get("after"), 10, 64)
	sequence, events := s.events.after(after)
	writeWFBridgeJSON(response, http.StatusOK, map[string]any{"sequence": sequence, "events": events})
}

func (s *wfBridgeServer) handleRuntime(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	response.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	_, _ = io.WriteString(response, wfBridgeRuntimeJavaScript)
}

func (s *wfBridgeServer) handleAssets(response http.ResponseWriter, request *http.Request) {
	if request.URL.Path == "/" || request.URL.Path == "/index.html" {
		index, err := wfBridgeAssets.ReadFile("frontend/dist/index.html")
		if err != nil {
			http.Error(response, "WF interface unavailable", http.StatusInternalServerError)
			return
		}
		html := strings.Replace(string(index), "</head>", `<script src="/wf-runtime.js"></script></head>`, 1)
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(response, html)
		return
	}
	s.assets.ServeHTTP(response, request)
}

func invokeWFBridgeMethod(application *App, request wfBridgeRPCRequest) (result any, err error) {
	methodName := strings.TrimSpace(request.Method)
	if methodName == "" || methodName == "QuitApp" {
		return nil, errors.New("method is not available through the embedded bridge")
	}
	method := reflect.ValueOf(application).MethodByName(methodName)
	if !method.IsValid() {
		return nil, fmt.Errorf("unknown method %q", methodName)
	}
	methodType := method.Type()
	if methodType.NumIn() != len(request.Args) {
		return nil, fmt.Errorf("method %s expects %d arguments", methodName, methodType.NumIn())
	}
	arguments := make([]reflect.Value, methodType.NumIn())
	for index := 0; index < methodType.NumIn(); index++ {
		argument := reflect.New(methodType.In(index))
		if err := json.Unmarshal(request.Args[index], argument.Interface()); err != nil {
			return nil, fmt.Errorf("invalid argument %d for %s", index+1, methodName)
		}
		arguments[index] = argument.Elem()
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("[xiass-wf-bridge] panic in %s: %v\n%s", methodName, recovered, debug.Stack())
			result = nil
			err = fmt.Errorf("method %s failed", methodName)
		}
	}()
	outputs := method.Call(arguments)
	if len(outputs) == 0 {
		return nil, nil
	}
	if last := outputs[len(outputs)-1]; last.Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
		outputs = outputs[:len(outputs)-1]
		if !last.IsNil() {
			return nil, last.Interface().(error)
		}
	}
	if len(outputs) == 1 {
		return outputs[0].Interface(), nil
	}
	values := make([]any, len(outputs))
	for index, output := range outputs {
		values[index] = output.Interface()
	}
	return values, nil
}

func writeWFBridgeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

const wfBridgeRuntimeJavaScript = `(() => {
  const handlers = new Map();
  let cursor = 0;
  let polling = false;
  let token = String(window.name || "").trim();
  window.name = "";
  let resolveToken;
  const tokenReady = token ? Promise.resolve(token) : new Promise((resolve) => { resolveToken = resolve; });

  window.addEventListener("message", (event) => {
    if (event?.data?.type !== "xiass-wf-auth") return;
    const candidate = String(event.data.token || "").trim();
    if (candidate.length < 32 || token) return;
    token = candidate;
    resolveToken(candidate);
  });

  async function call(method, args) {
    const credential = await tokenReady;
    const response = await fetch("/rpc", {
      method: "POST",
      headers: { "Authorization": "Bearer " + credential, "Content-Type": "application/json" },
      body: JSON.stringify({ method, args }),
    });
    const body = await response.json();
    if (!response.ok || !body.ok) throw new Error(body.error || ("RPC " + response.status));
    return body.result;
  }

  async function poll() {
    if (polling || handlers.size === 0) return;
    polling = true;
    try {
      const credential = await tokenReady;
      const response = await fetch("/events?after=" + cursor, { headers: { "Authorization": "Bearer " + credential } });
      if (response.ok) {
        const body = await response.json();
        cursor = Number(body.sequence || cursor);
        for (const event of body.events || []) {
          for (const callback of handlers.get(event.name) || []) callback(event.payload);
        }
      }
    } catch (_) {
    } finally {
      polling = false;
      if (handlers.size > 0) setTimeout(poll, 400);
    }
  }

  const app = new Proxy({}, { get: (_, method) => (...args) => call(String(method), args) });
  window.go = { main: { App: app } };
  window.runtime = {
    EventsOn(name, callback) {
      if (!handlers.has(name)) handlers.set(name, new Set());
      handlers.get(name).add(callback);
      poll();
      return () => handlers.get(name)?.delete(callback);
    },
    EventsOnMultiple(name, callback) { return this.EventsOn(name, callback); },
    EventsOnce(name, callback) {
      let unsubscribe = () => {};
      unsubscribe = this.EventsOn(name, (payload) => { unsubscribe(); callback(payload); });
      return unsubscribe;
    },
    EventsOff(name) { handlers.delete(name); },
    EventsOffAll() { handlers.clear(); },
    EventsEmit() {},
    LogPrint: console.log,
    LogTrace: console.trace,
    LogDebug: console.debug,
    LogInfo: console.info,
    LogWarning: console.warn,
    LogError: console.error,
  };
})();`
