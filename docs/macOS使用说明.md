# Antigravity WF助手 macOS v1.4.19

## 安装

1. 推荐双击 `Antigravity-WF-Assistant-macOS-universal-v1.4.19-Installer.pkg`，按向导安装到 `Applications`；如需桌面图标，勾选“在桌面创建快捷方式”。
2. 首次打开如果 macOS 显示开发者提示，请在 Finder 中右键 App，选择“打开”。

App 为 Universal 版本，同时支持 Apple Silicon 和 Intel Mac。运行时不需要 Python、Node.js 或外置补丁脚本。

## 首次使用

1. 打开 WF助手，本地代理会自动启动，并自动合并旧版 Antigravity 历史会话。
2. 进入“模型”，添加 OpenAI 兼容或 Anthropic 上游模型。“显示名称”留空时，自动使用上游模型名。
3. 回到“总览”，确认已自动识别 Antigravity IDE 或 Antigravity 2.x。
4. 点击“应用全部补丁”。如果 Antigravity 位于受保护的 `/Applications` 目录，macOS 可能要求管理员授权。
5. 使用“Antigravity 快捷启动”按钮启动或重启对应安装。

## 账户池与登录

1. 在“账户池”中可添加 API Key、Bearer / Access Token、x-api-key、Setup Token、Codex PAT 或自定义认证头；默认只需填写基础域名，选择“完整路径（手动）”后可完全自行指定请求地址。
2. 使用 ChatGPT / Codex 时，在“OAuth 授权登录”中选择“OpenAI / Codex”，点击“浏览器登录”。助手会显示并自动打开完整授权链接；在浏览器选择 ChatGPT 账户后会自动回到助手，账户卡可显示 OAuth 类型、邮箱、套餐、令牌到期和本机用量。自动回调同时保留手动兜底：复制浏览器跳转后的完整回调 URL，或只复制 `code` 值粘贴即可自动识别并完成；本机回调端口被占用时也会自动切换到此方式。
3. 已有 `auth.json`、OAuth JSON 或 XIASS 风格账户导出时，使用“导入账户 JSON”；Refresh Token / Mobile RT 使用专门的兑换入口，不能当作 API Key 直接保存。
4. 保存或登录成功后，直接在该账户卡片点击“同步全部模型”。它会读取当前账户的真实模型列表并添加到 Antigravity；与已有模型的协议、上游地址和模型名都一致时，会追加该账户到同一个账户池，不会覆盖其他账户或模型设置。
5. 每个账户卡的“测试连接”只测试该账户；运行时同一个模型会按优先级、并发与健康状态从已绑定账户中调度，遇到可重试的故障时尝试其他可用账户。

## 模型与图片

- 相同协议、地址与凭据绑定的上游模型会在“模型”页合并为一张卡片，可勾选每个模型是否注入 Antigravity。
- 自定义上游可在添加窗口直接打开完整测试流程，测试文字或图片模型，不会显示 API Key、认证头或原始响应。
- 当 Antigravity 请求生成图片时，已启用的同上游图片模型会使用当前文字模型的账户池和凭据执行 `/v1/images/generations`，不会改用 Gemini。

## 启动与重启保护

- 安装一个 Antigravity 时显示一个按钮，同时安装 IDE 和 2.x 时显示两个。
- 未运行显示“启动”，已运行显示“重启”。
- 重启前后都会同步历史会话。
- 只请求 Antigravity 正常退出，绝不强制结束进程。如 30 秒内未正常退出，助手会停止重启，保留当前现场。

## 界面和数据

- 右上角可选“浅色”、“深色”或“跟随系统”。
- 模型、凭据、统计和备份继续保存在 `~/.antigravity-byok/`，这个旧目录名为兼容现有数据而保留。
- 历史会话使用 `~/.gemini/antigravity/`；合并时只补充缺失文件，不覆盖现有会话。
- 点击窗口左上角关闭按钮时，主窗口会最小化到 Dock；代理与历史同步继续运行，顶部菜单栏的 WF 图标也会保留。点击顶部图标可打开“打开主界面 / 退出 Antigravity WF助手”菜单；Dock 右键或顶部菜单栏选择“退出”都会先释放 `127.0.0.1:50999`。

## 提醒

请在使用自定义模型时保持 WF助手运行。重装或升级 Antigravity 后，需要重新应用补丁。
