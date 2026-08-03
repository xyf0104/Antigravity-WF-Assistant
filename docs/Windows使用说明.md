# Antigravity WF助手 Windows x64 v1.4.4

## 安装

1. 双击 `Antigravity-WF-Assistant-Windows-x64-v1.4.4-Setup.exe`。
2. 在“选择组件”页按需要勾选“在桌面创建快捷方式”，然后完成安装并从开始菜单或桌面打开“Antigravity WF助手”。
3. 安装器默认安装到当前用户的 `%LOCALAPPDATA%\Programs\Antigravity WF助手`，安装助手本身不需要管理员权限。

该安装包已包含完整 x64 主程序，运行时不需要 Python、Node.js 或外置补丁脚本。支持 Windows 10/11 x64。

## 首次使用

1. 打开 WF助手，本地代理会自动启动，并自动合并旧版 Antigravity 历史会话。
2. 进入“模型”，添加 OpenAI 兼容或 Anthropic 上游模型。“显示名称”留空时，自动使用上游模型名。
3. 回到“总览”，确认已自动识别 Antigravity IDE 或 Antigravity 2.x。
4. 点击“应用全部补丁”。如果 Antigravity 安装在 `Program Files` 等受保护目录，请右键 WF助手，选择“以管理员身份运行”后再补丁。
5. 使用“Antigravity 快捷启动”按钮启动或重启对应安装。

## 账户池与登录

1. 在“账户池”中可添加 API Key、Bearer / Access Token、x-api-key、Setup Token、Codex PAT 或自定义认证头；默认只需填写基础域名，选择“完整路径（手动）”后可完全自行指定请求地址。
2. 使用 ChatGPT / Codex 时，在“OAuth 授权登录”中选择“OpenAI / Codex”，点击“浏览器登录”。助手会显示并自动打开完整授权链接；在浏览器选择 ChatGPT 账户后会自动回到助手，账户卡可显示 OAuth 类型、邮箱、套餐、令牌到期和本机用量。自动回调同时保留手动兜底：复制浏览器跳转后的完整回调 URL，或只复制 `code` 值粘贴即可自动识别并完成；本机回调端口被占用时也会自动切换到此方式。
3. 已有 `auth.json`、OAuth JSON 或 XIASS 风格账户导出时，使用“导入账户 JSON”；Refresh Token / Mobile RT 使用专门的兑换入口，不能当作 API Key 直接保存。
4. 一个模型可绑定多个同协议账户。正常请求会按优先级、并发与健康状态调度；可在账户卡片查看本机转发用量、上游限流快照及可显示的账号身份/套餐资料。

## 自动检测

助手会检查 LocalAppData、Program Files、当前运行进程、卸载注册表以及 C: 到 Z: 各盘常见便携目录。支持多个 Antigravity 安装并存。

## 启动与重启保护

- 安装一个 Antigravity 时显示一个按钮，安装两个时显示两个。
- 未运行显示“启动”，已运行显示“重启”。
- 重启前后都会同步历史会话。
- 只向 Antigravity 窗口发送标准关闭请求，绝不使用强制结束。如 30 秒内未正常退出，助手会停止重启。

## 界面和数据

- 右上角可选“浅色”、“深色”或“跟随系统”。
- 模型、凭据、统计和备份保存在 `%USERPROFILE%\.antigravity-byok\`，旧目录名为兼容现有数据而保留。
- 可在 Windows “设置 → 应用”或开始菜单的卸载入口删除助手。
- 点击窗口右上角关闭按钮时，主窗口会隐藏，不会继续占用任务栏；代理与历史同步继续运行。右下角时间旁的 WF 通知区域图标可“打开主界面”或“退出 Antigravity WF助手”；退出会先释放 `127.0.0.1:50999`。若 Windows 将图标收进 `^` 溢出区，可在系统的任务栏设置中将其设为始终显示。

## 提醒

当前 GitHub 发布包没有商业代码签名证书，Windows SmartScreen 可能显示“未知发布者”。请核对 `SHA256SUMS.txt` 后再安装。重装或升级 Antigravity 后，需要重新应用补丁。
