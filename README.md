<p align="center"><img src="macos/source/frontend/public/xiass-tools-logo.png" alt="XIASS Tools" width="96"></p>

# XIASS Tools

面向 macOS 和 Windows 的本机 Agent 配置、连接与诊断工具。XIASS Tools 保留已验证的 Antigravity 工作流，并提供 Codex 的安全本机配置管理、统一工具中心、账户与上游模型管理、双重验证器、更新与隐私脱敏诊断。

[![XIASS Tools release](https://img.shields.io/github/v/release/xyf0104/Antigravity-WF-Assistant?label=XIASS%20Tools%20release)](https://github.com/xyf0104/Antigravity-WF-Assistant/releases/latest)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
![macOS Universal](https://img.shields.io/badge/macOS-Universal-5b5b5b)
![Windows x64](https://img.shields.io/badge/Windows-x64-0078d4)

## 已实现的能力

- **Antigravity**：自动发现 Antigravity IDE 与 Antigravity 2.x 安装，管理本地代理、自定义上游模型、账户池、模型测试、启动/重启、补丁备份/恢复、历史恢复、自动批准、图片链路和脱敏诊断。
- **Codex**：安全读取并管理本机 `config.toml` 中独立的 `xiass_tools` Provider；支持 API 地址规范化、上游模型发现、原子写入、校验备份/恢复、workspace 状态修复与按需历史兼容修复。对于已验证的 Codex / ChatGPT Desktop，用户可在两次明确确认后执行“保存、可选历史修复并启动”的受保护事务；不会强制结束进程。不会读取 `auth.json`，不会伪造官方账号、订阅等级或额度。
- **Claude Code**：仅安全管理用户 `settings.json` 中明确的 API 根地址、授权令牌和模型；每次写入都通过原子备份与读回校验完成，不读取登录、OAuth、会话、项目配置或 MCP。
- **Cursor / Windsurf**：工具中心只检查公开安装信息；已确认客户端安装且全局 MCP JSON 结构安全时，可添加或更新 XIASS Tools 自己的远程 MCP 条目，并列出、恢复或删除本工具创建且通过校验的恢复点。不会读取或改写账号、Cookie、Token、聊天记录、数据库、其他 MCP 条目或未公开存储。
- **工具中心**：真实检查 Antigravity、Codex、Claude Code、Cursor 和 Windsurf 的本机安装与公开版本信息。每项操作仅在本机已验证可用时启用；已实现但当前结构不安全的配置会显示“暂不可配置”，而不是伪装为可用。
- **本地验证器（2FA）**：导入标准 `otpauth://totp/` 链接或手动 Base32 密钥；密钥仅保存在 macOS Keychain 或 Windows Credential Manager，可按需显示短时验证码，并能使用密码加密导出。
- **账户与上游**：支持 API Key、Bearer / Access Token、`x-api-key`、Setup Token、Codex PAT、自定义认证头、账户 JSON、Refresh Token 和已实现的 OAuth 流程；支持模型发现、连接测试和安全重试。
- **更新与诊断**：更新器只接受本项目公开 Release 的标准安装包，并在安装前校验 `SHA256SUMS.txt`。诊断包会二次隐藏密钥、Token、Cookie、授权码、用户目录和图片数据，不会主动打包账户、模型或聊天历史。

## 下载与安装

请从 [最新发布页](https://github.com/xyf0104/Antigravity-WF-Assistant/releases/latest) 下载与 Release 标签匹配的标准安装包。该公开地址暂时保留历史仓库路径，作为已验证的发布与更新通道，不是 XIASS Tools 的显示名称。当前发布准备版本为 v1.6.5；它高于历史 v1.6.4 基线，不会覆盖或冒充已有发布。Release 只提供两个系统的安装包和更新校验所需的 `SHA256SUMS.txt`，不提供 portable、独立 EXE 或 ZIP：

| 系统 | 安装包 | 支持范围 |
| --- | --- | --- |
| macOS | `XIASS-Tools-macOS-universal-v<version>-Installer.pkg` | Apple Silicon 与 Intel Mac |
| Windows | `XIASS-Tools-Windows-x64-v<version>-Setup.exe` | Windows 10/11 x64 |

Windows 安装时可以勾选创建桌面快捷方式。macOS 安装完成后可从“应用程序”或 Launchpad 打开；若系统拦截未签名应用，请在 Finder 中按住 Control 点按安装包或 App 后选择“打开”，并在“系统设置 → 隐私与安全性”中确认。

从旧版“Antigravity WF助手”升级到 XIASS Tools 时，macOS 会保留旧 App 与原有数据，避免安装程序擅自删除用户文件。请先退出旧 App，确认 XIASS Tools 能正常启动；确认不再需要旧 App 后，再由你手动移至废纸篓。

安装包包含运行时依赖，不需要额外安装 Python、Node.js 或补丁脚本。运行数据默认保存在 `~/.xiass-tools/`；升级时会从旧目录安全迁移已有数据，不会删除原目录。

## 快速开始

### 配置 Antigravity

1. 打开 XIASS Tools；本地代理会自动准备，但只有在 Antigravity 工作流需要时才会实际接管连接。
2. 在“账户池”添加 API Key、Token、账户 JSON 或 OAuth 账户；或者在“模型”中添加上游、获取模型并完成测试。
3. 在“总览”确认已发现的 Antigravity 安装，然后完全退出目标应用，选择“全部连接”或单独连接目标。
4. 连接完成后从首页启动目标应用；使用自定义模型期间保持 XIASS Tools 运行。

### 配置 Codex

1. 打开“工具 → Codex → 配置”。
2. 输入上游 API 地址和仅用于本次写入的 API Key，获取可用模型后选择默认模型与审查模型。
3. 保存后，XIASS Tools 只写入独立的 `model_providers.xiass_tools` 配置块，并自动创建校验备份。
4. 如出现旧会话兼容问题，再主动选择历史修复；普通保存不会改写本机历史。

如需一次完成“保存、可选历史兼容检查并启动 Codex Desktop”，展开配置窗口中的“高级：安全保存、检查历史并启动 Codex”。该操作要求风险确认；若任一步无法安全验证，XIASS Tools 会停止操作，并在可验证的范围内回滚，不会强制结束 Codex。

### 配置 Cursor 或 Windsurf MCP

1. 打开“工具 → Cursor”或“工具 → Windsurf”，选择“配置”。
2. 仅在本机已确认客户端且现有全局 MCP 设置通过安全检查时，输入远程 MCP 地址并保存。
3. 展开“经过校验的恢复点”可查看本工具为显式保存或恢复操作创建的恢复点；恢复与删除都需要再次确认。

该功能只管理 XIASS Tools 自己保留的全局 MCP 条目。列表不会展示地址、路径、原始 JSON、请求头、环境变量或认证信息；如果原文件带有敏感配置，XIASS Tools 会把它作为只读保护处理，保持零写入。

### 管理本地验证器

1. 打开“设置 → 验证器（2FA）”。
2. 粘贴标准验证器链接，或输入 Base32 密钥和识别名称。
3. 需要验证码时点击“显示验证码”，再按需复制；密钥不会回显。

## 兼容性与隐私边界

Antigravity 的连接操作按**实际安装结构**验证，而不是承诺任意未知版本都可以安全注入。遇到未识别的结构，XIASS Tools 会保留原文件并提示需要诊断或适配，不会强制覆盖未知 JavaScript、ASAR 或二进制。

所有需要修改本机配置的动作都应由用户主动触发，并使用原子写入与可恢复备份。只有在用户明确确认的 Codex Desktop 操作中，XIASS Tools 才会启动、请求正常退出或重新打开已验证的 Codex / ChatGPT Desktop；它不会强制结束进程，也不会隐式关闭或接管 Claude Code、Cursor、Windsurf 等其他应用。XIASS Tools 不会读取未经授权的账号、密码、Token 或第三方应用数据库。

遇到问题时，请使用“设置 → 诊断与日志 → 导出诊断日志”。请勿上传 API Key、Token、Cookie、账户 JSON、模型配置或未经脱敏的日志。

## 构建与贡献

macOS 与 Windows 的源代码、构建命令和平台说明分别位于：

- [macOS 源码说明](macos/source/README.md)
- [Windows 源码说明](windows/source/README.md)
- [系统架构与能力边界](docs/XIASS-Tools-architecture.md)

## 许可证与声明

代码按 [MIT License](LICENSE) 发布。Antigravity、Codex、Claude Code、Cursor 与 Windsurf 的名称及商标归各自权利人所有；XIASS Tools 是独立的本地辅助工具，不代表任何相关产品的官方授权或背书。使用第三方模型服务时，请遵守相应服务条款。
