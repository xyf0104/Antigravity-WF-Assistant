package main

import (
	"context"
	"fmt"
	"time"

	"antigravity-wf-assistant/internal/agent"
	"antigravity-wf-assistant/internal/agentdiscovery"
	"antigravity-wf-assistant/internal/mcpconfig"
)

// mcpAgentAdapter binds Cursor to its documented global MCP configuration and
// a project MCP file only after an explicit native project selection. Windsurf
// remains global-only. Neither integration inspects private account data,
// browser storage, conversations, or undocumented client files.
type mcpAgentAdapter struct {
	id        agent.ID
	target    mcpconfig.Target
	discovery agent.Adapter
}

func newCursorMCPAgentAdapter() *mcpAgentAdapter {
	return newMCPAgentAdapter(agent.CursorID, mcpconfig.TargetCursor, agentdiscovery.NewCursorAdapter())
}

func newWindsurfMCPAgentAdapter() *mcpAgentAdapter {
	return newMCPAgentAdapter(agent.WindsurfID, mcpconfig.TargetWindsurf, agentdiscovery.NewWindsurfAdapter())
}

func newMCPAgentAdapter(id agent.ID, target mcpconfig.Target, discovery agent.Adapter) *mcpAgentAdapter {
	return &mcpAgentAdapter{id: id, target: target, discovery: discovery}
}

func (adapter *mcpAgentAdapter) Metadata() agent.Metadata {
	for _, metadata := range agent.BuiltinMetadata() {
		if metadata.ID == adapter.id {
			return metadata
		}
	}
	panic("MCP client agent metadata is not registered")
}

func (adapter *mcpAgentAdapter) Detect(ctx context.Context) (agent.Status, error) {
	if adapter == nil || adapter.discovery == nil {
		return agent.Status{}, fmt.Errorf("MCP client adapter is unavailable")
	}
	discovered, err := adapter.discovery.Detect(ctx)
	if err != nil {
		return agent.Status{}, err
	}
	metadata := adapter.Metadata()
	manager, managerErr := mcpconfig.NewDefaultManager(adapter.target)
	if managerErr != nil {
		return mcpAgentStatus(metadata, discovered, mcpconfig.Snapshot{Target: adapter.target}, false, false), nil
	}
	snapshot, inspectErr := manager.Inspect()
	return mcpAgentStatus(metadata, discovered, snapshot, inspectErr == nil, inspectErr == nil), nil
}

