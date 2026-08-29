package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"antigravity-wf-assistant/internal/agent"
	"antigravity-wf-assistant/internal/claudeconfig"
)

// claudeCodeAgentAdapter is the concrete Claude Code integration. It owns no
// account state and only reports the safe state of the documented user
// settings.json target managed by claudeconfig.
type claudeCodeAgentAdapter struct{}

func newClaudeCodeAgentAdapter() *claudeCodeAgentAdapter { return &claudeCodeAgentAdapter{} }

func (adapter *claudeCodeAgentAdapter) Metadata() agent.Metadata {
	for _, metadata := range agent.BuiltinMetadata() {
		if metadata.ID == agent.ClaudeCodeID {
			return metadata
		}
	}
	panic("Claude Code agent metadata is not registered")
}

// Detect combines a non-invasive PATH check with the transaction manager's
// redacted user-settings inspection. It never executes Claude Code, reads a
// credential file, or treats a configured token as proof of account login.
func (adapter *claudeCodeAgentAdapter) Detect(ctx context.Context) (agent.Status, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return agent.Status{}, err
		}
	}
	metadata := adapter.Metadata()
	manager, err := claudeconfig.NewDefaultManager()
	if err != nil {
		return claudeCodeAgentErrorStatus(metadata), nil
	}

	snapshot, inspectErr := manager.Inspect()
	backupAvailable := false
	if inspectErr == nil && snapshot.Valid {
		if _, err := manager.ListBackups(); err == nil {
			backupAvailable = true
		}
	}

	cliPath, cliErr := exec.LookPath("claude")
	cliPath = strings.TrimSpace(cliPath)
	cliFound := cliPath != "" && cliErr == nil
	cliDiscoveryIssue := cliErr != nil && !errors.Is(cliErr, exec.ErrNotFound)
	return claudeCodeAgentSnapshotStatus(metadata, snapshot, inspectErr, cliPath, cliFound, cliDiscoveryIssue, backupAvailable), nil
}

