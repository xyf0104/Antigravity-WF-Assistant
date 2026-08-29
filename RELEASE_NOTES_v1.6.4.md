# XIASS Tools v1.6.4

v1.6.4 是 XIASS Tools 的双平台发布准备版本，提供 macOS Universal 与 Windows x64 的标准安装包。它保留 Antigravity 的已验证补丁、本地代理、自定义模型、图片链路、账户池、诊断与恢复能力，并将工具中心扩展为更安全的本机配置入口。

## 本版重点

- **品牌与安装体验**：应用、安装器、桌面快捷方式、托盘/菜单栏、图标、更新器和发布资产统一显示为 XIASS Tools；Release 仅提供 Windows Setup EXE、macOS Universal PKG 与 `SHA256SUMS.txt`。
- **Antigravity**：保留既有的发现、结构验证、补丁备份/恢复、本地代理、模型注入、图片输入输出、图片生成、会话恢复、重试、启动和脱敏诊断流程。
- **Codex**：只管理独立的 `xiass_tools` Provider；支持上游模型发现、原子写入、校验备份/恢复及显式的历史/工作区兼容修复。不会读取 `auth.json` 或声称账号、订阅与额度状态。
- **Claude Code**：只管理用户 `settings.json` 中的 API 根地址、授权令牌和模型；写入采用严格 JSON 校验、原子替换、读回校验与恢复点备份。不会读取登录、OAuth、会话、项目配置、MCP 或凭据文件。
- **Cursor / Windsurf MCP**：仅支持各自公开的全局 MCP JSON 文件，并且只有在客户端已确认、现有配置安全时才允许写入 XIASS Tools 自己的远程条目。含环境变量、请求头、认证或其他敏感字段的现有配置保持只读，不展示也不改写内容。
- **隐私与诊断**：工具中心、原生桥接、诊断导出和错误状态均使用脱敏数据；API Key、OAuth Token、TOTP 密钥、回调内容、账户数据库和聊天记录不会进入全局前端状态或诊断包。

## 升级说明

macOS 从旧版“Antigravity WF助手”升级时，安装器不会静默删除旧 App 或数据。请先退出旧 App，确认 XIASS Tools 能正常启动后，再自行决定是否移除旧 App。

## 下载校验

从项目的 GitHub Release 下载与标签匹配的安装包，并使用同一 Release 的 `SHA256SUMS.txt` 进行校验。不要下载 portable、ZIP 或来源不明的独立二进制。
