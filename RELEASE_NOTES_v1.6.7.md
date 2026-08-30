# XIASS Tools v1.6.7

v1.6.7 基于已发布的 v1.6.6 稳定版本，继续保留经过验证的 Antigravity 本地代理、连接补丁、模型注入、账户池、图片、重试、历史恢复和诊断链路。本说明描述候选发布的已实现改动；正式 Release 仍以双端原生构建、安装器校验和与 GitHub CI 结果为准。

## 本次改动

- 诊断日志的新名称统一为 `xiass-tools.log`，可受控迁移旧版日志并保留升级前历史；普通日志轮转不会静默覆盖这部分历史。
- 诊断日志初始化、轮转与导出均拒绝符号链接、目录和特殊文件；导出失败会清理未完成的 ZIP，且继续执行二次脱敏。
- Claude Code 只有在用户 `settings.json` 与可恢复备份位置均经过安全验证时才可保存或改写；一次性模型目录获取与 Claude Messages 测试仍只使用当次输入的凭据，不写入设置。
- Cursor / Windsurf 的 MCP 配置现在同时要求全局 JSON 与恢复点状态均已验证。成功提示只说明远程配置已写入，不再将 JSON 写入误表述为远端 MCP 已连接或已测试。
- 工具中心区分“可用本机操作”“手动模型获取”“本地代理健康检查”等状态，避免把配置存在、回环 URL 或模型目录入口混同为真实推理、远端连接或端点健康。
- Windows 的 Wails 绑定继续只在原生 Wails 构建后验证，避免将被 Git 忽略的生成目录误当作干净检出必须携带的源码快照。

## 隐私与兼容性

- 不读取或导入 `auth.json`、Cookie、浏览器数据库、官方会话、OAuth 状态或聊天内容。
- API Key、授权码、回调、Bearer Token、账户 JSON、TOTP 密钥与图片数据不进入诊断、普通日志、前端共享状态或 Release。
- Antigravity、Codex、Claude Code、Cursor 与 Windsurf 都按实际可验证的本机结构处理；未知结构保持零写入，不承诺未来未知版本必定可配置。

## 正式发布物

正式 Release 仅包含：

- macOS Universal PKG；
- Windows x64 Setup EXE；
- `SHA256SUMS.txt`。

不会发布 portable、独立 EXE、ZIP 或测试构建。
