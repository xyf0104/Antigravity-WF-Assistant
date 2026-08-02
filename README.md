<p align="center">
  <img src="assets/logo.png" alt="Antigravity WF助手 Logo" width="220">
</p>

<h1 align="center">Antigravity WF助手</h1>

<p align="center">
  让 Antigravity IDE / Antigravity 2.x 使用自定义 OpenAI 兼容与 Anthropic 模型<br>
  macOS Universal · Windows x64 · 中文桌面界面
</p>

<p align="center">
  <a href="https://github.com/xyf0104/Antigravity-WF-Assistant/releases/latest"><img src="https://img.shields.io/github/v/release/xyf0104/Antigravity-WF-Assistant?label=release" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="MIT License"></a>
  <img src="https://img.shields.io/badge/macOS-Universal-5b5b5b" alt="macOS Universal">
  <img src="https://img.shields.io/badge/Windows-x64-0078d4" alt="Windows x64">
</p>

## 项目说明

`Antigravity WF助手` 由 **WF 开发与维护**，是在 Antigravity BYOK 开源代码基础上持续完善的非官方二次开发版本。它不是 Antigravity 官方产品，也不代表官方授权或背书。

本项目把本地代理、模型管理、安装路径检测、补丁与恢复、历史会话保护、终端权限和启动器集成在一个桌面 App 中。安装后的正常使用不要求 Python、Node.js 或外置补丁脚本。

## 下载

前往 [最新版本下载页](https://github.com/xyf0104/Antigravity-WF-Assistant/releases/latest)。

| 系统 | 推荐文件 | 说明 |
| --- | --- | --- |
| macOS | `Antigravity-WF-Assistant-macOS-universal-v1.3.0.dmg` | 推荐；拖入 Applications 即可，兼容 Apple Silicon 与 Intel |
| macOS | `Antigravity-WF-Assistant-macOS-universal-v1.3.0.zip` | DMG 无法使用时的备用包 |
| Windows 10/11 x64 | `Antigravity-WF-Assistant-Windows-x64-v1.3.0-Setup.exe` | 推荐；当前用户安装，不要求安装器提权 |
| Windows 10/11 x64 | `Antigravity-WF-Assistant-Windows-x64-v1.3.0-Portable.exe` | 免安装便携版 |

下载后可用同一发布页中的 `SHA256SUMS.txt` 核对文件完整性。

## 主要功能

- 自动检测 Antigravity、Antigravity IDE 与 Antigravity 2.x；同时安装两个版本时分别显示两个启动按钮。
- 根据运行状态显示“启动”或“重启”；重启只请求应用正常退出，绝不强制结束进程，前后均同步聊天历史。
- 支持 OpenAI Chat Completions、OpenAI 兼容服务与 Anthropic Messages API。
- OpenAI 推理等级包含：自动、无、最小、低、中、高、超高、最大；实际可用值仍由上游模型决定。
- 显示名称留空时自动使用上游模型名。
- 新增/编辑模型弹窗固定为最高层级，点击外部空白或按 Esc 不会意外消失。
- 自动规范化工具 JSON Schema，处理 `BOOLEAN` 等非标准类型，避免相关 HTTP 400。
- 保留“应用全部补丁”“仅 IDE 补丁”和“恢复原始文件”。
- 启动时安全合并历史会话：只补充缺失文件，不覆盖现有会话。
- 可配置终端命令自动批准范围，并保留原始配置备份。
- 浅色、深色、跟随系统三种主题。
- 补丁前原子备份，失败自动回滚；Antigravity 升级后可重新应用。

## 首次使用

1. 先安装并启动 Antigravity，确认已使用本地凭据正常登录。
2. 安装并打开 `Antigravity WF助手`。
3. 进入“模型”，点击新增模型，填写上游模型名、API 地址、API Key 和协议；“显示名称”可留空。
4. 回到“总览”，确认助手已检测到正确的 Antigravity 安装路径。
5. 通常点击“应用全部补丁”；只想修改 IDE 模型列表时可选“仅 IDE 补丁”。
6. 使用首页的 Antigravity 启动/重启按钮打开对应版本。
7. 使用自定义模型期间保持 WF助手运行。

如果更新或重装了 Antigravity，请重新应用补丁。需要撤销时，点击“恢复原始文件”，然后正常重启 Antigravity。

详细说明：

- [macOS 安装与使用](docs/macOS使用说明.md)
- [Windows 安装与使用](docs/Windows使用说明.md)

## 自动检测范围

macOS 会检查 `/Applications`、`~/Applications`、当前运行中的应用以及 Spotlight 结果。

Windows 会检查 LocalAppData、Program Files、当前运行进程、卸载注册表，以及 C: 到 Z: 各磁盘常见的便携安装目录，包括类似 `D:\Antigravity` 的位置。

如需限定目标，可设置：

- 单路径：`ANTIGRAVITY_APP_PATH`
- 多路径：`ANTIGRAVITY_APP_PATHS`（macOS 用冒号分隔，Windows 用分号分隔）

显式设置路径后，助手只处理指定安装，不会回退修改其他位置。

## 数据、安全与兼容

- 本地代理仅监听 `127.0.0.1:50999`。
- 模型配置、凭据引用、统计和补丁备份仅保存在本机。
- macOS/Windows 继续使用 `.antigravity-byok` 兼容目录和部分旧环境变量/协议头。这是为了无损读取已有模型、凭据与备份，不是遗漏的旧品牌。
- 不要上传自己的 API Key、Cookie、Token、日志、`custom_models.json` 或运行目录。
- “所有终端命令自动批准”属于高风险选项，只应在工作区内容和指令来源完全可信时使用。
- 当前公开构建没有商业代码签名/公证证书，macOS Gatekeeper 或 Windows SmartScreen 可能显示开发者/发布者提示；请从本仓库发布页下载并核对 SHA-256。

## 源码结构

```text
macos/source/    macOS Universal 源码与测试
windows/source/  Windows x64 源码、路径检测与 NSIS 安装器
docs/            安装和使用说明
assets/          WF 品牌资源
```

两个平台均使用 Go、Wails v2.13.0 与 Vue 3。详细构建命令见各平台源码目录中的 `README.md`。GitHub Actions 会分别执行前端构建、Go 测试和平台打包。

## Logo

新版 Logo 保留 Antigravity 拱形识别轮廓与 `WF`，使用珊瑚红、日落橙、紫罗兰到青绿色的自定义渐变，并采用透明背景，以便和官方彩虹标识区分。该位图使用 OpenAI 内置 ImageGen 生成并完成本地透明边缘处理。

## 许可证与声明

代码按 [MIT License](LICENSE) 发布。请保留许可证中的 WF 与上游贡献者版权声明。

Antigravity 名称与相关商标归各自权利人所有。本项目仅用于本地兼容与开发辅助，使用第三方模型服务时请遵守对应服务条款。
