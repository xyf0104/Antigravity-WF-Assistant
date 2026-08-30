# XIASS Tools 完整功能差异审计（v1.6.8 基线）

更新时间：2026-08-30

## 1. 审计目的

本文件把“完整多 Agent 配置助手”拆成可验证的功能要求。每一项只能使用以下状态：

- `已实现`：存在真实后端、前端入口和针对该行为的测试或运行证据。
- `部分实现`：只有部分链路完成，不能对外宣称全功能。
- `缺失`：当前源码没有该功能，或只有不可操作的能力声明。
- `待实机验证`：实现存在，但尚未在对应真实客户端、真实账号或真实上游上完成端到端验证。

UI 标签、静态能力声明、截图相似度和浏览器预览都不能单独证明功能完成。

## 2. 商业实现与参考边界

`jlcodes99/cockpit-tools` 的 README 声明默认采用 CC BY-NC-SA 4.0，并明确禁止未经书面授权的商业集成、商业二次分发和付费产品使用。仓库没有提供一个可覆盖该限制的独立商业许可证文件。

XIASS Tools 的目标是商业软件，因此：

- 不直接复制 Cockpit 源码、固定 OAuth Client、品牌图标、广告、文案或受限资产；
- 可以把公开展示的功能作为需求参考；
- 具体实现必须使用 XIASS 自有代码、用户自有代码或许可证兼容的第三方组件；
- 使用第三方组件时保留其许可证和必要声明；
- 对没有公开稳定协议的账号、额度或客户端认证能力，必须通过真实链路验证后才启用。

该边界只约束实现方式，不缩小本文件的功能目标。

## 3. 品牌与安装包

| 要求 | 状态 | 证据 |
| --- | --- | --- |
| 产品名为 XIASS Tools | 已实现 | 双端 `wails.json`、安装器脚本、README 和 Release 元数据 |
| 使用用户提供的图 7 透明 Logo | 已实现 | 图 7与双端 `build/appicon.png` SHA-256 均为 `6ff633ce51267a5b68d3ad2955a9118855ccb87539d4af2474ec8e57e81de40a` |
| macOS Intel + Apple Silicon | 已实现 | v1.6.8 Universal 构建、PKG payload 和安装生命周期 CI |
| Windows x64 安装器 | 已实现 | v1.6.8 NSIS 构建和安装/卸载 CI |
| 只发布安装包，不发布 portable/ZIP | 已实现 | v1.6.8 Release 附件集合门禁 |
| macOS 公证 | 缺失 | v1.6.8 是用户允许的未公证包，不能宣称已公证 |
| Windows Authenticode 签名 | 缺失 | 当前发布没有可验证的商业代码签名证书 |

## 4. 母应用信息架构

| 要求 | 状态 | 证据 |
| --- | --- | --- |
| 左侧直接选择 Agent | 已实现 | 双端 `frontend/src/App.vue` |
| Antigravity WF、Codex、Claude Code、Cursor、Windsurf 五个独立模块 | 已实现 | 双端 `frontend/src/views/Tools.vue` |
| Agent 使用独立图标 | 已实现 | 双端 `frontend/src/components/AgentIcon.vue` |
| 深色、浅色、跟随系统 | 已实现 | 双端主题状态和 `frontend/src/style/global.css` |
| 不伪造未连接原生运行时状态 | 已实现 | `agentPreviewState.test.mjs`、`toolCenterIntegrity.test.mjs` |
| 各 Agent 完整账号/额度/会话/多开功能 | 部分实现 | 见下列逐平台矩阵 |

## 5. Antigravity WF

| 功能 | 状态 | 当前证据或缺口 |
| --- | --- | --- |
| IDE / Antigravity 2.x 路径和版本发现 | 已实现 | `internal/patcher` 和目标发现测试 |
| 代理、补丁、模型注入和恢复 | 已实现 | `internal/proxy`、`internal/patcher` |
| OpenAI/Claude/兼容 API 上游 | 已实现 | `internal/upstream`、账户与模型页面 |
| OpenAI Chat、Claude Messages、工具回传 | 已实现 | `internal/proxy` 协议和防重复测试 |
| 图片输入、文件、PDF | 已实现 | 代理媒体转换和 UI 补丁测试 |
| GPT Image 路由和 Gemini 原生图片链路 | 已实现 | 图片路由、预览、去重和集成测试 |
| 同账户重试和防重复回复 | 已实现 | 代理重试、请求 ID 和工具回传测试 |
| 自动批准、历史同步、诊断 | 已实现 | 权限、历史迁移和诊断导出 |
| 端口占用处理与退出释放 | 已实现 | 端口选择、启动/退出和托盘测试 |
| OpenAI/Codex OAuth | 已实现 | `oauth_profiles.go` 中 ready 预设，PKCE、自动回调、手动回调、刷新 |
| Claude/Google/Grok 原生 OAuth | 缺失 | 对应 profile 当前为 unavailable；不能只凭通用 OAuth 框架宣称可用 |
| Antigravity 原生 OAuth 预设 | 部分实现 | 当前仅 custom-only，需要用户提供公开客户端参数 |
| 官方账号等级、原生额度、分享 | 部分实现 | API/令牌账户可以测试和统计；并非所有原生客户端账号协议均已实现 |
| 应用多开、账号隔离实例 | 缺失 | 当前只检测并启动/重启已安装客户端，没有独立用户目录实例管理 |
| 唤醒任务 | 缺失 | 当前没有持久化调度器和任务 UI |

