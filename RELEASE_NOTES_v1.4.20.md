# Antigravity WF助手 v1.4.20

`v1.4.20` 修复了部分 Windows Antigravity `1.23.2` 客户端中“图片已生成但主聊天区不自动展示”的兼容问题。

## 图片生成与展示

- 自定义图片模型现在会注入与 Antigravity 协议一致的图片输出能力和图片模型索引；普通文字模型不会被错误标记为图片模型。
- 为已验证的 Antigravity `1.23.2` 图片渲染器加入了严格、可恢复的 **v4** 兼容回退：支持 `generatedMedia`、`generatedImage`、`inlineData`、Base64 以及本地 `file://` 图片 URI。
- Windows 的 `file:///C:/...` 与中文用户名目录会正确归一化；macOS 的绝对本地 URI 保持兼容。
- 不会向 Gemini HTTP 响应伪造内部 `generatedMedia` 字段，避免破坏图片生成和普通对话协议。

## 升级与恢复

- 若原有端点补丁仍存在、但图片预览兼容补丁尚未写入，首页会显示该 Antigravity 安装为“待补丁”。点击一次“应用全部补丁”后重启 Antigravity 即可完成升级。
- 已安装的旧 v2 / v3 图片预览补丁会安全迁移到 v4；升级不会覆盖“恢复原始文件”所需的官方原始备份。
- 补丁只作用于已知的渲染器入口；未知版本结构不会被改写，也不会阻止正常代理补丁。
- 所有新修改继续进入原始文件备份、失败回滚和“恢复原始文件”流程。

## 标准安装包

- macOS Universal：`Antigravity-WF-Assistant-macOS-universal-v1.4.20-Installer.pkg`
- Windows x64：`Antigravity-WF-Assistant-Windows-x64-v1.4.20-Setup.exe`

发布页只提供上述两个标准安装包及同次发布的 `SHA256SUMS.txt`。
