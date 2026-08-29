package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"antigravity-byok/internal/agent"
	"antigravity-byok/internal/codexconfig"
	"antigravity-byok/internal/codexdesktop"
)

// codexAgentAdapter exposes the verified, local Codex configuration lifecycle
// through the shared Agent Registry. It intentionally does not read
// ~/.codex/auth.json, claim an OpenAI account is connected, or start/stop a
// Codex application.
type codexAgentAdapter struct{}

func newCodexAgentAdapter() *codexAgentAdapter { return &codexAgentAdapter{} }

func (adapter *codexAgentAdapter) Metadata() agent.Metadata {
	for _, metadata := range agent.BuiltinMetadata() {
		if metadata.ID == agent.CodexID {
			return metadata
		}
	}
	panic("Codex agent metadata is not registered")
}

func (adapter *codexAgentAdapter) Detect(ctx context.Context) (agent.Status, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return agent.Status{}, err
		}
	}
	metadata := adapter.Metadata()
	desktop := codexdesktop.Discover(ctx)
	manager, err := codexconfig.NewDefaultManager()
	if err != nil {
		return decorateCodexDesktopStatus(codexAgentErrorStatus(metadata, err), desktop), nil
	}
	homeExists, homeError := existingCodexDirectory(manager.CodexHome)
	if homeError != nil {
		return decorateCodexDesktopStatus(codexAgentErrorStatus(metadata, homeError), desktop), nil
	}
	snapshot, inspectErr := manager.Inspect()
	if inspectErr != nil {
		return decorateCodexDesktopStatus(codexAgentSnapshotStatus(metadata, snapshot, homeExists, inspectErr), desktop), nil
	}
	return decorateCodexDesktopStatus(codexAgentSnapshotStatus(metadata, snapshot, homeExists, nil), desktop), nil
}

func (adapter *codexAgentAdapter) Diagnose(ctx context.Context) ([]agent.Diagnostic, error) {
	status, err := adapter.Detect(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	diagnostics := make([]agent.Diagnostic, 0, 3)
	if status.Details["desktopState"] == string(codexdesktop.StateDegraded) {
		diagnostics = append(diagnostics, agent.Diagnostic{
			AgentID: agent.CodexID, Code: "codex.desktop-inspection-incomplete", Severity: agent.SeverityWarning,
			Summary: "Codex Desktop installation could not be fully verified", Detail: status.Message,
			Remediation: "Quit Codex if it is open, then run the local check again. XIASS Tools will not close or control Codex automatically.", CreatedAt: now,
		})
	}
	switch status.State {
	case agent.StateNotInstalled:
		diagnostics = append(diagnostics, agent.Diagnostic{
			AgentID: agent.CodexID, Code: "codex.home-not-found", Severity: agent.SeverityInfo,
			Summary: "No local Codex configuration directory was found", Detail: status.Message,
			Remediation: "Open Codex once or set CODEX_HOME to the intended local configuration directory.", CreatedAt: now,
		})
	case agent.StateDegraded:
		diagnostics = append(diagnostics, agent.Diagnostic{
			AgentID: agent.CodexID, Code: "codex.config-invalid", Severity: agent.SeverityError,
			Summary: "Codex configuration needs attention", Detail: status.Message,
			Remediation: "Correct the invalid config.toml or restore one of the verified XIASS Tools backups before applying a new configuration.", CreatedAt: now,
		})
	case agent.StateDetected:
		diagnostics = append(diagnostics, agent.Diagnostic{
			AgentID: agent.CodexID, Code: "codex.not-configured", Severity: agent.SeverityInfo,
			Summary: "Codex configuration is available but is not managed by XIASS Tools", Detail: status.Message,
			Remediation: "Use the Codex configuration flow to add a separate XIASS Tools provider without replacing unrelated providers.", CreatedAt: now,
		})
	}
	return diagnostics, nil
}

func codexAgentSnapshotStatus(metadata agent.Metadata, snapshot codexconfig.ConfigSnapshot, homeExists bool, inspectErr error) agent.Status {
	state := agent.StateDetected
	message := "A local Codex configuration directory was detected."
	switch {
	case !homeExists:
		state = agent.StateNotInstalled
		message = "No local Codex configuration directory was found."
	case inspectErr != nil || !snapshot.Valid:
		state = agent.StateDegraded
		message = "The local Codex config.toml could not be validated."
	case !snapshot.Location.Exists:
		message = "The Codex configuration directory is ready; config.toml has not been created yet."
	case snapshot.ModelProvider == codexconfig.DefaultProviderID:
		state = agent.StateReady
		message = "Codex is configured with the XIASS Tools provider."
	default:
		message = "Codex config.toml is valid, but XIASS Tools is not the active provider."
	}
	status := agent.Status{
		AgentID:     agent.CodexID,
		DisplayName: metadata.DisplayName,
		State:       state,
		Message:     message,
		Details: map[string]string{
			"configPresent": fmt.Sprintf("%t", snapshot.Location.Exists),
			"configValid":   fmt.Sprintf("%t", snapshot.Valid && inspectErr == nil),
			"provider":      snapshot.ModelProvider,
		},
		UpdatedAt: time.Now().UTC(),
	}
	if inspectErr != nil {
		status.Details["inspectionError"] = redactCodexInspectionError(inspectErr)
	}
	status.Capabilities = codexCapabilityStatuses(metadata, homeExists, snapshot.Location.Exists, snapshot.Valid && inspectErr == nil)
	return status
}

// decorateCodexDesktopStatus keeps desktop discovery and config discovery
// distinct. A config.toml is not an executable, so it is deliberately never
// reported as Installation.ExecutablePath.
func decorateCodexDesktopStatus(status agent.Status, desktop codexdesktop.Status) agent.Status {
	if status.Details == nil {
		status.Details = make(map[string]string)
	}
	status.Details["desktopState"] = string(desktop.State)
	status.Details["desktopRunning"] = fmt.Sprintf("%t", desktop.Running)
	status.Details["desktopPresent"] = fmt.Sprintf("%t", desktop.Installation.Present)
	if desktop.Installation.Source != "" {
		status.Details["desktopSource"] = string(desktop.Installation.Source)
	}
	if desktop.Installation.Present {
		status.Installation = agent.Installation{
			Version:  desktop.Installation.Version,
			Platform: "desktop",
		}
	}

	switch desktop.State {
	case codexdesktop.StateRunning:
		status.Message = joinCodexStatusMessage(status.Message, "Codex Desktop is currently running; configuration changes take effect after an explicit restart.")
	case codexdesktop.StateInstalled:
		if status.State == agent.StateNotInstalled {
			status.State = agent.StateDetected
			status.Message = "Codex Desktop was found; its local configuration has not been created yet."
		} else {
			status.Message = joinCodexStatusMessage(status.Message, "Codex Desktop is installed.")
		}
	case codexdesktop.StateNotInstalled:
		if status.State != agent.StateNotInstalled {
			status.Message = joinCodexStatusMessage(status.Message, "Codex Desktop was not found in supported public installation locations.")
		}
	case codexdesktop.StateDegraded:
		status.State = agent.StateDegraded
		status.Message = joinCodexStatusMessage(status.Message, "Codex Desktop could not be fully verified; no write is attempted for history until its running state is safe to confirm.")
	}
	return status
}

func joinCodexStatusMessage(left, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" {
		return right
	}
	if right == "" {
		return left
	}
	return left + " " + right
}

