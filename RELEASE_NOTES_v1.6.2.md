# Antigravity WF助手 v1.6.2（Windows）

本次仅发布 Windows x64；macOS 继续保持 v1.5.9，不重新构建、不替换现有安装包。

## 图片重复显示修复

- 修复生成图片后同时出现上方 Prompt 卡片和下方正文大图的问题。
- IDE 1.x / 2.x 的通用去重规则升级为 `image-generation-dedupe:v4`。
- Antigravity 2.0 的内嵌 UI 去重规则升级为 `agent-image-generation-dedupe:v2`。
- 同一图片即使被 Antigravity 另存为不同 artifact URI，也只消费一个最近的生成事件并隐藏一次正文重复项；随后出现的普通 Markdown 图片不受影响。
- 旧 IDE `v2/v3` 和 Antigravity 2.0 `v1` 去重补丁会被识别为待升级状态，重新连接时自动安全迁移，不再误报为当前补丁。
- Antigravity 2.0 迁移会回收旧补丁写入 ZIP comment 的等长填充空间，重建后仍保持 `language_server.exe` 的原始总字节长度。

## 保持不变

- 上方 Prompt 图片卡片继续默认展开并显示实际生图模型名称。
- 推理等级继续显示上游原始值：`auto / none / minimal / low / medium / high / xhigh / max`，并按模型实际能力过滤。
- 普通 Markdown 图片、不同会话中未关联生成事件的图片以及未识别的未来 renderer 结构不进行猜测式全局替换。
- 不伪造 `generatedMedia` 字段，不读取或修改账户凭据、聊天记录与用户模型配置。
