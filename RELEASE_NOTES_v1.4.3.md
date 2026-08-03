# Antigravity WF助手 v1.4.3

## 重点修复

- Claude 兼容接口现在会在发送前规范化消息历史：保留正常上下文，但移除仅由 IDE 重试流程产生的末尾 assistant 预填回复。严格接口不再因 `last message must be user` 而拒绝请求。
- 应用、安装包、更新器与首页版本统一为 `1.4.3`，修复已安装新版本却显示 `1.4.1` 的问题。
- 按 XIASS 的 OpenAI / Codex OAuth 流程增加一键浏览器登录：完整授权链接可复制且会自动打开；浏览器选定 ChatGPT 账户后自动回调、本机换取和刷新凭据，并展示可解析的账号/套餐/订阅信息。仍保留可完全修改的高级自定义 OAuth、OAuth JSON 和 Refresh Token 导入方式。

## 安装包

- macOS：`Antigravity-WF-Assistant-macOS-universal-v1.4.3-Installer.pkg`
- Windows：`Antigravity-WF-Assistant-Windows-x64-v1.4.3-Setup.exe`

仅发布上述安装包及其 `SHA256SUMS.txt` 校验文件。