func (adapter *mcpAgentAdapter) Diagnose(ctx context.Context) ([]agent.Diagnostic, error) {
	status, err := adapter.Detect(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	diagnostics := make([]agent.Diagnostic, 0, 2)
	if !mcpClientDetected(status) {
		diagnostics = append(diagnostics, agent.Diagnostic{
			AgentID: adapter.id, Code: string(adapter.id) + ".not-installed", Severity: agent.SeverityInfo,
			Summary:     adapter.Metadata().DisplayName + " was not found",
			Detail:      mcpNotInstalledDetail(adapter.Metadata()),
			Remediation: "Install or open the client, then run the local check again.", CreatedAt: now,
		})
		return diagnostics, nil
	}
	if status.Details["mcpConfigSafe"] != "true" {
		diagnostics = append(diagnostics, agent.Diagnostic{
			AgentID: adapter.id, Code: string(adapter.id) + ".mcp-config-unsafe", Severity: agent.SeverityWarning,
			Summary:     "Global MCP configuration cannot be safely changed",
			Detail:      "XIASS Tools detected an invalid or sensitive configuration and did not expose or modify its contents.",
			Remediation: "Resolve the configuration in the client, then run the local check again.", CreatedAt: now,
		})
		return diagnostics, nil
	}
	if status.Details["mcpManaged"] != "true" {
		diagnostics = append(diagnostics, agent.Diagnostic{
			AgentID: adapter.id, Code: string(adapter.id) + ".mcp-not-configured", Severity: agent.SeverityInfo,
			Summary:     "Global MCP configuration is ready",
			Detail:      "No XIASS Tools MCP entry is currently active.",
			Remediation: "Use the configuration action to add the reserved XIASS Tools remote MCP entry.", CreatedAt: now,
		})
	}
	return diagnostics, nil
}

func mcpAgentStatus(metadata agent.Metadata, discovered agent.Status, snapshot mcpconfig.Snapshot, inspected, inspectSafe bool) agent.Status {
	clientDetected := mcpClientDetected(discovered)
	configurationSafe := inspected && inspectSafe && snapshot.Valid && !snapshot.HasSensitiveConfiguration
	status := discovered
	status.AgentID = metadata.ID
	status.DisplayName = metadata.DisplayName
	if status.Details == nil {
		status.Details = make(map[string]string)
	}
	status.Details["mcpConfigPresent"] = fmt.Sprintf("%t", snapshot.Exists)
	status.Details["mcpConfigValid"] = fmt.Sprintf("%t", snapshot.Valid && inspected)
	status.Details["mcpConfigSafe"] = fmt.Sprintf("%t", configurationSafe)
	status.Details["mcpSensitive"] = fmt.Sprintf("%t", snapshot.HasSensitiveConfiguration)
	status.Details["mcpManaged"] = fmt.Sprintf("%t", snapshot.ManagedServerConfigured)

	switch {
	case !clientDetected:
		status.Message = metadata.DisplayName + " was not found. Its global MCP configuration will not be changed."
	case !configurationSafe:
		status.State = agent.StateDegraded
		if snapshot.HasSensitiveConfiguration {
			status.Message = "The global MCP configuration contains sensitive values and is read-only in XIASS Tools."
		} else {
			status.Message = "The global MCP configuration could not be safely validated."
		}
	case snapshot.ManagedServerConfigured:
		status.State = agent.StateReady
		status.Message = "The XIASS Tools MCP entry is configured in the verified global MCP configuration."
	case status.State == agent.StateReady || status.State == agent.StateDetected:
		status.State = agent.StateDetected
		status.Message = metadata.DisplayName + " is installed and its global MCP configuration is ready to be configured."
	}
	status.Capabilities = mcpAgentCapabilities(metadata, clientDetected, configurationSafe)
	return status
}

func mcpAgentCapabilities(metadata agent.Metadata, clientDetected, configurationSafe bool) []agent.CapabilityStatus {
	statuses := make([]agent.CapabilityStatus, 0, len(metadata.Capabilities))
	for _, declaration := range metadata.Capabilities {
		status := agent.CapabilityStatus{
			Capability: declaration.Capability, Availability: declaration.Availability,
			Reason: mcpUnsupportedCapabilityReason(metadata),
		}
		switch declaration.Capability {
		case agent.CapabilityDiscovery:
			status.Availability = agent.CapabilityAvailable
			status.Available = true
			status.Reason = "Local application discovery completed without reading private account data."
		case agent.CapabilityConfiguration:
			status.Availability = agent.CapabilityAvailable
			status.Available = clientDetected && configurationSafe
			if status.Available {
				status.Reason = mcpAvailableConfigurationReason(metadata)
			} else if !clientDetected {
				status.Reason = mcpUndetectedConfigurationReason(metadata)
			} else {
				status.Reason = mcpUnsafeConfigurationReason(metadata)
			}
		case agent.CapabilityDiagnostics:
			status.Availability = agent.CapabilityAvailable
			status.Available = true
			status.Reason = mcpDiagnosticsReason(metadata)
		case agent.CapabilityBackup:
			// The bridge can list, restore, and delete only verified recovery
			// points created by explicit MCP Apply/Restore operations. It
			// intentionally does not implement the registry's broad BackupProvider
			// contract, which could imply account, session, or arbitrary filesystem
			// backups. Cursor keeps global and explicit-project recovery points
			// isolated; Windsurf has only global recovery points.
			status.Availability = agent.CapabilityNotImplemented
			status.Available = false
			status.Reason = mcpRecoveryPointReason(metadata)
		}
		statuses = append(statuses, status)
	}
	return statuses
}

func mcpSupportsExplicitProjectConfiguration(metadata agent.Metadata) bool {
	return metadata.ID == agent.CursorID
}

func mcpNotInstalledDetail(metadata agent.Metadata) string {
	if mcpSupportsExplicitProjectConfiguration(metadata) {
		return "XIASS Tools will not create or change the global MCP configuration until Cursor is locally detected. A separately selected Cursor project can still be configured through the explicit project MCP action."
	}
	return "XIASS Tools will not create or change this client's global MCP configuration until the client is locally detected."
}

func mcpUnsupportedCapabilityReason(metadata agent.Metadata) string {
	if mcpSupportsExplicitProjectConfiguration(metadata) {
		return "This capability is not implemented for Cursor's documented global MCP configuration or an explicitly selected project MCP configuration."
	}
	return "This capability is not implemented for the documented global MCP configuration."
}

func mcpAvailableConfigurationReason(metadata agent.Metadata) string {
	if mcpSupportsExplicitProjectConfiguration(metadata) {
		return "The documented global MCP configuration can be changed with an atomic backup and rollback. A separately selected project's .cursor/mcp.json can be handled through an explicit native project selection."
	}
	return "The documented global MCP configuration can be changed with an atomic backup and rollback."
}

func mcpUndetectedConfigurationReason(metadata agent.Metadata) string {
	if mcpSupportsExplicitProjectConfiguration(metadata) {
		return "Cursor must be locally detected before its global MCP configuration can be changed. A separately selected project can still use the explicit project MCP action."
	}
	return "The client must be locally detected before its MCP configuration can be changed."
}

func mcpUnsafeConfigurationReason(metadata agent.Metadata) string {
	if mcpSupportsExplicitProjectConfiguration(metadata) {
		return "The existing global MCP configuration must be safe and valid before it can be changed. Project MCP configuration is checked separately after an explicit project selection."
	}
	return "The existing MCP configuration must be safe and valid before it can be changed."
}

func mcpDiagnosticsReason(metadata agent.Metadata) string {
	if mcpSupportsExplicitProjectConfiguration(metadata) {
		return "Credential-free global MCP diagnostics are available. Project MCP diagnostics occur only after an explicit project selection."
	}
	return "Credential-free local MCP configuration diagnostics are available."
}

func mcpRecoveryPointReason(metadata agent.Metadata) string {
	if mcpSupportsExplicitProjectConfiguration(metadata) {
		return "Verified recovery points are limited to explicit global MCP or explicitly selected project MCP Apply or Restore actions; no general agent backup provider is exposed."
	}
	return "Verified recovery points are limited to explicit global MCP configuration Apply or Restore actions; no general agent backup provider is exposed."
}

func mcpClientDetected(status agent.Status) bool {
	return status.State == agent.StateReady || status.State == agent.StateDetected
}

var _ agent.Adapter = (*mcpAgentAdapter)(nil)
var _ agent.DiagnosticProvider = (*mcpAgentAdapter)(nil)
