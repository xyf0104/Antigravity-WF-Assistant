package main

import (
	"errors"
	"strings"

	"antigravity-byok/internal/agent"
	"antigravity-byok/internal/agentdiscovery"
	"antigravity-byok/internal/mcpconfig"
)

// MCPConfigurationInput is intentionally limited to a remote endpoint. XIASS
// Tools does not collect, retain, or write headers, environment values, OAuth
// material, cookies, API keys, or other credentials into Cursor/Windsurf MCP
// configuration files.
type MCPConfigurationInput struct {
	Target    string `json:"target"`
	RemoteURL string `json:"remoteUrl"`
}

// MCPConfigurationStatus is safe for the renderer: the configuration snapshot
// deliberately omits paths, endpoint values, server command lines, headers,
// env entries, backup IDs, and all secret material.
type MCPConfigurationStatus struct {
	OK             bool               `json:"ok"`
	Message        string             `json:"message"`
	ClientDetected bool               `json:"clientDetected"`
	CanApply       bool               `json:"canApply"`
	Snapshot       mcpconfig.Snapshot `json:"snapshot"`
}

func (a *App) GetMCPConfiguration(target string) MCPConfigurationStatus {
	resolved, ok := parseMCPConfigurationTarget(target)
	if !ok {
		return MCPConfigurationStatus{Message: "不支持的 MCP 客户端。"}
	}
	return a.mcpConfigurationStatus(resolved)
}

func (a *App) ApplyMCPConfiguration(input MCPConfigurationInput) MCPConfigurationStatus {
	resolved, ok := parseMCPConfigurationTarget(input.Target)
	if !ok {
		return MCPConfigurationStatus{Message: "不支持的 MCP 客户端。"}
	}
	status := a.mcpConfigurationStatus(resolved)
	if !status.ClientDetected {
		status.Message = "尚未在本机确认该客户端。为避免创建无效配置，XIASS Tools 不会写入全局 MCP 设置。"
		return status
	}
	if !status.Snapshot.Valid || status.Snapshot.HasSensitiveConfiguration {
		status.Message = "现有全局 MCP 设置无法安全修改。XIASS Tools 不会读取、展示或改写其中的敏感内容。"
		return status
	}

	manager, err := mcpconfig.NewDefaultManager(resolved)
	if err != nil {
		status.Message = "无法安全定位该客户端的全局 MCP 设置。"
		return status
	}
	result, err := manager.ApplyRemote(mcpconfig.ApplyInput{RemoteURL: input.RemoteURL})
	input.RemoteURL = ""
	if err != nil {
		status.Message = mcpConfigurationErrorMessage(err)
		return status
	}
	return MCPConfigurationStatus{
		OK:             true,
		Message:        "已安全保存 XIASS Tools 的 MCP 远程连接，并完成写入校验与本地恢复点备份。",
		ClientDetected: true,
		CanApply:       result.Snapshot.Valid && !result.Snapshot.HasSensitiveConfiguration,
		Snapshot:       result.Snapshot,
	}
}

func (a *App) mcpConfigurationStatus(target mcpconfig.Target) MCPConfigurationStatus {
	clientDetected := a.mcpTargetDetected(target)
	status := MCPConfigurationStatus{ClientDetected: clientDetected, Snapshot: mcpconfig.Snapshot{Target: target}}
	manager, err := mcpconfig.NewDefaultManager(target)
	if err != nil {
		status.Message = "无法安全定位该客户端的全局 MCP 设置。"
		return status
	}
	snapshot, err := manager.Inspect()
	status.Snapshot = snapshot
	if err != nil {
		status.Message = "全局 MCP 设置无法通过安全校验。"
		return status
	}
	status.OK = true
	status.CanApply = clientDetected && snapshot.Valid && !snapshot.HasSensitiveConfiguration
	switch {
	case !clientDetected:
		status.Message = "尚未在本机确认该客户端；不会创建或修改其全局 MCP 设置。"
	case snapshot.HasSensitiveConfiguration:
		status.Message = "检测到敏感 MCP 设置。内容保持私密，XIASS Tools 已将该文件设为只读。"
	case !snapshot.Valid:
		status.Message = "全局 MCP 设置格式无效，修复前不会进行写入。"
	case snapshot.ManagedServerConfigured:
		status.Message = "XIASS Tools MCP 远程连接已配置。"
	default:
		status.Message = "全局 MCP 设置已验证，可以添加 XIASS Tools MCP 远程连接。"
	}
	return status
}

func (a *App) mcpTargetDetected(target mcpconfig.Target) bool {
	ctx, cancel := a.agentOperationContext()
	defer cancel()
	var adapter agent.Adapter
	switch target {
	case mcpconfig.TargetCursor:
		adapter = agentdiscovery.NewCursorAdapter()
	case mcpconfig.TargetWindsurf:
		adapter = agentdiscovery.NewWindsurfAdapter()
	default:
		return false
	}
	status, err := adapter.Detect(ctx)
	return err == nil && mcpClientDetected(status)
}

func parseMCPConfigurationTarget(target string) (mcpconfig.Target, bool) {
	switch strings.TrimSpace(strings.ToLower(target)) {
	case string(mcpconfig.TargetCursor):
		return mcpconfig.TargetCursor, true
	case string(mcpconfig.TargetWindsurf):
		return mcpconfig.TargetWindsurf, true
	default:
		return "", false
	}
}

func mcpConfigurationErrorMessage(err error) string {
	switch {
	case errors.Is(err, mcpconfig.ErrInvalidRemote):
		return "远程地址必须是 HTTPS，或无凭据的本机 localhost/回环 HTTP 地址。"
	case errors.Is(err, mcpconfig.ErrUnsafeConfiguration):
		return "现有全局 MCP 设置含有敏感或不安全内容，XIASS Tools 未做任何修改。"
	case errors.Is(err, mcpconfig.ErrInvalidConfiguration):
		return "现有全局 MCP 设置格式无效，XIASS Tools 未做任何修改。"
	case errors.Is(err, mcpconfig.ErrOperationBusy):
		return "另一项 MCP 设置操作正在进行，请完成后重试。"
	default:
		return "未能安全保存 MCP 远程连接；现有设置已保持不变。"
	}
}
