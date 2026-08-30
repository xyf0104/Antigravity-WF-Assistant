# XIASS Tools v1.6.7 发布核验记录

- **发布状态：** 已公开发布
- **Tag：** `v1.6.7`（注释标签，解析到提交 `45e457c49400f145dd773ed89a361543ff947ae1`）
- **Release：** [XIASS Tools v1.6.7](https://github.com/xyf0104/Antigravity-WF-Assistant/releases/tag/v1.6.7)
- **发布日期：** 2026-08-30

本记录仅保存可公开核验的发布事实；不含 API Key、Token、账户数据、真实用户路径、日志或会话内容。

## 已完成的构建与安装验证

- [macOS 主线构建](https://github.com/xyf0104/Antigravity-WF-Assistant/actions/runs/33294613578) 已通过：前端、Go race、vet、Universal App、生成绑定、PKG、安装生命周期和上传。
- [Windows 主线构建](https://github.com/xyf0104/Antigravity-WF-Assistant/actions/runs/33294613582) 已通过：前端、Go race、vet、Wails x64、生成绑定、NSIS Setup、静默安装/卸载生命周期和上传。
- [正式发布工作流](https://github.com/xyf0104/Antigravity-WF-Assistant/actions/runs/33294799153) 已通过：以 Tag 源码重新构建双端安装器，验证准确版本与资产集合，发布公开 Release。
- macOS 安装器冒烟测试只在 GitHub 托管的隔离 Runner 执行：它拒绝覆盖预先存在的 App，安装后回读 Bundle ID、版本、主可执行文件和 Universal 架构，最后只移除本次安装的 `XIASS Tools.app`。
- Windows 安装器冒烟测试在 GitHub 托管的隔离 Windows Runner 执行：验证 NSIS 安装、开始菜单、卸载入口、注册表元数据、可选桌面快捷方式默认行为和完整清理。

## 正式资产与校验和

Release 只包含以下三个文件：

```text
XIASS-Tools-macOS-universal-v1.6.7-Installer.pkg
XIASS-Tools-Windows-x64-v1.6.7-Setup.exe
SHA256SUMS.txt
```

| 资产 | SHA-256 |
| --- | --- |
| macOS Universal PKG | `1dfe3ff945a67538b4bce08b63ef6c546743c234dd2b7e06deae346e18e54cc7` |
| Windows x64 Setup EXE | `179874b48721c3569b71ec797f31aa7fa1e3e82508f7980e3ce1fbad51ef9da5` |

公开 Release 回下载后的两项 SHA-256 已分别重新计算，并均与 `SHA256SUMS.txt` 一致。macOS 下载包已展开回读：Bundle ID 为 `com.xiass.tools`，版本为 `1.6.7`，主可执行文件同时包含 `x86_64` 与 `arm64`，并通过本地签名结构验证。

## 真实边界

- macOS PKG 未使用 Apple Developer ID 签名或 Apple 公证；Windows Setup 未使用商业代码签名。Gatekeeper 或 SmartScreen 仍可能显示系统级风险提示，不能宣称无提示安装。
- 自动化验证证明已识别结构上的构建、安装器、配置安全与回滚边界；它不等同于对所有未知 Antigravity、Codex、Claude Code、Cursor 或 Windsurf 版本作无条件兼容承诺。
- 未识别结构必须继续保持零写入，并通过脱敏诊断进入后续适配流程。
