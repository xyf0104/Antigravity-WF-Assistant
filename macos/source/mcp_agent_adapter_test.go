package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"antigravity-wf-assistant/internal/agent"
	"antigravity-wf-assistant/internal/mcpconfig"
)

func TestMCPAgentStatusOnlyEnablesVerifiedGlobalConfiguration(t *testing.T) {
	metadata := newCursorMCPAgentAdapter().Metadata()
	cases := []struct {
		name          string
		discovered    agent.Status
		snapshot      mcpconfig.Snapshot
		inspected     bool
		wantState     agent.State
		wantConfigure bool
	}{
		{
			name:          "verified Cursor and empty configuration",
			discovered:    agent.Status{State: agent.StateDetected},
			snapshot:      mcpconfig.Snapshot{Target: mcpconfig.TargetCursor, Valid: true},
			inspected:     true,
			wantState:     agent.StateDetected,
			wantConfigure: true,
		},
		{
			name:          "managed global entry",
			discovered:    agent.Status{State: agent.StateReady},
			snapshot:      mcpconfig.Snapshot{Target: mcpconfig.TargetCursor, Valid: true, ManagedServerConfigured: true},
			inspected:     true,
			wantState:     agent.StateReady,
			wantConfigure: true,
		},
		{
			name:          "sensitive configuration is protected",
			discovered:    agent.Status{State: agent.StateDetected},
			snapshot:      mcpconfig.Snapshot{Target: mcpconfig.TargetCursor, Valid: true, HasSensitiveConfiguration: true},
			inspected:     true,
			wantState:     agent.StateDegraded,
			wantConfigure: false,
		},
		{
			name:          "unvalidated configuration is protected",
			discovered:    agent.Status{State: agent.StateDetected},
			snapshot:      mcpconfig.Snapshot{Target: mcpconfig.TargetCursor},
			inspected:     false,
			wantState:     agent.StateDegraded,
			wantConfigure: false,
		},
		{
			name:          "missing client prevents configuration creation",
			discovered:    agent.Status{State: agent.StateNotInstalled},
			snapshot:      mcpconfig.Snapshot{Target: mcpconfig.TargetCursor, Valid: true},
			inspected:     true,
			wantState:     agent.StateNotInstalled,
			wantConfigure: false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			status := mcpAgentStatus(metadata, testCase.discovered, testCase.snapshot, testCase.inspected, testCase.inspected)
			if status.State != testCase.wantState {
				t.Fatalf("state = %q, want %q", status.State, testCase.wantState)
			}
			if got := agentCapabilityAvailable(status, agent.CapabilityConfiguration); got != testCase.wantConfigure {
				t.Fatalf("configuration available = %v, want %v", got, testCase.wantConfigure)
			}
			if !agentCapabilityAvailable(status, agent.CapabilityDiscovery) || !agentCapabilityAvailable(status, agent.CapabilityDiagnostics) {
				t.Fatalf("discovery/diagnostics were not available: %#v", status.Capabilities)
			}
			for _, capability := range []agent.Capability{agent.CapabilityOAuth, agent.CapabilityUsage, agent.CapabilityTwoFactorAuth, agent.CapabilityLocalProxy, agent.CapabilityModelCatalog} {
				if agentCapabilityAvailable(status, capability) {
					t.Fatalf("unsupported account or proxy capability was advertised: %s", capability)
				}
			}
		})
	}
}

func TestMCPBridgeRejectsUnknownTargetWithoutEchoingEndpoint(t *testing.T) {
	endpoint := "https://token-in-hostname.example.invalid/mcp?secret=must-not-escape"
	status := (&App{}).ApplyMCPConfiguration(MCPConfigurationInput{Target: "unknown-client", RemoteURL: endpoint})
	if status.OK || status.Message == "" {
		t.Fatalf("unknown target result = %#v", status)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), endpoint) || strings.Contains(string(encoded), "must-not-escape") {
		t.Fatalf("bridge result echoed endpoint data: %s", encoded)
	}
}

func TestMCPTargetAndErrorMessagesStayGeneric(t *testing.T) {
	if target, ok := parseMCPConfigurationTarget(" CURSOR "); !ok || target != mcpconfig.TargetCursor {
		t.Fatalf("cursor target parse = %q, %v", target, ok)
	}
	if target, ok := parseMCPConfigurationTarget("windsurf"); !ok || target != mcpconfig.TargetWindsurf {
		t.Fatalf("windsurf target parse = %q, %v", target, ok)
	}
	if _, ok := parseMCPConfigurationTarget("cursor-project"); ok {
		t.Fatal("unsupported project target was accepted")
	}
	for _, err := range []error{mcpconfig.ErrInvalidRemote, mcpconfig.ErrUnsafeConfiguration, mcpconfig.ErrInvalidConfiguration, mcpconfig.ErrOperationBusy, errors.New("/private/path/token")} {
		message := mcpConfigurationErrorMessage(err)
		if message == "" || strings.Contains(message, "/private/path/token") {
			t.Fatalf("unsafe error message %q for %v", message, err)
		}
	}
}
