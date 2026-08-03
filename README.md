![Antigravity WF助手 Logo](assets/logo.png)

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
- 使用 OpenAI Chat Completions、OpenAI 兼容接口或 Anthropic Messages API；
- 管理 GPT 的推理等级；
- 自动检测并启动 Antigravity IDE / Antigravity 2.x；
- 在更新 Antigravity 后重新应用本地配置；
- 保留已有聊天会话并安全恢复历史记录的用户。

## 主要功能

- 自动检测 Antigravity、Antigravity IDE 和 Antigravity 2.x；若安装了多个版本，首页会分别提供启动或重启按钮。
- 模型配置支持自定义显示名、上游模型名、API 地址、API Key、协议和推理等级；显示名留空时自动使用上游模型名。
- 支持浅色、深色和跟随系统主题。
- 提供“应用全部补丁”“仅 IDE 补丁”“恢复原始文件”三项操作，并在修改前创建本地备份。
- 启动时安全合并历史会话；关闭窗口后继续驻留：Windows 右下角托盘、macOS 顶部菜单栏均可打开主界面或退出；选择退出后释放本地端口。
- 可设置终端命令的自动批准范围；请只在完全可信的工作区和指令来源中启用。

## 下载

请从 [最新发布页](https://github.com/xyf0104/Antigravity-WF-Assistant/releases/latest) 下载对应系统的安装包。

| 系统 | 推荐下载 | 说明 |
| --- | --- | --- |
| macOS | `Antigravity-WF-Assistant-macOS-universal-v1.3.4-Installer.pkg` | 标准安装器，兼容 Apple Silicon 与 Intel |
| Windows 10/11 x64 | `Antigravity-WF-Assistant-Windows-x64-v1.3.4-Setup.exe` | 标准安装器，可选择创建桌面快捷方式 |

发布页仅提供以上两个标准安装包及 `SHA256SUMS.txt`，用于校验文件完整性。

## 使用方法

1. 安装并打开 Antigravity WF助手。
2. 确认首页已检测到 Antigravity 的安装路径；若未检测到，可按系统说明设置路径环境变量。
3. 在“模型”中添加服务的上游模型名、API 地址、API Key 和协议。
4. 根据使用范围选择“应用全部补丁”或“仅 IDE 补丁”。
5. 使用首页的启动按钮打开 Antigravity，并在使用自定义模型期间保持助手运行。

如重新安装或更新 Antigravity，可再次应用补丁。若想撤销本地修改，使用“恢复原始文件”后正常重启 Antigravity。

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