### 5.1 上游凭据存储边界

当前 `upstream_accounts.json` 仍包含 API Key、OAuth Access Token 和 Refresh Token。macOS 依赖 `0600` 文件权限；Windows 的原子替换不能等价于当前用户专属 protected DACL。完整商业版还需要：

- 将敏感凭据迁入 macOS Keychain / Windows Credential Manager；
- JSON 只保留非敏感账户元数据和系统凭据引用；
- 对旧 JSON 执行有恢复点的原子迁移；
- 迁移失败时恢复原文件，迁移成功后验证并清除旧明文；
- 保持诊断、日志、普通导出和 Vue 状态完全不含凭据。

## 6. Codex

### 6.1 XIASS Codex Helper 功能迁移

用户提供的正式 Helper：

- macOS ZIP SHA-256：`8ca8be58cf093619bbaadc50268f2794ae5cdc2e87cce9cfd250f805b828b188`
- Windows EXE SHA-256：`e46e9318c4fd7f981070cc7a6ecf5b649c279bb219748f77486b075495dd4a5d`
- 两端来自 `github.com/xyf0104/xiass-api/tools/xiass-codex-helper`，源码版本 1.0.99。

| Helper 能力 | 状态 | XIASS Tools 证据 |
| --- | --- | --- |
| Codex 安装发现和手选 | 已实现 | `codex_desktop_tools.go`、`internal/codexdesktop` |
| 配置读取和脱敏状态 | 已实现 | `codex_tools.go`、`internal/codexconfig/inspect.go` |
| 手动 Base URL / API Key | 已实现 | `ApplyCodexConfiguration` |
| XIASS 网站选择 Key、自动/手动回调 | 已实现 | `codex_key_selection.go` |
| `/v1/models` 模型发现 | 已实现 | `internal/codexconfig/models.go` |
| Provider、模型、审查模型、联网搜索 | 已实现 | `internal/codexconfig` 和 Codex 配置界面 |
| 235K/372K/512K/1M/自定义上下文 | 已实现 | `ApplyConfig` 和上下文预设 UI |
| 配置备份、恢复、删除 | 已实现 | `Manager.Apply/Restore/List/Delete` |
| 历史、SQLite/WAL、JSONL、workspace 修复 | 已实现 | `internal/codexconfig/history.go`、`workspace.go` |
| 旧 Helper 备份导入 | 已实现 | `legacy_import.go` |
| 旧 Provider 迁移 | 已实现 | `legacy_provider_migration.go` |
| Desktop 启动、正常退出、重启和事务回滚 | 已实现 | `codex_configuration_lifecycle.go`、`codex_desktop_tools.go` |
| 脱敏诊断 | 已实现 | `internal/diagnostics` |

### 6.2 Cockpit 式扩展功能

| 功能 | 状态 | 当前缺口 |
| --- | --- | --- |
| 官方 Codex 账号 OAuth 管理 | 部分实现 | 全局 OpenAI/Codex OAuth 可用，但不会强行映射为静态 Responses Provider |
| 账号计划、Hourly/Weekly 额度 | 部分实现 | 全局账户可以保存身份/套餐/额度；Codex 独立模块没有完整账号总览页面 |
| Provider/API 账号与官方账号统一管理 | 部分实现 | 静态 Responses 账户可复用；OAuth 与 API 账号仍是不同安全边界 |
| 实际 Responses 模型推理测试 | 缺失 | 当前只发现模型 ID，并明确不宣称推理已验证 |
| Codex 会话列表、搜索、导入、导出、废纸篓 | 缺失 | 当前只做兼容修复与备份，不提供完整会话管理 UI |
| 会话用量统计 | 缺失 | 当前未建立会话级 token/cost 索引 |
| 应用多开和账号隔离实例 | 缺失 | 当前控制一个经验证的 Desktop 安装 |
| 唤醒任务 | 缺失 | 当前无调度器 |

## 7. Claude Code

| 功能 | 状态 | 当前证据或缺口 |
| --- | --- | --- |
| 安装和版本发现 | 已实现 | `internal/agentdiscovery` |
| settings.json 网关、Token、模型配置 | 已实现 | `internal/claudeconfig` |
| 静态 Anthropic Messages 账户复用 | 已实现 | `claude_code_tools.go` |
| `/v1/models` 发现和 `/v1/messages` 测试 | 已实现 | `claude_code_tools.go`、httptest |
| 配置备份、恢复、删除和诊断 | 已实现 | `internal/claudeconfig`、诊断导出 |
| OAuth、Refresh Token 生命周期 | 缺失 | Claude OAuth profile 当前 unavailable；专属模块只接受静态凭据 |
| JSON/本机登录导入 | 缺失 | 没有经验证的 Claude Code 原生凭据导入器 |
| 账号等级和额度 | 缺失 | 没有官方订阅/额度查询适配器 |
| 会话和项目管理 | 缺失 | 没有 Claude Code 会话索引或恢复 UI |
| MCP 管理 | 缺失 | Claude 模块未接入独立 MCP 配置管理 |
| 多开、启动、退出、重启 | 缺失 | 当前不管理 Claude Desktop/CLI 生命周期 |

