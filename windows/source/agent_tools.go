package main

import (
	"context"
	"strings"
	"time"

	"antigravity-byok/internal/agent"
)

// AgentDiagnosticsResult keeps Wails callers on a normal result path for
// expected local conditions such as an absent installation or an unbound
// adapter. The diagnostics themselves are structured and credential-free.
type AgentDiagnosticsResult struct {
	OK          bool               `json:"ok"`
	Message     string             `json:"message"`
	Diagnostics []agent.Diagnostic `json:"diagnostics,omitempty"`
}

// GetAgentStatuses runs the normal bounded local discovery pass. It is safe
// to call at application startup because every adapter either reads public
// local metadata or uses a command with its own short timeout.
func (a *App) GetAgentStatuses() agent.AggregateStatus {
	return a.detectAgents(false)
}

// RefreshAgentStatuses explicitly asks Antigravity for a fresh structural
// scan, then re-runs all registered local discoverers. The mutex prevents two
// user clicks from starting overlapping deep scans of the same installations.
func (a *App) RefreshAgentStatuses() agent.AggregateStatus {
	return a.detectAgents(true)
}

func (a *App) detectAgents(refreshAntigravity bool) agent.AggregateStatus {
	if a.agentRegistry == nil {
		return unavailableAgentAggregate("The XIASS Tools agent registry is not available.")
	}
	if refreshAntigravity {
		a.agentRefreshMu.Lock()
		defer a.agentRefreshMu.Unlock()
	}
	ctx, cancel := a.agentOperationContext()
	defer cancel()
	if refreshAntigravity && a.antigravityAgent != nil {
		// Refresh returns a cached verified snapshot consumed immediately by
		// Registry.DetectAll below. Other discovery adapters still run in
		// parallel under the same bounded context.
		_, _ = a.antigravityAgent.Refresh(ctx)
	}
	return a.agentRegistry.DetectAll(ctx)
}

// DiagnoseAgent returns target-specific diagnostics only for a registered
// integration. It never performs a patch, starts an application, or exposes
// credentials in its response.
func (a *App) DiagnoseAgent(agentID string) AgentDiagnosticsResult {
	identifier := agent.ID(strings.TrimSpace(agentID))
	if err := identifier.Validate(); err != nil {
		return AgentDiagnosticsResult{OK: false, Message: "无效的工具标识。"}
	}
	if a.agentRegistry == nil {
		return AgentDiagnosticsResult{OK: false, Message: "工具中心尚未完成初始化。"}
	}
	ctx, cancel := a.agentOperationContext()
	defer cancel()
	diagnostics, err := a.agentRegistry.Diagnose(ctx, identifier)
	if err != nil {
		// Adapters may carry filesystem details in their native errors. Keep the
		// renderer result stable and credential-free; structured diagnostics are
		// the only user-visible diagnostic channel.
		return AgentDiagnosticsResult{OK: false, Message: "无法完成本机诊断。请检查该工具是否已正确安装，或导出脱敏诊断日志。", Diagnostics: diagnostics}
	}
	return AgentDiagnosticsResult{OK: true, Message: "本机诊断已完成。", Diagnostics: diagnostics}
}

func (a *App) agentOperationContext() (context.Context, context.CancelFunc) {
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, 6*time.Second)
}

func unavailableAgentAggregate(message string) agent.AggregateStatus {
	return agent.AggregateStatus{
		GeneratedAt: time.Now().UTC(),
		State:       agent.StateError,
		Diagnostics: []agent.Diagnostic{{
			Code:      "agent.registry-unavailable",
			Severity:  agent.SeverityError,
			Summary:   "Agent registry is unavailable",
			Detail:    message,
			CreatedAt: time.Now().UTC(),
		}},
	}
}
