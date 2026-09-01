# XIASS Tools macOS 源码

这是 XIASS Tools macOS Universal 源码，支持 macOS 12 或更高版本的 Apple Silicon 与 Intel Mac。XIASS Tools 是本机 Agent 配置与连接工具；Antigravity 集成为非官方兼容实现，与 Antigravity 官方无隶属关系。

目前已提供 Antigravity 模型、补丁、代理、会话恢复、诊断和备份能力；并提供 Codex 本机 `config.toml` 的安全配置、上游模型发现、静态 OpenAI Responses 账户复用、原子备份/恢复和兼容历史修复。Claude Code 仅管理明确的用户 `settings.json` 字段，并可复用静态 Anthropic Messages 账户；Cursor 在本机确认客户端且 MCP 结构安全时，可管理全局条目，或在用户通过原生选择器明确选定项目后管理该项目的 `.cursor/mcp.json` 中 XIASS Tools 自己的条目；Windsurf 仅管理公开全局 MCP JSON 中 XIASS Tools 自己的条目。界面不会将未接入、未验证或受保护的配置能力伪装成可用。

## 构建

需要 Go 1.25+、Node.js 22+、npm 和 Xcode Command Line Tools。

```bash
cd frontend
npm ci
npm run build
cd ..
go test ./...
go run github.com/wailsapp/wails/v2/cmd/wails@v2.13.0 build \
  -platform darwin/universal -trimpath -o "XIASS Tools"
```

构建结果位于 `build/bin/XIASS Tools.app`。运行数据保存在 `~/.xiass-tools/`；升级时应用会自动从历史目录安全合并模型、凭据、日志和补丁备份，不删除旧数据。

完整安装和使用说明见发布仓库根目录的 `README.md` 与 `docs/macOS使用说明.md`。
