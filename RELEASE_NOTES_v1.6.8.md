# XIASS Tools v1.6.8

XIASS Tools 现以多 Agent 母应用形式组织功能。Antigravity WF、Codex、Claude Code、Cursor 与 Windsurf 分别进入独立模块；Antigravity 的模型、上游账户、代理和权限不再与其他客户端的配置入口混用。

## 主要内容

- 左侧直接列出 Antigravity WF、Codex、Claude Code、Cursor 与 Windsurf；点击后只进入对应 Agent 的独立功能区，不再经过重复的平台选择器。
- 首次启动默认进入 `Antigravity WF → 总览`；Antigravity 内部使用 `总览`、`模型`、`上游` 和 `权限` 子导航，全局设置固定在左侧底部。
- 五个 Agent 使用独立矢量图标和识别色，不再使用字母占位；深浅主题的辅助文字对比度与功能栏字号同步提高。
- Codex 明确展示 Provider、模型发现、备份恢复、历史兼容、Desktop 与诊断；Claude Code 展示网关、模型测试、备份恢复与诊断；Cursor / Windsurf 分别展示其已实现的 MCP、恢复点、应用选择、启动与诊断入口。
- 本地浏览器预览会展示完整 Agent 信息架构，但保持“等待检查”并禁用所有原生操作，不会伪造安装、登录或可用状态。
- 深色主题采用深海军蓝背景、蓝灰卡片与橙色主操作，并提高辅助文字在高分屏上的可读性；浅色与跟随系统继续保留。
- OpenAI / Codex OAuth 增加预设隔离、自动回调、手动兜底、临期 Token 刷新和显式账户操作预检。
- 暂时隐藏尚无专用推理、模型和额度链路的 Grok、Claude、Google/Gemini 一键 OAuth 入口，避免出现“授权成功但不能使用”。
- Codex、Claude Code、Cursor 与 Windsurf 不再跳入 Antigravity 上游账户页；兼容凭据只能在各自的原生配置模块中选择。
- Cursor / Windsurf 增加原生应用选择与启动前结构复检，选择路径不会进入渲染层或持久化文件。
- Codex 与 Claude Code 的配置写入继续使用原子备份、读回校验和失败回滚；账户凭据不返回界面。
- 增加本地 TOTP 加密导入恢复、OAuth 验证码快选、账户搜索筛选、批量启停和私有 Header 隔离。
- Windows 安装器在主程序复制前检测 WebView2 Runtime；缺失时仅从 Microsoft 官方地址下载并验证数字签名，安装失败会安全中止。
- Windows 运行中升级/卸载遇到主程序锁定时安全中止，不会留下半安装或半卸载状态。
- macOS PKG 和 GitHub Release 增加严格单 App、精确附件和 SHA-256 清单门禁。

## 安装包

- macOS Universal：`XIASS-Tools-macOS-universal-v1.6.8-Installer.pkg`
- Windows x64：`XIASS-Tools-Windows-x64-v1.6.8-Setup.exe`
- 校验和：`SHA256SUMS.txt`

Release 不提供 portable、独立 EXE 或 ZIP 附件。
