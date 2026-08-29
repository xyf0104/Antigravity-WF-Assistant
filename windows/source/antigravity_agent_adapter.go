package main

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"antigravity-byok/internal/agent"
	"antigravity-byok/internal/patcher"
)

// antigravityAgentAdapter projects the existing Antigravity integration onto
// the generic XIASS Tools agent contract. It deliberately delegates all
// mutation to the established patch and launch actions in App; discovery here
// is read-only and cannot write into an Antigravity installation.
type antigravityAgentAdapter struct {
	mu        sync.RWMutex
	cached    agent.Status
	cachedAt  time.Time
	cacheLife time.Duration
}

func newAntigravityAgentAdapter() *antigravityAgentAdapter {
	return &antigravityAgentAdapter{cacheLife: 15 * time.Second}
}

func (adapter *antigravityAgentAdapter) Metadata() agent.Metadata {
	for _, metadata := range agent.BuiltinMetadata() {
		if metadata.ID == agent.AntigravityID {
			return metadata
		}
	}
	panic("Antigravity agent metadata is not registered")
}

// Detect keeps first paint responsive by using the existing bounded quick
// scanner. A user-initiated refresh is handled by Refresh, which stores the
// authoritative deep scan for the registry's next read.
func (adapter *antigravityAgentAdapter) Detect(ctx context.Context) (agent.Status, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return agent.Status{}, err
		}
	}
	if status, ok := adapter.cachedStatus(); ok {
		return status, nil
	}
	status := antigravityAgentStatus(adapter.Metadata(), patcher.GetQuickStatus())
	adapter.store(status)
	return status, nil
}

// Refresh performs the same authoritative structural scan used by the
// dashboard Refresh action. It never treats a discovered executable as safe
// to patch unless the existing patcher marked that target supported.
func (adapter *antigravityAgentAdapter) Refresh(ctx context.Context) (agent.Status, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return agent.Status{}, err
		}
	}
	status := antigravityAgentStatus(adapter.Metadata(), patcher.RefreshStatus())
	adapter.store(status)
	return status, nil
}

func (adapter *antigravityAgentAdapter) Diagnose(ctx context.Context) ([]agent.Diagnostic, error) {
	status, err := adapter.Detect(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	diagnostics := make([]agent.Diagnostic, 0, 3)
	supportedTargets, patchedTargets := targetCounts(status.Details)
	switch status.State {
	case agent.StateNotInstalled:
		diagnostics = append(diagnostics, agent.Diagnostic{
			AgentID: agent.AntigravityID, Code: "antigravity.not-installed", Severity: agent.SeverityInfo,
			Summary: "No Antigravity installation was detected", Detail: status.Message,
			Remediation: "Install Antigravity IDE or Antigravity 2.0, then run the local check again.", CreatedAt: now,
		})
	case agent.StateDegraded:
		diagnostics = append(diagnostics, agent.Diagnostic{
			AgentID: agent.AntigravityID, Code: "antigravity.requires-attention", Severity: agent.SeverityWarning,
			Summary: "Antigravity needs attention before it can be used safely", Detail: status.Message,
			Remediation: "Review the detected installation in the dashboard and apply a patch only when it is reported as supported.", CreatedAt: now,
		})
	}
	if supportedTargets > 0 && patchedTargets < supportedTargets {
		diagnostics = append(diagnostics, agent.Diagnostic{
			AgentID: agent.AntigravityID, Code: "antigravity.patch-incomplete", Severity: agent.SeverityWarning,
			Summary: "One or more supported installations are not connected", Detail: fmt.Sprintf("%d of %d supported installations are connected.", patchedTargets, supportedTargets),
			Remediation: "Use the existing Antigravity dashboard action to connect all supported installations.", CreatedAt: now,
		})
	}
	return diagnostics, nil
}

func (adapter *antigravityAgentAdapter) cachedStatus() (agent.Status, bool) {
	adapter.mu.RLock()
	defer adapter.mu.RUnlock()
	if adapter.cachedAt.IsZero() || time.Since(adapter.cachedAt) > adapter.cacheLife {
		return agent.Status{}, false
	}
	return adapter.cached, true
}

func (adapter *antigravityAgentAdapter) store(status agent.Status) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	adapter.cached = status
	adapter.cachedAt = time.Now()
}

