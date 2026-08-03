# Antigravity WF助手 v1.4.1

## 模型与原生能力

- 图片、文本文件、PDF、工具调用和推理按实际协议转换：OpenAI Chat / Responses 与 Claude Messages 各自使用对应的原生请求格式；嵌入、语音、视频等非聊天或未实现的媒体能力不会声明。
- 联网搜索与图片生成仅对 OpenAI 的 `auto` / `Responses` 路径声明；Claude Messages、Chat-only 和通用兼容接口不会被错误标记为支持 OpenAI 专属工具。
- 新增模型不再需要逐项勾选能力；测试成功会在按钮旁显示绿色 `模型可用（HTTP 200）`。
- 自动模式只在本次请求明确使用联网、生图或富媒体能力时选择 Responses API，普通聊天继续使用 Chat Completions，避免无谓的工具调用和额度消耗。
- 上游明确拒绝 `web_search` 或 `image_generation` 时，会在尚未开始生成前仅移除被拒绝的工具并安全重试一次；后续请求会记住该兼容性结果。
- 模型可绑定同协议的多个账户：发现和测试使用首个账户，保存后会保留整个账户池用于优先级、并发和健康状态调度。

## 稳定性

- 阻止并发的相同生成请求，防止重复回复与重复扣费。
- 已有任何文本、工具或附件输出的流不再自动重放；连接中断会以正常终止事件收束现有内容。
- Responses 流仅返回 `[DONE]` 时会补发终止事件，避免 Antigravity 一直等待。
- macOS 与 Windows 的模型注入均覆盖包装响应、压缩响应、选择器索引和上游异常透传。
- macOS 关闭主窗口会最小化到 Dock，并保留顶部菜单栏图标；Dock 或菜单栏的“退出”都会释放本地代理端口。

## 安装包

- macOS: `Antigravity-WF-Assistant-macOS-universal-v1.4.1-Installer.pkg`
- Windows: `Antigravity-WF-Assistant-Windows-x64-v1.4.1-Setup.exe`

发布资产仅包含两个标准安装包和 `SHA256SUMS.txt`。
