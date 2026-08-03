# Antigravity WF助手 Windows 源码

这是由 WF 开发与维护的 Windows x64 源码。项目基于 Antigravity BYOK 开源代码继续开发，属于非官方二次开发版本，与 Antigravity 官方无隶属关系。

主要能力包括自定义 OpenAI 兼容/Anthropic 模型、完整推理等级、工具 Schema 规范化、Antigravity IDE/2.x 智能路径检测与安全启动/重启、历史会话恢复、终端命令自动批准、补丁备份和回滚，以及浅色/深色/跟随系统界面。

## 构建

需要 Go 1.25+、Node.js 22+、npm、Wails v2.13.0；生成安装器还需要 NSIS。

```powershell
cd frontend
npm ci
npm run build
cd ..
go test ./...
go run github.com/wailsapp/wails/v2/cmd/wails@v2.13.0 build `
  -platform windows/amd64 -trimpath -o "Antigravity WF助手-v1.4.5.exe"
cd build\windows
makensis installer.nsi
```

主程序和安装器位于 `build\bin\`。运行数据继续保存在 `%USERPROFILE%\.antigravity-byok\`，旧目录名是为了无损兼容已有模型、凭据和补丁备份，请勿删除或改名。

完整安装和使用说明见发布仓库根目录的 `README.md` 与 `docs/Windows使用说明.md`。
