# XIASS Tools v1.6.5

v1.6.5 是 XIASS Tools 的双平台安装包发布准备版本。它继续保留经过验证的 Antigravity 本地代理、模型注入、补丁、账户池、图片、重试、历史恢复与诊断链路，并扩展一站式工具中心中已实现的本机配置和恢复能力。

## 本版重点

- **图片路由一致性**：Windows 与 macOS 现在使用相同的原生图片模型恢复逻辑。用户从自定义模型切换回 Gemini 原生模型后，旧版本残留的自定义图片标记不会再把新的 Gemini 图片请求错误路由到 `gpt-image-2`；同一自定义会话中的图片生成仍优先使用已启用的自定义图片模型。
- **Codex 本机配置**：保留独立 `xiass_tools` Provider、模型发现、原子写入、校验备份/恢复、按需历史兼容检查和工作区修复。已验证的 Codex / ChatGPT Desktop 可在用户明确确认后采用“正常退出 → 保存 → 可选兼容修复 → 启动”的事务流程，不会强制结束进程。
- **其他本机工具**：Claude Code 可管理明确的 `settings.json` API 配置；Cursor 与 Windsurf 可在安全条件下管理 XIASS Tools 自有 MCP 条目及恢复点；本地 2FA 验证器使用系统凭据库存储密钥并支持加密导出。
- **发布保护**：构建流程会在原生 Wails 构建后验证生成的前后端绑定；macOS 安装包会拒绝包含额外可执行文件的 App Bundle。标准 Release 仍只提供 macOS Universal PKG、Windows x64 Setup EXE 和 `SHA256SUMS.txt`。

## 使用与兼容性

Antigravity 的补丁与模型注入以实际安装结构为准。无法识别或无法验证的结构会保持零写入并提供脱敏诊断信息，而不会强制替换未知文件。安装后请使用应用内的“工具中心”确认目标客户端的真实检测状态。

## macOS 首次打开

本版安装包可在未公证环境中发布，但未使用 Apple Developer ID 公证。若 Gatekeeper 阻止首次启动，请在 Finder 中按住 Control 点按 App 后选择“打开”，或在“系统设置 → 隐私与安全性”中确认打开；这不会影响已通过应用内部完整性校验的安装包结构。