func antigravityAgentStatus(metadata agent.Metadata, snapshot patcher.Status) agent.Status {
	targets := snapshot.Targets
	supportedTargets := 0
	patchedTargets := 0
	unsupportedTargets := 0
	var primary *patcher.TargetStatus
	for index := range targets {
		target := &targets[index]
		if target.Supported {
			supportedTargets++
			if target.Patched {
				patchedTargets++
			}
			if primary == nil || !primary.Supported {
				primary = target
			}
			continue
		}
		unsupportedTargets++
		if primary == nil {
			primary = target
		}
	}

	state := agent.StateDetected
	message := "Detected Antigravity installation; verify the compatibility status before applying changes."
	switch {
	case len(targets) == 0:
		state = agent.StateNotInstalled
		message = "No Antigravity IDE or Antigravity 2.0 installation was detected."
	case supportedTargets == 0:
		state = agent.StateDegraded
		message = "Antigravity was detected, but its current structure has not passed the compatibility check."
	case patchedTargets == supportedTargets && snapshot.ProxyListening:
		state = agent.StateReady
		message = "All supported Antigravity installations are connected to the local proxy."
	case patchedTargets > 0:
		state = agent.StateDegraded
		message = "Antigravity was detected, but some supported installations still need attention."
	case snapshot.ProxyListening:
		message = "A supported Antigravity installation was detected and is ready to be connected."
	default:
		message = "A supported Antigravity installation was detected; start the local proxy before connecting it."
	}

	status := agent.Status{
		AgentID:     agent.AntigravityID,
		DisplayName: metadata.DisplayName,
		State:       state,
		Message:     message,
		UpdatedAt:   time.Now().UTC(),
		Details: map[string]string{
			"targetCount":        fmt.Sprintf("%d", len(targets)),
			"supportedTargets":   fmt.Sprintf("%d", supportedTargets),
			"patchedTargets":     fmt.Sprintf("%d", patchedTargets),
			"unsupportedTargets": fmt.Sprintf("%d", unsupportedTargets),
		},
	}
	if primary != nil {
		status.Installation = agent.Installation{
			Root:           primary.AppPath,
			ExecutablePath: primary.ExecutablePath,
			Version:        primary.Version,
			Platform:       runtime.GOOS,
		}
		if primary.ConnectionMode != "" {
			status.Details["connectionMode"] = primary.ConnectionMode
		}
	}
	status.Capabilities = antigravityCapabilityStatuses(metadata, len(targets), supportedTargets, snapshot.ProxyListening)
	return status
}

func antigravityCapabilityStatuses(metadata agent.Metadata, targetCount, supportedTargets int, proxyListening bool) []agent.CapabilityStatus {
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
			if targetCount == 0 {
				status.Reason = "Local Antigravity discovery completed; no installation was found."
			} else {
				status.Reason = "Local Antigravity discovery completed."
			}
		case agent.CapabilityLocalProxy:
			status.Availability = agent.CapabilityAvailable
			status.Available = proxyListening
			if proxyListening {
				status.Reason = "The established local proxy is listening."
			} else {
				status.Reason = "The local proxy is not listening."
			}
		case agent.CapabilityConfiguration, agent.CapabilityPatchInjection, agent.CapabilityModelCatalog,
			agent.CapabilitySessionRecovery, agent.CapabilityImageIO, agent.CapabilityDiagnostics, agent.CapabilityBackup:
			status.Availability = agent.CapabilityAvailable
			status.Available = supportedTargets > 0
			if supportedTargets > 0 {
				status.Reason = "A compatible Antigravity installation was verified."
			} else {
				status.Reason = "No compatible Antigravity installation was verified."
			}
		}
		statuses = append(statuses, status)
	}
	return statuses
}

func targetCounts(details map[string]string) (int, int) {
	if len(details) == 0 {
		return 0, 0
	}
	var supported, patched int
	_, _ = fmt.Sscanf(strings.TrimSpace(details["supportedTargets"]), "%d", &supported)
	_, _ = fmt.Sscanf(strings.TrimSpace(details["patchedTargets"]), "%d", &patched)
	return supported, patched
}
