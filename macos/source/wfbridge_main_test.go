//go:build wfbridge

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWFBridgeParentPipeSignalsExitOnEOF(t *testing.T) {
	reader, writer := io.Pipe()
	exited, err := wfBridgeParentExitSignal("123", reader)
	if err != nil {
		t.Fatalf("create parent exit signal: %v", err)
	}
	select {
	case <-exited:
		t.Fatal("parent exit signal fired before the pipe closed")
	default:
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close parent pipe: %v", err)
	}
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("parent exit signal did not fire after EOF")
	}
}

func TestWFBridgeParentPipeRejectsInvalidPIDAndAllowsStandaloneMode(t *testing.T) {
	if exited, err := wfBridgeParentExitSignal("", strings.NewReader("")); err != nil || exited != nil {
		t.Fatalf("standalone mode should disable parent monitoring: signal=%v err=%v", exited, err)
	}
	if _, err := wfBridgeParentExitSignal("not-a-pid", strings.NewReader("")); err == nil {
		t.Fatal("invalid parent PID must be rejected")
	}
}

func TestWFBridgeEventHubReturnsOnlyNewEvents(t *testing.T) {
	hub := &wfBridgeEventHub{}
	hub.emit("wf:first", map[string]any{"value": 1})
	sequence, events := hub.after(0)
	if sequence != 1 || len(events) != 1 || events[0].Name != "wf:first" {
		t.Fatalf("unexpected initial events: sequence=%d events=%+v", sequence, events)
	}
	hub.emit("wf:second", nil)
	sequence, events = hub.after(1)
	if sequence != 2 || len(events) != 1 || events[0].Name != "wf:second" {
		t.Fatalf("unexpected incremental events: sequence=%d events=%+v", sequence, events)
	}
}

func TestWFBridgeRPCRequiresToken(t *testing.T) {
	bridge := &wfBridgeServer{application: &App{}, token: strings.Repeat("a", 32), events: &wfBridgeEventHub{}}
	request := httptest.NewRequest(http.MethodPost, "/rpc", strings.NewReader(`{"method":"IsProxyListening","args":[]}`))
	response := httptest.NewRecorder()
	bridge.handleRPC(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d", response.Code)
	}
}

func TestWFBridgeRPCInvokesSafeExportedMethod(t *testing.T) {
	request := wfBridgeRPCRequest{Method: "IsProxyListening", Args: []json.RawMessage{}}
	if _, err := invokeWFBridgeMethod(&App{}, request); err != nil {
		t.Fatalf("invoke exported method: %v", err)
	}
	if _, err := invokeWFBridgeMethod(&App{}, wfBridgeRPCRequest{Method: "QuitApp"}); err == nil {
		t.Fatal("QuitApp must remain owned by the Tauri parent")
	}
	if _, err := invokeWFBridgeMethod(&App{}, wfBridgeRPCRequest{Method: "MissingMethod"}); err == nil {
		t.Fatal("unknown method must be rejected")
	}
}

func TestWFBridgeRuntimeDoesNotEmbedCredential(t *testing.T) {
	if strings.Contains(wfBridgeRuntimeJavaScript, "XIASS_WF_RPC_TOKEN") {
		t.Fatal("runtime script must receive the per-process credential at runtime")
	}
	if !strings.Contains(wfBridgeRuntimeJavaScript, "xiass-wf-auth") {
		t.Fatal("runtime script is missing the postMessage authentication handshake")
	}
}
