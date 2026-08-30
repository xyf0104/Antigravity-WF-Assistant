# XIASS Tools macOS

## 安装

1. 推荐双击 `XIASS-Tools-macOS-universal-v<version>-Installer.pkg`，按向导安装到 `Applications`；如需桌面图标，勾选“在桌面创建快捷方式”。
2. 当前发布包未使用 Apple Developer ID 签名或公证。若 macOS 阻止打开安装包，请在 Finder 中按住 Control 点按该 `.pkg` 后选择“打开”，或在“系统设置 → 隐私与安全性”中确认继续；安装完成后首次打开 App 遇到相同提示时也按此方式处理。安装前请先用发布页的 `SHA256SUMS.txt` 核验下载文件。

App 为 Universal 版本，同时支持 Apple Silicon 和 Intel Mac。运行时不需要 Python、Node.js 或外置补丁脚本。

## 首次使用

1. 打开 XIASS Tools，本地代理会自动启动，并自动合并旧版 Antigravity 历史会话。
2. 进入“账户池”，添加 API Key、导入账户 JSON，或完成 OAuth 登录；保存成功后，直接在该账户卡片点击“同步全部模型”。“模型”页用于手动添加或精细调整单个模型。
3. 回到“总览”，确认已自动识别 Antigravity IDE 或 Antigravity 2.x。
4. 完全退出 Antigravity，然后点击“全部连接”；也可只连接 IDE 或 Antigravity 2.0。助手只会处理通过结构验证的安装，未知结构会显示原因并保持原文件不变。
5. 使用“Antigravity 快捷启动”按钮启动或重启对应安装。

## 账户池与登录

1. 在“账户池”中可添加 API Key、Bearer / Access Token、x-api-key、Setup Token、Codex PAT 或自定义认证头；默认只需填写基础域名，选择“完整路径（手动）”后可完全自行指定请求地址。
2. 在“账户池”点击 **OAuth 登录**，再选择需要的平台。**OpenAI / Codex** 与 **Grok** 使用浏览器授权和本机自动回调；助手会显示并自动打开授权链接。自动回调同时保留手动兜底：复制浏览器跳转后的完整回调 URL，或只复制 `code` 值粘贴即可完成；本机回调端口被占用时也会自动切换到此方式。
3. **Claude Code** 的公开流程使用固定回调，因此授权后需粘贴完整回调地址或授权码；**Google / Antigravity（Google OAuth）** 只需填写你自己注册的公开 Desktop Client ID，然后即可打开授权页，不需要也不会保存 Client Secret，也不会读取 Antigravity 的原生登录会话。其他提供方可使用“高级自定义 OAuth”填写自己注册的公开客户端、授权地址、令牌地址和回调地址。
4. 已有自己有权使用的账户凭据 JSON 或 XIASS 风格账户导出时，使用“导入账户 JSON”；导入器只保留账户级 API Key、Access / Refresh Token、ID Token 与公开 OAuth Client ID，客户端密钥、Cookie、浏览器/桌面会话与未知私有字段都会忽略。Refresh Token / Mobile RT 使用专门的兑换入口，不能当作 API Key 直接保存。这里管理的是 XIASS Tools 的上游账户，不会读取、替换或导出任何客户端的原生登录会话。
5. 保存或登录成功后，直接在该账户卡片点击“同步全部模型”。它会读取当前账户的真实模型列表并添加到 Antigravity；与已有模型的协议、上游地址和模型名都一致时，会追加该账户到同一个账户池，不会覆盖其他账户或模型设置。
6. 每个账户卡的“测试连接”只测试该账户；每个新请求会按优先级与并发选择一次账户，同一请求遇到可安全重试的瞬时故障时只重试该账户，不切换账户，也不建立影响下一次请求的失败冷却。

## 模型与图片

- 相同协议、地址与凭据绑定的上游模型会在“模型”页合并为一张卡片，可勾选每个模型是否注入 Antigravity。
- 自定义上游可在添加窗口直接打开完整测试流程，测试文字或图片模型，不会显示 API Key、认证头或原始响应。
- 当自定义聊天模型请求生成图片时，助手优先选择同供应商已启用的 `gpt-image-2` 等图片模型；该供应商没有图片模型时，可选择另一张供应商卡中的图片模型，并始终使用图片供应商自己的地址、凭据和账户池执行 `/v1/images/generations`。
- 使用 Antigravity 原生 Gemini 模型时仍保持 Gemini 原生生图链路；切换回自定义模型后才使用自定义 Images API，两条链路不会互相串用。
- 图片生成结果会在已识别的 Antigravity renderer 中默认展开并显示实际的图片模型标题；同一会话从自定义图片模型切回原生 Gemini 后，后续原生图片请求不会继续复用先前的自定义图片来源。

## 启动与重启保护

- 安装一个 Antigravity 时显示一个按钮，同时安装 IDE 和 2.x 时显示两个。
- 未运行显示“启动”，已运行显示“重启”。
- 重启前后都会同步历史会话。
- 只请求 Antigravity 正常退出，绝不强制结束进程。如 30 秒内未正常退出，助手会停止重启，保留当前现场。

## 界面和数据

- 右上角可选“浅色”、“深色”或“跟随系统”。
- 模型、凭据、统计和备份保存在 `~/.xiass-tools/`。首次启动时会安全迁移 `~/.antigravity-wf/` 与 `~/.antigravity-byok/` 中的兼容数据；这两个旧目录只作为迁移来源，不再作为新的运行数据位置。
- 历史会话使用 `~/.gemini/antigravity/`；合并时只补充缺失文件，不覆盖现有会话。
- 点击窗口左上角关闭按钮时，主窗口会最小化到 Dock；代理与历史同步继续运行，顶部菜单栏的 XIASS Tools 图标也会保留。点击顶部图标可打开“打开主界面 / 退出 XIASS Tools”菜单；Dock 右键或顶部菜单栏选择“退出”都会先释放本地代理。

## 兼容说明

助手会扫描 `/Applications`、`/System/Applications`、`~/Applications`、正在运行的应用和 Spotlight 结果。IDE 优先使用官方用户设置连接链；renderer 图片预览只修改已识别的 JavaScript 结构，并将 renderer、产品 checksum 和用户设置放入同一失败回滚事务。未知或结构变化的安装会被安全跳过，不应手动套用其他版本文件。

## 提醒

请在使用自定义模型时保持 XIASS Tools 运行。重装或升级 Antigravity 后，需要重新执行连接。
