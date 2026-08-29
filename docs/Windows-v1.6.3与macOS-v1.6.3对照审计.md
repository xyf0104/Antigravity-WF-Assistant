# 历史只读审计：Windows v1.6.3 与 macOS v1.6.3 对照

> 本文是 XIASS Tools 重品牌前的交付档案，只记录当时的文件名、Bundle ID、安装路径和产品名称，不能作为当前发布、安装或品牌文案的依据。

本文记录 Antigravity WF助手 v1.6.3 从 Windows 实测交付源码到 macOS Universal 版本的逐文件对照结果。它用于说明哪些逻辑必须完全一致、哪些实现必须因操作系统和官方安装结构不同而分别实现，以及本次实际执行过的验证。

## 审计基线

- Windows 基线：`Antigravity-WF助手_v1.6.3_Windows测试交付_20260814.zip`
- 基线 SHA-256：`7071b7cf82f8a0d32cced6193c0344fd0bc201926e9a66ca0d219cbb512a6c62`
- 当前 Windows 源码/配置行数：47,591 行。
- 当前 macOS 源码/配置行数：52,172 行。
- 双端同相对路径文件：139 个。
- Windows 平台专属文件：25 个。
- macOS 平台专属文件：37 个。
- 对同路径文件消除 CRLF 和 Go 模块名前缀差异后，96 个文件逐字节一致；其余 43 个文件的差异块已经逐项审查。
- Windows 与 macOS 对外 Wails `App` 方法均为 54 个，方法集合完全一致。
- 双端前端实际调用的原生方法均为 47 个；不存在前端调用但 macOS 后端缺失的方法。

Windows 当前生产逻辑与实测交付源码消除 CRLF 后，仅有 10 个功能文件存在后续明确修改：更新/重新连接界面、供应商模型分组与显示、图片模型供应商选择、模型显示名持久化、产品版本来源收紧及对应测试。代理、账户、OAuth、Claude、OpenAI Chat/Responses、工具调用和补丁主体没有被整体替换。

## 产品版本识别

版本显示和“安装更新后需要重新连接”必须使用 Antigravity 产品版本，不能使用 Electron、Chromium 或 VS Code OSS 内核版本。

| 系统 | 权威来源 | 明确禁止作为产品版本的来源 |
| --- | --- | --- |
| Windows | Antigravity 主程序 EXE 的 PE `ProductVersion` / `FileVersion` 资源 | `resources/app/package.json`、`product.json`、Electron/VS Code 固定产品字段 |
| macOS | `<App>.app/Contents/Info.plist` 的 `CFBundleShortVersionString`；为空时回退 `CFBundleVersion` | App 内的 `package.json`、`product.json`、Electron/VS Code 版本 |

回归夹具故意同时写入：

```text
Info.plist 产品版本 = 2.5.5 / 2.8.1
内部 package.json 版本 = 1.107.0
```

IDE 必须返回 `2.5.5`，Antigravity 2.0 必须返回 `2.8.1`。快速状态、完整刷新、启动按钮、安装状态记录、补丁 revision 检查和重新连接弹窗全部使用同一个产品版本字段。

## 共享功能逐域对照

| 功能域 | Windows v1.6.3 | macOS v1.6.3 | 结果 |
| --- | --- | --- | --- |
| 前端界面、更新弹窗、首次引导、主题 | Vue 页面与状态模块 | 相同页面与状态模块 | 一致 |
| Wails 原生 API | 54 个 `App` 方法 | 同名同集合 54 个方法 | 一致 |
| 模型显示 | `模型名 · 账户池名/供应商名` | 相同规则 | 一致 |
| 真实模型 ID | 显示后缀不进入请求 | 相同 | 一致 |
| 账户池、OAuth、JSON、Token、额度与单账户测试 | Windows 实测逻辑 | 共享 Go/前端逻辑 | 一致 |
| OpenAI Chat/Responses 与兼容网关 | 自动/手动路径、窄范围降级 | 同一转换、重试和错误边界 | 一致 |
| Anthropic Messages | Claude 工具、prefill 修复、媒体 | 同一实现 | 一致 |
| 工具调用和断线策略 | 已可能送达后不重放整轮；安全失败同账户重试 | 同一实现 | 一致 |
| Prompt cache | 不重复堆叠上下文，失败后按请求降级 | 同一实现 | 一致 |
| 图片模型选择 | 当前供应商 `gpt-image-2` 优先，缺失时跨供应商 | 同一选择及独立凭据 | 一致 |
| Gemini 生图 | Gemini 原生模型不被 GPT Image 劫持 | macOS 额外按 trajectory 保存原生图片模型，避免切回 Gemini 后仍走 GPT | 行为一致，macOS 防串线更严格 |
| 图片 Prompt | 排除 MCP rules/systemInstruction | 同一提取规则 | 一致 |
| 图片显示 | Prompt 卡正常显示，正文重复图隐藏或限制 320×320 | 同一 renderer 规则 | 一致 |
| 诊断日志 | 脱敏 ZIP | 同一脱敏字段与文件边界 | 一致 |
| 更新 | GitHub 检查、缓存、跳过、下载、SHA256、启动安装 | 同一逻辑，平台安装器不同 | 一致 |
| 补丁事务 | 当前结构备份、候选校验、原子写入、失败回滚 | 同一事务语义，文件格式不同 | 一致 |
| 历史会话 | 启动时合并恢复 | 同一业务逻辑 | 一致 |
| 退出 | 托盘退出并释放代理 | Dock/菜单栏退出并释放代理 | 平台等价 |

