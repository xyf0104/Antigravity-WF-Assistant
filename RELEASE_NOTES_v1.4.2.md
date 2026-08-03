# Antigravity WF助手 v1.4.2

## 账户与登录

- 新增通用 OAuth 2.0 Authorization Code + PKCE 登录：填写服务商公开客户端配置后，助手会打开真实授权链接，并校验回调状态后保存账户。
- 支持将 Refresh Token / Mobile RT 通过服务商令牌端点兑换为 OAuth 账户；刷新令牌不会被当作模型 API Key 使用。
- 支持导入常见的账户 JSON、`auth.json` 与 OAuth 凭据 JSON；解析后会识别访问令牌、刷新令牌、到期时间、可显示的账号身份与套餐信息。
- 账户可配置优先级与并发数。OAuth 令牌临近到期时会单飞刷新；刷新或限流失败时，调度器会冷却该账户并尝试同池其他已绑定账户。

## 透明度与稳定性

- 账户页展示本机转发用量、上游响应头中的限流快照，并支持用户显式配置的额度接口刷新；不会因打开页面自动消耗上游额度。
- 认证地址默认只需填写基础域名，助手按 OpenAI / Anthropic 协议补全路径；切换“完整路径（手动）”后会严格保留用户填写的地址。
- 流式请求只会在上游明确拒绝、且尚未返回任何内容时安全重试。出现不确定的网络中断或已开始输出时不会重放原请求，避免重复回答、重复工具调用和无谓扣费。
- JSON / Refresh Token 类型无法通过普通 API Key 保存入口绕过，避免错误落盘或发送到模型接口。

## 安装包

- macOS: `Antigravity-WF-Assistant-macOS-universal-v1.4.2-Installer.pkg`
- Windows: `Antigravity-WF-Assistant-Windows-x64-v1.4.2-Setup.exe`

发布资产仅包含两个标准安装包和 `SHA256SUMS.txt`。
