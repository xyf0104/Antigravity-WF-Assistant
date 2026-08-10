# Antigravity WF助手 Windows v1.5.1 修复、兼容与交付报告

## 1. 交付范围

- 起始正式基线：`v1.4.20`，提交 `a636b253fb40351c503b2cd781f87384e86c0d4e`。
- 本次 Windows 版本：`v1.5.1`。
- 本次只发布 Windows x64；macOS 版本、安装包和发布资产不升级。
- 不读取、不打包、不提交 API Key、GitHub 凭据、OAuth 凭据、Cookie、账户 JSON、`custom_models.json`、聊天记录或用户日志。
- 不删除用户配置；所有安装目录写入继续采用候选文件、校验、原子替换、备份和失败回滚。

## 2. v1.4.20 之后实机暴露的问题

### 2.1 IDE 图片结果行为

`v1.4.20` 已修复 Windows `file:///C:/...` 在中文用户目录下的 URI 转换，但后续实机版本还暴露了以下差异：

1. 生成结果卡片默认折叠，需要手动展开。
2. 自定义 GPT 图片模型生成时，标题仍可能写死为 Gemini。
3. Prompt 卡片已显示缩略图时，聊天正文还会重复显示一张大图。
4. 部分新 IDE 的 workbench CSP 不允许直接加载原始本地 `file:` URI，需要转换为其允许的 `vscode-file:` 资源。
5. IDE 修改 renderer 后，`product.json` 中官方 checksum 未同步，左下角会出现“安装似乎已损坏”。
6. 旧实现直接改安装目录中的代理启动参数，跨版本脆弱，也可能触发完整性提示。

### 2.2 实际图片模型路由

模型标题并不只是文案问题。Antigravity 会先发起普通模型回合，再发起内部图片子请求。旧逻辑会让同一会话此前记住的自定义图片模型继续影响后续原生 Gemini 回合，或者在内部子请求到达时丢失当前自定义图片模型来源，导致：

- 选择 GPT Image 2 后，内部图片子请求可能没有继续路由到 GPT 图片上游；
- 切回原生 Gemini 后，又可能错误继承此前的 GPT 图片路由。

修复后仅在同一 trajectory 的原生图片子请求中继承最近一次兼容的自定义图片模型；普通原生模型回合会清除旧来源。没有伪造 `generatedMedia`、`generatedImage` 或其他 Antigravity 上游字段。

### 2.3 Antigravity 2.0 独立应用

Antigravity 2.0 的可见聊天 UI 不在 IDE renderer，也不稳定地存在于 `app.asar`；它位于 `resources/bin/language_server.exe` 内嵌的 ZIP 资源中。已验证版本存在三类布局：

| 布局 | 代表版本 | 内嵌样式表 |
|---|---|---|
| 旧布局 | 2.0.6、2.0.10 | `jetbox.css` |
| 新布局 | 2.3.1、2.6.0 | `compiled_tailwind.css` |
| 混合布局 | 2.5.0 | 两者同时存在 |

此前只识别其中一种布局时，会出现“不能注入”或图片实际由 GPT Image 2 生成、标题仍显示 Gemini。现在按真实 ZIP 结构识别，不按版本号硬编码；三类已验证布局都会进入同一安全补丁链。未知布局、重复关键 ZIP 项或未知样式表会拒绝写入。

## 3. 最终实现效果

### 3.1 图片 UI

- 生成期间在尚未收到真实模型名时显示中性标题“Generating image”，不提前误报 Gemini。
- 实际模型为 `gpt-image-2` 时显示 `GPT Image 2`。
- 实际模型为 Gemini 图片模型时保留 Gemini 的模型显示和香蕉图标。
- 其他真实模型名按实际值显示，不硬编码为 GPT 或 Gemini。
- 生成结果卡片默认展开，生成完成后无需手动点开。
- 保留 Prompt 卡片自身的图片预览。
- 对同一图片 URI 做十分钟、精确 URI 归一化去重，隐藏聊天正文重复的大图；不同图片不受影响。
- 支持 Windows 盘符、中文目录、UNC、本地 `file:`、`vscode-file:`、`generatedMedia`、`generatedImage`、`inlineData`、Base64 和 artifact resolver。

### 3.2 IDE 安全连接

