# XIASS Tools Windows 源码

这是 XIASS Tools Windows x64 源码。XIASS Tools 是本机 Agent 配置与连接工具；Antigravity 集成为非官方兼容实现，与 Antigravity 官方无隶属关系。

目前已提供 Antigravity 模型、补丁、代理、会话恢复、诊断和备份能力；并提供 Codex 本机 config.toml 的安全配置、上游模型发现、静态 OpenAI Responses 账户复用、原子备份/恢复和兼容历史修复。Claude Code 仅管理明确的用户 settings.json 字段，并可复用静态 Anthropic Messages 账户；Cursor 在本机确认客户端且 MCP 结构安全时，可管理全局条目，或在用户通过原生选择器明确选定项目后管理该项目的 `.cursor/mcp.json` 中 XIASS Tools 自己的条目；Windsurf 仅管理公开全局 MCP JSON 中 XIASS Tools 自己的条目。界面不会将未接入、未验证或受保护的配置能力伪装成可用。

## 构建

需要 Go 1.25+、Node.js 22+、npm、Wails v2.13.0；生成安装器还需要 NSIS。

正式 Setup 会在复制 XIASS Tools 主程序前按 Microsoft 官方 `pv` 注册表契约检测 Evergreen WebView2 Runtime。缺失时通过官方 fwlink 下载架构自适应 Bootstrapper，验证 Microsoft Authenticode 签名，执行 `/silent /install`，并在继续安装前重新读取 HKLM/HKCU Runtime 版本。不会捆绑 Fixed Version Runtime。

```powershell
cd frontend
npm ci
npm run build
cd ..
$version = (Get-Content VERSION -Raw).Trim()
go test ./...
go run github.com/wailsapp/wails/v2/cmd/wails@v2.13.0 build `
  -platform windows/amd64 -trimpath -o "XIASS Tools-v$version.exe"
cd build\windows
& "${env:ProgramFiles(x86)}\NSIS\makensis.exe" /INPUTCHARSET UTF8 "/DAPP_VERSION=$version" installer.nsi
```

主程序和安装器位于 `build\bin\`。运行数据默认保存在 `%USERPROFILE%\.xiass-tools\`；升级时会从历史目录安全迁移已有模型、凭据和补丁备份，不删除或改名旧目录。

完整安装和使用说明见发布仓库根目录的 `README.md` 与 `docs/Windows使用说明.md`。