Windows 生产发现器已经在本轮改为复用平台发现逻辑，因此能够在 PATH 尚未刷新时检查 `%APPDATA%\npm\claude.cmd` / `claude.exe`，且不会执行该 fallback 文件。

## 8. Cursor

| 功能 | 状态 | 当前证据或缺口 |
| --- | --- | --- |
| 安装发现、原生选择和启动 | 已实现 | `internal/agentdiscovery`、`agent_tools.go` |
| 全局 MCP | 已实现 | `internal/mcpconfig` |
| 显式选择项目后的项目 MCP | 已实现 | `cursor_project_mcp.go` |
| MCP 恢复点和诊断 | 已实现 | `mcp_tools.go`、恢复点测试 |
| OAuth/Token/JSON/本机账号导入 | 缺失 | 没有 Cursor 账号存储和令牌刷新模块 |
| 套餐和额度 | 缺失 | 没有 Cursor 配额 API 适配器 |
| 切号注入 | 缺失 | 不读取或写入 Cursor 认证存储 |
| 会话/项目恢复 | 缺失 | 没有 Cursor 会话索引或迁移实现 |
| 账号隔离多开 | 缺失 | 只有单应用启动，没有独立 user-data-dir 实例 |
| 退出、重启、批量关闭 | 缺失 | 通用启动器只提供受控启动 |

Windows MCP 原子替换目前没有达到 Claude settings 管理器的 protected DACL 等级。后续需要把 ACL 创建、应用和写后验证抽成共享安全模块，再用于 Cursor/Windsurf MCP 与凭据元数据文件。

## 9. Windsurf

| 功能 | 状态 | 当前证据或缺口 |
| --- | --- | --- |
| 安装发现、原生选择和启动 | 已实现 | `internal/agentdiscovery`、`agent_tools.go` |
| 全局 MCP | 已实现 | `internal/mcpconfig` |
| MCP 恢复点和诊断 | 已实现 | `mcp_tools.go` |
| 项目 MCP | 不适用 | 当前只管理已确认的全局 MCP 契约 |
| OAuth/Token/JSON/本机账号导入 | 缺失 | 没有 Windsurf 账号存储和刷新模块 |
| 套餐、Prompt Credits 和周期 | 缺失 | 没有 Windsurf 配额适配器 |
| 切号注入 | 缺失 | 不读取或写入 Windsurf 认证存储 |
| 会话/项目恢复 | 缺失 | 没有 Windsurf 会话索引或迁移实现 |
| 账号隔离多开 | 缺失 | 只有单应用启动 |
| 退出、重启、批量关闭 | 缺失 | 通用启动器只提供受控启动 |

## 10. 全局 2FA、更新与诊断

| 功能 | 状态 | 证据 |
| --- | --- | --- |
| `otpauth://totp/` 导入 | 已实现 | `internal/totp` |
| Base32 手动导入 | 已实现 | `internal/totp` |
| 动态验证码生成和 OAuth 快选 | 已实现 | `TOTPSettingsCard.vue`、`OAuthTOTPQuickPicker.vue` |
| 系统凭据库保存 | 已实现 | macOS Keychain / Windows Credential Manager 适配 |
| 加密导出/导入 | 已实现 | `internal/totp/export.go`、`totp_tools.go` |
| 检查更新、跳过版本、下载安装和重启 | 已实现 | `internal/updater`、全局设置 UI 和安装器测试 |
| 脱敏诊断 ZIP | 已实现 | `internal/diagnostics` |

## 11. 当前结论

v1.6.8 是可安装、可运行、经过双端 CI 的多 Agent 基线，但不是完整 Cockpit 功能等价版。

已完成：

- Antigravity WF 的主要代理、补丁、模型、图片、历史和诊断能力；
- 用户自有 XIASS Codex Helper 的主要配置、历史、备份和 Desktop 生命周期能力；
- Claude Code 的静态网关配置与模型测试；
- Cursor/Windsurf 的安装发现、MCP 和受控启动；
- 全局 2FA、更新、诊断和双端安装器。

主要未完成：

- Claude/Cursor/Windsurf 完整账号、OAuth、Token 刷新、等级和额度；
- Codex/Antigravity 的 Cockpit 式会话管理、唤醒任务和隔离多开；
- Cursor/Windsurf 的切号注入、会话恢复和隔离多开；
- 各平台真实客户端、真实账号和真实上游的完整端到端矩阵；
- 商业代码签名和 macOS 公证。

只有这些缺口逐项实现并获得对应证据后，才能把完整目标标记为完成。