- IDE 1.x/2.x 不再依赖版本号硬编码，而是验证官方 `jetski.cloudCodeUrl` 配置链是否同时存在于主进程和扩展。
- 通过用户级 `settings.json` 的 JSONC 安全编辑连接本地代理；保留注释和其他用户字段。
- 恢复时只移除助手写入且值仍匹配的字段，不删除整个配置文件。
- 对确需修改的已验证 renderer，同步更新 `product.json` 中对应 checksum，解决“安装似乎已损坏”。
- checksum 与官方源文件、当前文件均不匹配时拒绝写入。

### 3.3 Antigravity 2.0 安全连接

- 验证 `app.asar/dist/languageServer.js` 中唯一的 `--cloud_code_endpoint` 链路。
- 验证 Language Server 内嵌 ZIP 必须包含唯一的 `index.html`、`main.js`，以及已知样式表组合。
- 仅重建内嵌 ZIP；Language Server 外部 PE 字节保持不变。
- 使用高压缩重建并通过 ZIP comment/entry comment 做等长填充，最终 EXE 总长度不变。
- 重建后重新读取 ZIP、校验两个 UI marker，再写回候选文件。
- 未知结构、压缩后超出预留空间、重复条目、marker 校验失败时不修改正式文件。

### 3.4 助手交互

- 首页提供“全部连接”“仅连接 IDE”“仅连接 Antigravity 2.0”“恢复原机配置”。
- 连接过程显示 0–100% 进度、当前阶段和说明。
- 防止同时启动多个连接任务。
- 成功后显示动态弹窗：
  - IDE 与 2.0 共存且选择全部连接：`确定`、`打开 Antigravity IDE`、`打开 Antigravity 2.0`。
  - 只检测到一个产品：`确定` 和对应的打开按钮。
  - 同类存在多个安装时，按钮附带版本号。
- 托盘左键单击直接显示主界面；右键继续显示原菜单。

## 4. 兼容性结论

### 4.1 已有用户实机验收

- Antigravity IDE 1.23.2：图片自动展开、实际模型名、Prompt 卡片保留、正文重复大图隐藏通过。
- Antigravity IDE 2.0.1：IDE 2.x 兼容链通过。
- Antigravity 2.0 2.0.10：v1.4.28 用户实机通过。
- Antigravity 2.0 2.3.1：v1.4.27 用户实机通过。
- Antigravity 2.0 2.6.0：用户实机通过。

### 4.2 本轮隔离验证

- Antigravity 2.0 2.0.6：等长写回、补丁状态、真实 Language Server 启动和 HTTPS `/main.js` 验证通过。
- Antigravity 2.0 2.5.0：混合样式布局识别、等长写回、真实 Language Server 启动和 HTTPS `/main.js` 验证通过。
- Antigravity 2.0 2.5.0：正式安装写入后，GPT Image 2 实际路由与标题、Prompt 卡片默认展开、正文重复大图隐藏、进度条、动态成功弹窗、打开按钮、托盘左右键均由用户确认通过。

### 4.3 不夸大的支持边界

本版本覆盖上述已验证结构及结构相同的版本，不声明对所有未来未知版本无条件兼容。未来版本结构不匹配时会显示不支持原因并安全跳过，不会全局替换未知 JS。

## 5. 关键证据（脱敏）

### 2.0.6 隔离副本

- 官方 Language Server SHA-256：`B8F2BEB2E09E72715EF7B219AC635915DA920E49E85712CF018485F65B91769C`
- 补丁后 Language Server SHA-256：`9EA666102AE97B0FBC51D4BA774D50E889636738E82152F43F45CA050782C50C`
- HTTPS `/main.js`：7,750,536 字节，包含模型标题与去重两个 marker。
- 补丁后导出 `main.js` 通过 `node --check`。

### 2.5.0 隔离副本

- 官方 Language Server 大小：143,644,160 字节。
- 补丁后 Language Server 大小：143,644,160 字节。
- 官方 SHA-256：`A173330E9CD4933D26DF7158D7F44E2F038FC03B3800AC087BA0EBFD3FB48B54`
- 补丁后 SHA-256：`244A05CA73D5124E17D24199876BB689721801767AA258EC9366CB9903EF5B9B`
- HTTPS `/main.js`：8,789,981 字节，包含两个补丁 marker。
- 真实导出 `main.js` 通过 `node --check`。

以上路径、用户名、会话 ID 和图片内容均未写入报告。

## 6. 修改文件

### Windows 应用、交互与构建

