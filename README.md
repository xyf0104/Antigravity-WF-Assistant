<p align="center"><img src="assets/logo.png" alt="Antigravity WF助手 Logo" width="180"></p>

# Antigravity WF助手

面向 macOS 与 Windows 的 Antigravity 本地模型管理与启动工具。它帮助已安装 Antigravity IDE / Antigravity 2.x 的用户接入自定义模型服务、管理本地配置，并从一个桌面界面完成补丁、恢复与启动操作。

[![Release](https://img.shields.io/github/v/release/xyf0104/Antigravity-WF-Assistant?label=release)](https://github.com/xyf0104/Antigravity-WF-Assistant/releases/latest)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
![macOS Universal](https://img.shields.io/badge/macOS-Universal-5b5b5b)
![Windows x64](https://img.shields.io/badge/Windows-x64-0078d4)

## 工具作用

Antigravity WF助手在本机运行代理服务，使 Antigravity 可以使用符合 OpenAI 或 Anthropic 协议的模型服务。它会管理所需的本地配置和 IDE 补丁，并尽可能自动找到已安装的 Antigravity 应用。

适用于希望：

- 添加、编辑和管理自定义模型；
- 使用 OpenAI Chat Completions / Responses、OpenAI 兼容接口或 Anthropic Messages API；
- 管理 GPT 的推理等级；
- 自动检测并启动 Antigravity IDE / Antigravity 2.x；
- 在更新 Antigravity 后重新应用本地配置；
- 保留已有聊天会话并安全恢复历史记录的用户。

## 主要功能

- 自动检测 Antigravity、Antigravity IDE 和 Antigravity 2.x；若安装了多个版本，首页会分别提供启动或重启按钮。
- 模型配置支持自定义显示名、上游模型名、API 地址、API Key、协议和推理等级；显示名留空时自动使用上游模型名。
- 支持账户池绑定：同协议的多个账户可轮换与故障切换；发现、测试使用首个账户，运行时按优先级、并发和健康状态调度全部已绑定账户。
- 账户池支持 API Key、Bearer / Access Token、x-api-key、Setup Token、Codex PAT、自定义认证头、账户 JSON、Refresh Token 兑换，以及与 XIASS 一致的 OpenAI / Codex 一键 OAuth 登录；授权链接会显示并自动打开浏览器，完成后凭据仅保存在本机、不会回显到界面或日志。
- 账户卡片可展示可解析的账号身份/套餐、本机转发用量，以及上游响应头或用户显式配置额度接口返回的限流快照。
- OpenAI 自动 / Responses 模式可转发截图、文件、联网搜索与图片生成请求；当上游明确不支持某项 Responses 内置工具时，助手会在生成前安全降级并记住兼容性结果。Claude Messages 可转发文字、图片、PDF、工具调用和推理请求。
- 支持浅色、深色和跟随系统主题。
- 提供“应用全部补丁”“仅 IDE 补丁”“恢复原始文件”三项操作，并在修改前创建本地备份。
- 启动时安全合并历史会话；关闭窗口后继续驻留：Windows 右下角托盘、macOS 顶部菜单栏均可打开主界面或退出；macOS 点红色关闭按钮会最小化到 Dock，Dock 或菜单栏的“退出”都会释放本地端口。
- 可设置终端命令的自动批准范围；请只在完全可信的工作区和指令来源中启用。

## 下载

请从 [最新发布页](https://github.com/xyf0104/Antigravity-WF-Assistant/releases/latest) 下载对应系统的安装包。

| 系统 | 推荐下载 | 说明 |
| --- | --- | --- |
| macOS | `Antigravity-WF-Assistant-macOS-universal-v1.4.20-Installer.pkg` | 标准安装器，兼容 Apple Silicon 与 Intel |
| Windows 10/11 x64 | `Antigravity-WF-Assistant-Windows-x64-v1.4.20-Setup.exe` | 标准安装器，可选择创建桌面快捷方式 |

发布页仅提供以上两个标准安装包及 `SHA256SUMS.txt`，用于校验文件完整性。

## 使用方法

1. 安装并打开 Antigravity WF助手。
2. 确认首页已检测到 Antigravity 的安装路径；若未检测到，可按系统说明设置路径环境变量。
3. 在“账户池”添加 API Key 账户、导入账户 JSON，或完成 OAuth 登录。保存成功后，直接在该账户卡片点击“同步全部模型”。
4. 同步会读取该账户真实可用的全部模型并添加到 Antigravity；相同协议、相同上游地址、相同模型名的账户会自动合并为一个账户池，原有模型配置与账户不会被覆盖。
5. 根据使用范围选择“应用全部补丁”或“仅 IDE 补丁”。
6. 使用首页的启动按钮打开 Antigravity，并在使用自定义模型期间保持助手运行。

如重新安装或更新 Antigravity，可再次应用补丁。若想撤销本地修改，使用“恢复原始文件”后正常重启 Antigravity。

### 账户池快速使用

1. 每个官方 OAuth、API Key、Bearer Token、Claude 或 JSON 导入账户都有自己的“同步全部模型”按钮，不需要再进入模型页逐个绑定。
2. 对同一上游再同步一个账户时，同名模型会保留所有已绑定账户。运行时按优先级、并发和健康状态选择账户；遇到可重试的网络、限流或认证故障时会尝试其他可用账户。
3. “测试连接”只测试当前账户；同步失败时账户卡会直接显示错误，不会改动已保存的模型或其他账户。
4. “模型”页仍可用于手动添加模型、指定完整接口路径或精细调整模型设置。

- [macOS 安装与使用说明](docs/macOS使用说明.md)
- [Windows 安装与使用说明](docs/Windows使用说明.md)

## 自动检测与数据

macOS 会检查 `/Applications`、`~/Applications`、当前运行中的应用和 Spotlight 结果。Windows 会检查常见安装目录、运行中的进程、卸载注册表和各磁盘上的便携安装目录。

如需指定安装位置，可设置：

- `ANTIGRAVITY_APP_PATH`：单个路径
- `ANTIGRAVITY_APP_PATHS`：多个路径（macOS 使用冒号分隔，Windows 使用分号分隔）

本地代理仅监听 `127.0.0.1:50999`。模型配置、凭据引用和备份保存在本机；请勿上传 API Key、Cookie、Token、运行日志或模型配置文件。

## 许可证与声明

代码按 [MIT License](LICENSE) 发布。Antigravity 名称及相关商标归各自权利人所有；本项目为本地兼容与开发辅助工具，不代表 Antigravity 官方授权或背书。使用第三方模型服务时，请遵守对应的服务条款。
