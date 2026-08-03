# Antigravity WF助手 macOS v1.3.5

## 安装

1. 推荐双击 `Antigravity-WF-Assistant-macOS-universal-v1.3.5-Installer.pkg`，按向导安装到 `Applications`；如需桌面图标，勾选“在桌面创建快捷方式”。
2. 首次打开如果 macOS 显示开发者提示，请在 Finder 中右键 App，选择“打开”。

App 为 Universal 版本，同时支持 Apple Silicon 和 Intel Mac。运行时不需要 Python、Node.js 或外置补丁脚本。

## 首次使用

1. 打开 WF助手，本地代理会自动启动，并自动合并旧版 Antigravity 历史会话。
2. 进入“模型”，添加 OpenAI 兼容或 Anthropic 上游模型。“显示名称”留空时，自动使用上游模型名。
3. 回到“总览”，确认已自动识别 Antigravity IDE 或 Antigravity 2.x。
4. 点击“应用全部补丁”。如果 Antigravity 位于受保护的 `/Applications` 目录，macOS 可能要求管理员授权。
5. 使用“Antigravity 快捷启动”按钮启动或重启对应安装。

## 启动与重启保护

- 安装一个 Antigravity 时显示一个按钮，同时安装 IDE 和 2.x 时显示两个。
- 未运行显示“启动”，已运行显示“重启”。
- 重启前后都会同步历史会话。
- 只请求 Antigravity 正常退出，绝不强制结束进程。如 30 秒内未正常退出，助手会停止重启，保留当前现场。

## 界面和数据

- 右上角可选“浅色”、“深色”或“跟随系统”。
- 模型、凭据、统计和备份继续保存在 `~/.antigravity-byok/`，这个旧目录名为兼容现有数据而保留。
- 历史会话使用 `~/.gemini/antigravity/`；合并时只补充缺失文件，不覆盖现有会话。
- 点击窗口左上角关闭按钮时，主窗口会隐藏，应用仅保留在顶部菜单栏，不会继续占用 Dock；代理与历史同步继续运行。顶部菜单栏的 WF 图标可“打开主界面”或“退出 Antigravity WF助手”；退出会先释放 `127.0.0.1:50999`。

## 提醒

请在使用自定义模型时保持 WF助手运行。重装或升级 Antigravity 后，需要重新应用补丁。
