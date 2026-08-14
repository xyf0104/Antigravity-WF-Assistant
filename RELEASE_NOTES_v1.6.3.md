# Antigravity WF助手 v1.6.3

发布日期：2026-08-15

v1.6.3 同时提供 Windows x64 与 macOS Universal 安装包。Windows 以已经实际验收的 v1.6.3 源码为基准；macOS 移植同一套模型管理、图片显示、连接状态与升级逻辑，并保留 macOS 原生 App Bundle、签名和事务处理方式。

## 模型与供应商

- Antigravity 模型下拉框统一显示为“模型名 · 账户池名/供应商名”，例如 `gpt-5.6-sol · 无风`。
- 已绑定账户池时沿用账户池显示名，直接 API 模型显示供应商名；这只决定文字后缀，与生图模型选择优先级无关，也不会写入模型 ID、上游模型名或 API 请求。
- 供应商卡片支持统一编辑名称、协议、地址、认证和账户绑定，卡片内模型仍可分别启用、停用、删除和调整推理等级。
- 推理等级保留上游原始值，并按模型能力过滤可选项。

## 图片生成与显示

- GPT 聊天模型优先使用当前供应商卡片内已启用的 `gpt-image-2`；当前供应商没有图片模型时，再选择其他已启用供应商的图片模型。
- 图片请求始终使用被选中图片模型自己的地址、API Key、自定义请求头或账户池，不会借用聊天模型的凭据。
- Gemini 聊天继续走原生 Gemini 图片链路；GPT 图片链路显示实际使用的 GPT 图片模型。
- Prompt 卡片中的生成图片正常显示；同一结果的正文 artifact 优先隐藏，无法隐藏时最大显示为 320×320；Prompt 卡图片和普通 Markdown 图片不受此尺寸限制。
- IDE 图片规则升级为 v6，Antigravity 2.0 图片规则升级为 v4；旧的已知规则可安全迁移，未知结构保持原文件不变。
- macOS 已加入 Antigravity IDE 2.5.5 原生预览结构和 Antigravity 2.0 2.8.1 renderer 家族的专门回归；Darwin 端继续通过 App Bundle、Mach-O、ASAR/内嵌 ZIP 与完整性校验执行事务式注入。

## 连接、更新与可靠性

- IDE 与 Antigravity 2.0 分别记录安装结构、连接状态和平台独立补丁 revision；只连接其中一个不会误标另一个已完成。
- 产品版本不再使用内置 VS Code/Electron 的 `package.json` / `product.json` 版本。macOS 读取 App Bundle 的 `CFBundleShortVersionString`，缺失时只回退到 `CFBundleVersion`；因此 IDE 2.5.5 与 Antigravity 2.0 2.8.1 不会被错误显示为内部版本 `1.107.0`。
- macOS 同步 Windows 的快速状态与安装发现逻辑：优先复用上次成功连接的路径和两分钟结构缓存，首页不解析大型 ASAR；产品版本、主程序、Language Server、renderer、ASAR、`product.json` 或安装目录变化时缓存立即失效，手动刷新仍执行进程、Spotlight 与完整兼容性检查。
- Antigravity 安装版本、路径、程序文件或连接规则发生变化时提示重新连接；可以暂时关闭提示，关闭不会伪造升级完成。
- 连接过程继续使用可恢复事务：写入前备份当前结构，失败时回滚；已安装旧助手或第三方修改的文件也会先按当前状态备份后再处理。
- 请求已经可能送达上游但连接中断时，不自动重放整轮请求，避免重复扣费和重复工具调用；安全的瞬时连接失败按设置重试。
- 软件内更新同时识别 Windows 与 macOS 的标准安装包，并通过 `SHA256SUMS.txt` 校验完整性。

## 安装包

- `Antigravity-WF-Assistant-Windows-x64-v1.6.3-Setup.exe`
- `Antigravity-WF-Assistant-macOS-universal-v1.6.3-Installer.pkg`
- `SHA256SUMS.txt`

Release 不提供 portable、独立主程序或源码 ZIP 附件。
