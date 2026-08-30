# XIASS Tools v1.6.6

v1.6.6 是 XIASS Tools 的双平台正式安装包发布准备版本。它保留已经验证的 Antigravity 本地代理、补丁、模型注入、账户池、图片、重试、历史恢复和诊断链路，并继续完善一站式本机 Agent 工具中心。

## 本次完成

- Codex 配置仅在 `xiass_tools` Provider 的完整非敏感语义被验证后才显示为已配置；单纯 TOML 可解析或同名 Provider 不会被误报为成功。
- Codex 旧版第一方 Provider 可在用户明确操作后安全迁移到 `xiass_tools`，保留可迁移的模型、审查模型、上下文和联网搜索设置；未知、歧义或第三方 Provider 不会被自动接管。
- Codex 的配置恢复在 Desktop 正在运行或运行状态无法安全确认时会拒绝直写；不会强制结束 Codex，也不会创建无效恢复点。
- Codex 上游模型发现明确区分“读取模型目录”和“Responses 推理验证”，不会因为 `/v1/models` 返回成功而伪称模型已经可推理，也不会发起隐式 Token 消耗测试。
- Claude Code Gateway 使用实际的 Anthropic Messages 协议校验，而不是只以 HTTP 200 作为连接成功；模型发现与连接测试均使用当前表单的一次性凭据。
- Cursor 与 Windsurf 仅管理 XIASS Tools 自己拥有的 MCP 条目，支持校验恢复点和受限删除；包含敏感字段的既有 MCP 配置保持只读。
- Codex Desktop 发现支持受限的多安装位置检查：非标准已验证实例运行时不会被误用作启动、停止或重启的生命周期目标。
- Windows 的 Claude Code 与 Codex 配置/恢复文件使用当前用户专属受保护 DACL；无法验证时写入失败关闭，不会把凭据文件当作安全保存。

## 隐私与安全边界

- 不读取或导入 `auth.json`、Cookie、浏览器数据库、官方会话、OAuth 状态或聊天内容。
- API Key、授权码、回调、Bearer Token 和 TOTP 密钥不进入诊断、普通日志、前端共享状态或 Release。
- 客户端结构无法验证时显示实际状态并保持零写入；不会承诺未知客户端版本一定可配置。

## 发布物

正式 Release 只包含：

- macOS Universal PKG；
- Windows x64 Setup EXE；
- `SHA256SUMS.txt`。

没有 portable、独立 EXE、ZIP 或测试构建附件。