func codexAgentErrorStatus(metadata agent.Metadata, err error) agent.Status {
	return agent.Status{
		AgentID:      agent.CodexID,
		DisplayName:  metadata.DisplayName,
		State:        agent.StateError,
		Message:      "The local Codex configuration location could not be resolved.",
		Details:      map[string]string{"inspectionError": redactCodexInspectionError(err)},
		UpdatedAt:    time.Now().UTC(),
		Capabilities: codexCapabilityStatuses(metadata, false, false, false),
	}
}

func codexCapabilityStatuses(metadata agent.Metadata, homeExists, configExists, configValid bool) []agent.CapabilityStatus {
	statuses := make([]agent.CapabilityStatus, 0, len(metadata.Capabilities))
	for _, declaration := range metadata.Capabilities {
		status := agent.CapabilityStatus{
			Capability:   declaration.Capability,
			Availability: declaration.Availability,
			Reason:       "This capability has not been verified on this machine.",
		}
		switch declaration.Capability {
		case agent.CapabilityDiscovery:
			status.Availability = agent.CapabilityAvailable
			status.Available = true
			status.Reason = "Local Codex configuration discovery completed."
		case agent.CapabilityConfiguration, agent.CapabilityModelCatalog:
			status.Availability = agent.CapabilityAvailable
			// A missing ~/.codex is an initial state, not an unsafe state. The
			// manager has already resolved its fixed default target and can create
			// the directory through the same verified atomic-write path used for
			// an existing valid config.toml.
			status.Available = configValid
			if status.Available {
				if homeExists {
					status.Reason = "Codex configuration can be managed with verified atomic writes."
				} else {
					status.Reason = "Codex can be safely initialized at its verified default configuration location."
				}
			} else if homeExists {
				status.Reason = "Codex configuration must be repaired before it can be changed."
			} else {
				status.Reason = "Codex configuration location could not be safely prepared."
			}
		case agent.CapabilityDiagnostics:
			status.Availability = agent.CapabilityAvailable
			status.Available = true
			status.Reason = "Credential-safe local diagnostics are available."
		case agent.CapabilityBackup, agent.CapabilitySessionRecovery:
			status.Availability = agent.CapabilityAvailable
			status.Available = homeExists && configExists && configValid
			if status.Available {
				status.Reason = "Verified local backup and recovery operations are available."
			} else {
				status.Reason = "A valid local config.toml is required before recovery operations are available."
			}
		}
		statuses = append(statuses, status)
	}
	return statuses
}

func existingCodexDirectory(path string) (bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return false, fmt.Errorf("Codex home is not a directory")
		}
		return true, nil
	}
	if errorsIsNotExist(err) {
		return false, nil
	}
	return false, err
}

func errorsIsNotExist(err error) bool {
	return err != nil && os.IsNotExist(err)
}

func redactCodexInspectionError(err error) string {
	if err == nil {
		return ""
	}
	return "Local Codex configuration inspection did not complete safely."
}

var _ agent.Adapter = (*codexAgentAdapter)(nil)
var _ agent.DiagnosticProvider = (*codexAgentAdapter)(nil)
