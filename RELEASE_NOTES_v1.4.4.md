# Antigravity WF助手 v1.4.4

## 账户测试与 OAuth

- 每个账户卡片现在都可独立测试，不再进入账户选择器。API Key、OAuth、账户 JSON 导入和 Claude 账户均可在各自账户内选择模型并运行测试。
- 关闭或取消测试时会实际中止对应测试请求，避免测试窗口关闭后仍继续等待或更新界面。
- 改善 OpenAI / Codex OAuth 直连账户的模型获取、可显示的账户额度信息和请求保护；OAuth 凭据不会被误用于不适用的 Chat Completions 请求路径。

## 使用体验

- 更新检查加入短期成功缓存和明确超时处理，重复检查通常会更快返回；网络异常时界面不会持续停留在检查状态。
- OpenAI / Codex 请求在账户或运行时状态发生变化时会进行额外保护，降低错误路由与不兼容请求的风险。

## 安装包

- macOS：`Antigravity-WF-Assistant-macOS-universal-v1.4.4-Installer.pkg`
- Windows：`Antigravity-WF-Assistant-Windows-x64-v1.4.4-Setup.exe`

发布前仍需完成本地构建、GitHub Actions 工作流与真实设备验收；详见 [发布待验收清单](docs/发布核验.md)。
