<p align="center"><img src="assets/logo.png" alt="Antigravity WF助手 Logo" width="160"></p>

# Antigravity WF助手

面向 Windows 与 macOS 的 Antigravity 本地模型管理、连接与启动工具。它可以自动发现已安装的 Antigravity IDE / Antigravity 2.x，通过本机代理接入自定义模型，并集中管理账户、模型、测试、恢复和启动操作。

[![Release](https://img.shields.io/github/v/release/xyf0104/Antigravity-WF-Assistant?label=release)](https://github.com/xyf0104/Antigravity-WF-Assistant/releases/latest)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
![macOS Universal](https://img.shields.io/badge/macOS-Universal-5b5b5b)
![Windows x64](https://img.shields.io/badge/Windows-x64-0078d4)

## 主要功能

- 自动检测 Antigravity、Antigravity IDE 和 Antigravity 2.x；多个安装可以分别连接、启动或重启。
- 支持 OpenAI Chat Completions、OpenAI Responses、OpenAI 兼容接口和 Anthropic Messages API。
- 支持 API Key、Bearer / Access Token、x-api-key、Setup Token、Codex PAT、自定义认证头、账户 JSON、Refresh Token，以及 OpenAI / Codex 浏览器 OAuth 登录。
- 从账户或 API 获取真实可用模型，一键同步到 Antigravity；同一模型可绑定多个账户并按优先级、并发和健康状态调度。
- 模型支持自定义显示名、完整 API 路径、推理等级、连接测试与可用性测试。
- 支持文字、截图、图片识别、文件、PDF、工具调用、推理、联网搜索和上游支持的图片生成能力；当前聊天供应商没有图片模型时，可自动使用另一个已启用供应商的 `gpt-image-2`，不改变当前聊天模型。
- 已识别的图片界面可显示实际图片模型名、默认展开生成结果、保留 Prompt 缩略图，并隐藏同一结果的正文重复大图。
- 提供“全部连接”“仅连接 IDE”“仅连接 Antigravity 2.0”和“恢复原机配置”；写入前创建备份，失败时自动回滚。
- 启动时安全合并历史会话；支持浅色、深色和跟随系统主题。
- 设置页可一键导出脱敏诊断 ZIP，自动收集 WF助手和 Antigravity 最近一次运行日志，便于定位客户电脑上的补丁、模型注入、对话和生图故障。
- Windows 关闭主窗口后驻留通知区域；macOS 关闭主窗口后保留 Dock 与顶部菜单栏入口。选择“退出”会关闭本地代理并释放端口。

## 下载与安装

请从 [最新发布页](https://github.com/xyf0104/Antigravity-WF-Assistant/releases/latest) 下载 Windows v1.6.2 安装包。本次只更新 Windows；macOS 继续使用 v1.5.9。Release 提供标准安装包和客户端安全更新所需的 `SHA256SUMS.txt`，不额外提供 portable、独立 EXE 或 ZIP：

| 系统 | 安装包 | 支持范围 |
| --- | --- | --- |
| macOS | `Antigravity-WF-Assistant-macOS-universal-v1.5.9-Installer.pkg` | Apple Silicon 与 Intel Mac（本次未修改） |
| Windows | `Antigravity-WF-Assistant-Windows-x64-v1.6.2-Setup.exe` | Windows 10/11 x64 |

手动下载时可使用同一 Release 中的 `SHA256SUMS.txt` 校验文件完整性；软件内更新会自动完成该校验。

安装包已包含运行所需组件，不需要另外安装 Python、Node.js 或外置补丁脚本。

Windows 安装时可以勾选创建桌面快捷方式。macOS 安装完成后可从“应用程序”或 Launchpad 打开；若系统拦截未签名应用，请在 Finder 中按住 Control 点按安装包或 App 后选择“打开”，并在“系统设置 → 隐私与安全性”中确认。

## 快速使用

1. 安装并打开 Antigravity WF助手，本地代理会自动启动。
2. 在“账户池”添加 API Key、Token、账户 JSON 或 OAuth 账户。
3. 保存后在账户卡片点击“同步全部模型”；也可以在“模型”页手动添加上游并测试模型。
4. 回到“总览”，确认助手已经检测到 Antigravity IDE 或 Antigravity 2.x。
5. 完全退出正在运行的 Antigravity，然后选择“全部连接”“仅连接 IDE”或“仅连接 Antigravity 2.0”。
6. 连接完成后使用首页启动按钮打开 Antigravity，并在使用自定义模型期间保持 WF助手运行。

更新或重新安装 Antigravity 后，通常需要重新执行连接。若要撤销助手写入的连接配置，请先完全退出 Antigravity，再点击“恢复原机配置”。该操作不会删除账户、模型或聊天记录。

## 账户与模型

- 每个账户卡片都可以单独测试、查看可解析的账号信息和本机转发用量，并直接同步该账户的全部模型。
- 同协议、同上游地址、同模型名的多个账户会绑定到同一个模型，不会重复创建大量模型卡片。
- 每个新请求按优先级和并发选择一次账户；运行中遇到可安全重试的瞬时网络或线路故障时，只重试本次选中的账户，不切换账户、不建立跨请求冷却，也不会重放已经可能送达或已开始输出的完整对话。
- API 地址可以只填写基础域名并由助手补全，也可以切换到完整路径手动编辑。
- 模型能力以实际上游响应为准；上游不支持的 Responses 工具会安全降级，不会伪造模型能力。

## 自动检测与数据

macOS 会检查 `/Applications`、`/System/Applications`、`~/Applications`、运行中的应用和 Spotlight。Windows 会检查常见安装目录、运行进程、卸载注册表及各磁盘常用目录。

如需指定安装位置，可设置：

- `ANTIGRAVITY_APP_PATH`：单个安装路径；
- `ANTIGRAVITY_APP_PATHS`：多个路径，macOS 使用冒号分隔，Windows 使用分号分隔。

本地代理只监听本机回环地址，不向局域网或互联网开放。账户凭据、模型配置、统计与备份只保存在本机。请勿上传 API Key、Token、Cookie、账户 JSON、模型配置或未经脱敏的运行日志。

遇到客户电脑故障时，请打开“设置 → 诊断与日志 → 导出诊断日志”，将生成的 ZIP 发给技术支持。导出过程会二次隐藏 API Key、Authorization、Token、OAuth 授权码、Cookie、用户目录和图片 Base64，并且不会打包账户、模型或 OAuth 配置文件。

## 兼容说明

助手按安装结构而不是仅按版本号判断兼容性。已识别的连接链、renderer 和完整性字段会进入可恢复事务；图片 renderer 未识别时会保持原文件不变并跳过可选界面增强，不会阻断已验证的用户级代理设置。必要的连接链本身无法识别时仍保持零写入；助手不会全局替换未知 JavaScript，也不会把其他版本文件强行复制到当前安装。

更多说明：

- [macOS 安装与使用说明](docs/macOS使用说明.md)
- [Windows 安装与使用说明](docs/Windows使用说明.md)

## 许可证与声明

代码按 [MIT License](LICENSE) 发布。Antigravity 名称及相关商标归各自权利人所有；本项目为本地兼容与开发辅助工具，不代表 Antigravity 官方授权或背书。使用第三方模型服务时，请遵守对应服务条款。
