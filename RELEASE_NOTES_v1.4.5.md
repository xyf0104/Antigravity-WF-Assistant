# Antigravity WF助手 v1.4.5

## 账户池快捷使用

- 每个官方 OAuth、API Key、Claude、JSON 导入账户的卡片都提供“同步全部模型”。添加账户后只需点击一次，模型会自动同步到 Antigravity。
- 协议、上游地址和上游模型名相同的账户会自动合并为同一账户池；在 Antigravity 中直接选择该模型即可使用，无需逐个模型手动绑定账户。
- 运行时按账户优先级、并发和健康状态调度；可重试的网络、限流或鉴权失败会尝试其他已绑定账户。
- 原有直连模型在账户池暂时不可用时仍会回退原有凭据，不会因同步账户而失效。

## 可靠性与安全

- 账户同步期间若账户被删除或连接路由改变，结果不会写入过期的模型绑定。
- 上游模型发现、测试和额度查询中的 API Key、认证头及其裸回显会被隐藏。
- OpenAI / Codex OAuth 模型发现兼容官方模型清单，避免仅显示本地回退模型。

## 安装包

- macOS Universal：`Antigravity-WF-Assistant-macOS-universal-v1.4.5-Installer.pkg`
- Windows x64：`Antigravity-WF-Assistant-Windows-x64-v1.4.5-Setup.exe`

发布页仅提供以上两个标准安装包和由同次构建生成的 `SHA256SUMS.txt`。
