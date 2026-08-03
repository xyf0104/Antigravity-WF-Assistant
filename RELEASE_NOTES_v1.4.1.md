# Antigravity WF助手 v1.4.1

## 模型与原生能力

- 自定义聊天模型默认公开图片、文件、工具调用、推理、联网与生图能力；嵌入、语音等非聊天模型保守关闭不适用功能。
- 新增模型不再需要逐项勾选能力；测试成功会在按钮旁显示绿色 `模型可用（HTTP 200）`。
- 自动模式只在本次请求明确使用联网、生图或富媒体能力时选择 Responses API，普通聊天继续使用 Chat Completions，避免无谓的工具调用和额度消耗。
- 上游明确拒绝 `web_search` 或 `image_generation` 时，会在尚未开始生成前仅移除被拒绝的工具并安全重试一次；后续请求会记住该兼容性结果。

## 稳定性

- 阻止并发的相同生成请求，防止重复回复与重复扣费。
- 已有任何文本、工具或附件输出的流不再自动重放；连接中断会以正常终止事件收束现有内容。
- Responses 流仅返回 `[DONE]` 时会补发终止事件，避免 Antigravity 一直等待。
- macOS 与 Windows 的模型注入均覆盖包装响应、压缩响应、选择器索引和上游异常透传。

## 安装包

- macOS: `Antigravity-WF-Assistant-macOS-universal-v1.4.1-Installer.pkg`
- Windows: `Antigravity-WF-Assistant-Windows-x64-v1.4.1-Setup.exe`

发布资产仅包含两个标准安装包和 `SHA256SUMS.txt`。
