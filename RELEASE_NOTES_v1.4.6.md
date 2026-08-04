# Antigravity WF助手 v1.4.6

## 稳定性

- 修复 OpenAI Chat、Responses 和 Claude 工具调用的参数转换。完整参数会被保留，损坏调用会被安全丢弃，不再让 Antigravity 因无效签名重复发送同一轮对话。
- 补充工具调用、最终文本去重、模型启停和请求轨迹的双端回归测试。

## 模型与测试

- 相同协议、上游地址与凭据绑定的模型合并为一个上游卡片，可逐个启用或停用；停用的模型不会注入或路由到 Antigravity。
- 账户池卡片操作支持窄窗口自动换行，不会越过卡片边界。
- 自定义模型的“详细测试”复用账户池完整测试弹窗，可选择模型、测试文字或图片模型、查看安全的测试步骤和图片预览，并支持取消。

## 图片路由

- Antigravity 的原生两阶段图片请求会关联同一会话轨迹，使用同一上游已启用的图片模型（如 `gpt-image-2`）调用 `/v1/images/generations`。
- 图片请求继承当前文字模型的凭据和账户池调度，不会误用其他 API Key，也不会错误回落到 Gemini。

## 安装包

- macOS Universal：`Antigravity-WF-Assistant-macOS-universal-v1.4.6-Installer.pkg`
- Windows x64：`Antigravity-WF-Assistant-Windows-x64-v1.4.6-Setup.exe`

发布页仅提供以上两个标准安装包和由同次构建生成的 `SHA256SUMS.txt`。