func (adapter *claudeCodeAgentAdapter) Diagnose(ctx context.Context) ([]agent.Diagnostic, error) {
	status, err := adapter.Detect(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	diagnostics := make([]agent.Diagnostic, 0, 2)
	switch status.State {
	case agent.StateError:
		diagnostics = append(diagnostics, agent.Diagnostic{
			AgentID: agent.ClaudeCodeID, Code: "claude-code.settings-location-unavailable", Severity: agent.SeverityError,
			Summary:     "Claude Code user-settings location is unavailable",
			Detail:      "XIASS Tools could not safely resolve the selected Claude Code settings.json location.",
			Remediation: "Check CLAUDE_CONFIG_DIR or the local Claude Code user directory, then run the check again.", CreatedAt: now,
		})
	case agent.StateDegraded:
		diagnostics = append(diagnostics, agent.Diagnostic{
			AgentID: agent.ClaudeCodeID, Code: "claude-code.settings-needs-attention", Severity: agent.SeverityError,
			Summary:     "Claude Code user settings need attention",
			Detail:      "The selected settings.json could not be safely validated. No settings were changed.",
			Remediation: "Repair the JSON file or restore a verified user-settings backup before saving a new configuration.", CreatedAt: now,
		})
	case agent.StateNotInstalled:
		diagnostics = append(diagnostics, agent.Diagnostic{
			AgentID: agent.ClaudeCodeID, Code: "claude-code.cli-not-found", Severity: agent.SeverityInfo,
			Summary:     "Claude Code CLI was not found",
			Detail:      "XIASS Tools can prepare the selected user settings, but does not claim that Claude Code is installed or logged in.",
			Remediation: "Install or open Claude Code, then run the local check again.", CreatedAt: now,
		})
	case agent.StateDetected:
		diagnostics = append(diagnostics, agent.Diagnostic{
			AgentID: agent.ClaudeCodeID, Code: "claude-code.settings-not-managed", Severity: agent.SeverityInfo,
			Summary:     "Claude Code user settings are available",
			Detail:      "The selected settings.json is valid but is not fully managed by XIASS Tools.",
			Remediation: "Use the Claude Code configuration flow to save only the API root, authorization token, and model fields.", CreatedAt: now,
		})
	}
	return diagnostics, nil
}

func claudeCodeAgentSnapshotStatus(metadata agent.Metadata, snapshot claudeconfig.Snapshot, inspectErr error, cliPath string, cliFound, cliDiscoveryIssue, backupAvailable bool) agent.Status {
	validConfig := inspectErr == nil && snapshot.Valid
	state := agent.StateDetected
	message := "Claude Code user settings are ready to be configured."
	switch {
	case !validConfig:
		state = agent.StateDegraded
		message = "The selected Claude Code settings.json could not be safely validated."
	case snapshot.Managed:
		state = agent.StateReady
		message = "Claude Code user settings are configured by XIASS Tools."
	case !cliFound && !snapshot.Location.Exists:
		state = agent.StateNotInstalled
		message = "Claude Code CLI was not found and no existing user settings file was detected."
	case snapshot.Location.Exists:
		message = "A valid Claude Code user settings file was detected."
	}
	if cliDiscoveryIssue {
		state = agent.StateDegraded
		message = "Claude Code user settings were inspected, but CLI discovery could not be completed."
	}

	status := agent.Status{
		AgentID:     agent.ClaudeCodeID,
		DisplayName: metadata.DisplayName,
		State:       state,
		Message:     message,
		Installation: agent.Installation{
			ExecutablePath: cliPath,
			Platform:       runtime.GOOS,
		},
		Details: map[string]string{
			"settingsPresent":    fmt.Sprintf("%t", snapshot.Location.Exists),
			"settingsValid":      fmt.Sprintf("%t", validConfig),
			"managed":            fmt.Sprintf("%t", snapshot.Managed),
			"cliPresent":         fmt.Sprintf("%t", cliFound),
			"loopbackConfigured": fmt.Sprintf("%t", claudeCodeLoopbackConfigured(snapshot, validConfig)),
		},
		UpdatedAt: time.Now().UTC(),
	}
	status.Capabilities = claudeCodeCapabilityStatuses(metadata, validConfig, snapshot.Managed, claudeCodeLoopbackConfigured(snapshot, validConfig), backupAvailable)
	return status
}

func claudeCodeAgentErrorStatus(metadata agent.Metadata) agent.Status {
	status := agent.Status{
		AgentID:     agent.ClaudeCodeID,
		DisplayName: metadata.DisplayName,
		State:       agent.StateError,
		Message:     "The Claude Code user-settings location could not be safely resolved.",
		UpdatedAt:   time.Now().UTC(),
	}
	status.Capabilities = claudeCodeCapabilityStatuses(metadata, false, false, false, false)
	return status
}

func claudeCodeCapabilityStatuses(metadata agent.Metadata, validConfig, managed, loopbackConfigured, backupAvailable bool) []agent.CapabilityStatus {
	statuses := make([]agent.CapabilityStatus, 0, len(metadata.Capabilities))
	for _, declaration := range metadata.Capabilities {
		status := agent.CapabilityStatus{
			Capability:   declaration.Capability,
			Availability: declaration.Availability,
			Available:    false,
			Reason:       "This capability is not implemented for Claude Code.",
		}
		switch declaration.Capability {
		case agent.CapabilityDiscovery:
			status.Availability = agent.CapabilityAvailable
			status.Available = true
			status.Reason = "Local Claude Code user-settings discovery completed."
		case agent.CapabilityConfiguration:
			status.Availability = agent.CapabilityAvailable
			status.Available = validConfig
			if validConfig {
				status.Reason = "The selected settings.json can be changed with verified atomic writes."
			} else {
				status.Reason = "The selected settings.json must be repaired before it can be changed."
			}
		case agent.CapabilityModelCatalog:
			status.Availability = agent.CapabilityAvailable
			status.Available = validConfig
			if validConfig {
				status.Reason = "The explicit Claude Code model user setting can be selected and saved."
			} else {
				status.Reason = "A valid settings.json is required before model selection is available."
			}
		case agent.CapabilityLocalProxy:
			status.Availability = agent.CapabilityAvailable
			status.Available = managed && loopbackConfigured
			if status.Available {
				status.Reason = "The managed API root is a verified local loopback endpoint."
			} else {
				status.Reason = "No managed local loopback API root is configured."
			}
		case agent.CapabilityDiagnostics:
			status.Availability = agent.CapabilityAvailable
			status.Available = true
			status.Reason = "Credential-safe local user-settings diagnostics are available."
		case agent.CapabilityBackup:
			status.Availability = agent.CapabilityAvailable
			status.Available = validConfig && backupAvailable
			if status.Available {
				status.Reason = "Verified Claude Code user-settings backups are available."
			} else {
				status.Reason = "The user-settings and backup locations must be safely verified first."
			}
		}
		statuses = append(statuses, status)
	}
	return statuses
}

func claudeCodeLoopbackConfigured(snapshot claudeconfig.Snapshot, validConfig bool) bool {
	if !validConfig || !snapshot.Managed || snapshot.BaseURL == "" {
		return false
	}
	parsed, err := url.Parse(snapshot.BaseURL)
	if err != nil || parsed == nil {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(parsed.Hostname())), ".")
	if host == "localhost" {
		return true
	}
	parsedIP := net.ParseIP(host)
	return parsedIP != nil && parsedIP.IsLoopback()
}

var _ agent.Adapter = (*claudeCodeAgentAdapter)(nil)
var _ agent.DiagnosticProvider = (*claudeCodeAgentAdapter)(nil)