- `windows/source/app.go`
- `windows/source/tray_windows.go`
- `windows/source/tray_windows_test.go`
- `windows/source/frontend/src/state/appState.js`
- `windows/source/frontend/src/views/Dashboard.vue`
- `windows/source/frontend/test/patchProgressUX.test.mjs`
- `windows/source/frontend/package.json`
- `windows/source/frontend/package-lock.json`
- `windows/source/frontend/package.json.md5`
- `windows/source/frontend/src/App.vue`
- `windows/source/VERSION`
- `windows/source/wails.json`
- `windows/source/README.md`
- `windows/source/build/windows/info.json`
- `windows/source/build/windows/installer.nsi`
- `windows/source/go.mod`
- `windows/source/go.sum`
- `.github/workflows/build-windows.yml`

### Windows 补丁与完整性

- `windows/source/internal/patcher/image_preview_patch.go`
- `windows/source/internal/patcher/image_preview_patch_test.go`
- `windows/source/internal/patcher/patcher.go`
- `windows/source/internal/patcher/patcher_windows.go`
- `windows/source/internal/patcher/patcher_windows_core.go`
- `windows/source/internal/patcher/patcher_windows_core_test.go`
- `windows/source/internal/patcher/patcher_windows_image_preview_integration_test.go`
- `windows/source/internal/patcher/agent_image_ui_patch_windows.go`
- `windows/source/internal/patcher/agent_image_ui_patch_windows_test.go`
- `windows/source/internal/patcher/agent_ui_archive_windows.go`
- `windows/source/internal/patcher/agent_ui_archive_windows_test.go`
- `windows/source/internal/patcher/agent_runtime_windows_test.go`
- `windows/source/internal/patcher/official_agent_structure_windows_test.go`
- `windows/source/internal/patcher/ide_integrity_windows.go`
- `windows/source/internal/patcher/ide_integrity_windows_test.go`
- `windows/source/internal/patcher/user_config_windows.go`
- `windows/source/internal/patcher/user_config_windows_test.go`

### 图片路由与更新

- `windows/source/internal/proxy/image_generation_context.go`
- `windows/source/internal/proxy/router.go`
- `windows/source/internal/proxy/antigravity_integration_test.go`
- `windows/source/internal/updater/updater.go`
- `windows/source/internal/updater/updater_test.go`

macOS 源文件不属于本次 Windows v1.5.1 发布内容。

## 7. 测试结果

- 前端 Node 测试：19/19 通过。
- 前端 Vite production build：通过。
- `go test ./... -count=1`：全部包通过。
- `go test -race ./... -count=1`：全部包通过。
- `go vet ./...`：通过。
- Agent 标题/去重 Node 运行时测试：2.0.10 inline loading、2.3.1 旧 artifact renderer、2.6.0 当前 renderer 三组通过。
- 内嵌 ZIP 布局测试：单 `jetbox.css`、单 `compiled_tailwind.css`、双样式通过；未知样式、重复 `main.js`、重复 `index.html`、重复样式表均拒绝。
- 2.0.6/2.5.0 隔离 Language Server 真实 HTTPS 启动：通过。
- Windows Wails x64 build：通过。
- NSIS Setup build：通过。

## 8. 构建产物

- 应用：`Antigravity WF助手-v1.5.1.exe`
- 安装包：`Antigravity-WF-Assistant-Windows-x64-v1.5.1-Setup.exe`
- 安装包 FileVersion / ProductVersion：`1.5.1.0`
- 主程序 SHA-256：`21B5F7524C3669651FC0E09D5DFE059A60BFB7DCC558BAFA076CBE8FACBB9FA2`
- 安装包 SHA-256：`406C086FDF35408384D5058C2260A534B5BED8CB1BA3F546397FF7AC76EDE58D`

## 9. 开发者合并建议

1. 以本报告列出的 Windows 文件为合并范围，不包含本地工具缓存、浏览器测试 profile、用户配置或隔离测试副本。
2. 保留结构验证和安全拒绝，不要改回按版本号硬编码或全局替换压缩 JS。
3. 保留 IDE 用户设置连接与 checksum 同步，不要重新直接改写未知 IDE 主进程。
4. 保留 Agent 内嵌 ZIP 等长写回；PE 外部字节不应变化。
5. 发布前再次在真实 IDE 和 Antigravity 2.0 中验证：实际模型路由、模型标题、默认展开、只保留 Prompt 卡片图片、关闭重开会话、托盘左右键。
6. macOS 若后续需要同等能力，应独立适配和验收，不应直接复用 Windows Language Server 写回方案。