## 平台实现映射

不能把 Windows 的 PE 路径和文件名直接复制到 macOS。以下文件承担相同职责，但使用各平台真实结构：

| Windows | macOS | 对应职责 |
| --- | --- | --- |
| `discovery_windows.go` | `discovery_darwin.go` | 标准路径、历史路径、进程/注册表或 Spotlight 发现、缓存、产品版本 |
| `patcher_windows_core.go` | `patcher_darwin.go` | 连接目标、端点替换、状态与事务 |
| `user_config_windows.go` | `user_config_darwin.go` | 用户级 IDE 连接设置与完整性处理 |
| `agent_ui_archive_windows.go` | `agent_ui_archive_darwin.go` | Antigravity 2.0 内嵌 UI ZIP 识别与等长更新 |
| `agent_image_ui_patch_windows.go` | `agent_image_ui_patch_darwin.go` | 图片标题、Prompt 卡、去重与尺寸规则 |
| `ide_integrity_windows.go` | `ide_integrity_darwin.go` | renderer 与 `product.json` checksum 事务 |
| `launcher_windows.go` | `launcher_darwin.go` | 启动、重启和历史保护 |
| `tray_windows.go` | `tray_darwin.go` | 通知区域或 Dock/顶部菜单栏生命周期 |
| `launch_windows.go` | `launch_darwin.go` | 启动已下载的系统安装器 |

macOS 当前识别的官方关键路径：

```text
Antigravity IDE 2.5.5 ARM64:
Contents/Resources/app/extensions/antigravity/bin/language_server_macos_arm

Antigravity IDE 2.5.5 Intel x64:
Contents/Resources/app/extensions/antigravity/bin/language_server_macos_x64

Antigravity 2.0 2.8.1（ARM64 / Intel x64）:
Contents/Resources/bin/language_server
Contents/Resources/app.asar
```

IDE renderer：

```text
Contents/Resources/app/out/jetskiAgent/main.js
Contents/Resources/app/out/vs/workbench/workbench.desktop.main.js
Contents/Resources/app/product.json
```

## 安装发现与快速状态

macOS 已同步 Windows 的“快速首页、完整后台核验”逻辑：

1. 首页快速状态只检查标准目录、上次成功连接的路径、显式恢复路径和有效缓存。
2. 快速状态不运行 `ps`、不运行 Spotlight、不解析 `app.asar`、不扫描 renderer 内容，也不会提前写入 `Patched=true`。
3. 手动刷新和补丁操作执行完整发现、ASAR、Language Server、renderer、checksum 和用户设置校验。
4. 两分钟缓存只保存已发现目标，不保存补丁成功结论。
5. `Info.plist`、产品版本、主程序、Language Server、ASAR、renderer、`product.json`、安装目录或成功路径记录变化都会立即使缓存失效。
6. 历史安装路径只是候选提示；非标准位置仍必须重新通过 Bundle ID 和目录结构验证，不能绕过安全边界。

## 官方原包只读验证

以下四个官方 DMG 均以只读方式挂载验证，验证过程不修改官方 App：

| 产品 | 架构 | 产品版本 | 结果 |
| --- | --- | --- | --- |
| Antigravity IDE | ARM64 | 2.5.5 | renderer、原生预览、v6 图片规则、checksum、幂等和 Node 语法通过 |
| Antigravity IDE | Intel x64 | 2.5.5 | renderer、原生预览、v6 图片规则、checksum、幂等和 Node 语法通过 |
| Antigravity 2.0 | ARM64 | 2.8.1 | ASAR、唯一 UI ZIP、v4 图片规则、等长 Mach-O、幂等和 Node 语法通过 |
| Antigravity 2.0 | Intel x64 | 2.8.1 | ASAR、唯一 UI ZIP、v4 图片规则、等长 Mach-O、幂等和 Node 语法通过 |

## 自动化验证结果

```text
macOS Go 全量测试       PASS
macOS Go race           PASS
macOS go vet            PASS
macOS 前端              26/26 PASS
macOS Vite build         PASS
Windows 前端            26/26 PASS
Windows Vite build       PASS
Windows amd64 交叉编译   PASS
Windows amd64 go vet     PASS
```

Windows 原生运行测试必须在 Windows runner 或 Windows 实机执行；macOS 不能本地执行 PE 程序。交叉测试已经编译全部 Windows 包、测试文件和平台专属调用，GitHub Actions 负责最终原生 runner 验证。

## 最终 macOS 安装包

```text
Antigravity-WF-Assistant-macOS-universal-v1.6.3-Installer.pkg
```

- App 架构：`x86_64 + arm64`。
- Bundle ID：`com.wufeng.antigravity-wf-assistant`。
- App 版本：`1.6.3`。
- App ad-hoc codesign：通过 `codesign --verify --deep --strict`。
- PKG：未公证、未使用 Apple Developer Installer 证书；这是当前发布策略，不等同于损坏。
- 安装位置：`/Applications/Antigravity WF助手.app`。
- 安装器包含可选的“在桌面创建快捷方式”，默认不覆盖用户已有文件。
