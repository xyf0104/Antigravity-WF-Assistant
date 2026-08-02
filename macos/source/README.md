# Antigravity WF助手 macOS 源码

这是由 WF 开发与维护的 macOS Universal 源码，支持 Apple Silicon 与 Intel Mac。项目基于 Antigravity BYOK 开源代码继续开发，属于非官方二次开发版本，与 Antigravity 官方无隶属关系。

主要能力包括自定义 OpenAI 兼容/Anthropic 模型、完整推理等级、工具 Schema 规范化、Antigravity IDE/2.x 自动检测与安全启动/重启、历史会话自动恢复、终端命令自动批准、补丁备份和回滚，以及浅色/深色/跟随系统界面。

## 构建

需要 Go 1.25+、Node.js 22+、npm 和 Xcode Command Line Tools。

```bash
cd frontend
npm ci
npm run build
cd ..
go test ./...
go run github.com/wailsapp/wails/v2/cmd/wails@v2.13.0 build \
  -platform darwin/universal -trimpath -o "Antigravity WF助手"
```

构建结果位于 `build/bin/Antigravity WF助手.app`。运行数据继续保存在 `~/.antigravity-byok/`，旧目录名是为了无损兼容已有模型、凭据和补丁备份，请勿删除或改名。

完整安装和使用说明见发布仓库根目录的 `README.md` 与 `docs/macOS使用说明.md`。
