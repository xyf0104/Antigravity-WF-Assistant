# Antigravity WF助手 v1.4.24

`v1.4.24` 将经过 Windows 交付验证的图片路由修复同步为 macOS 与 Windows 的正式发布基线，并升级图片预览兼容补丁。

## 图片生成与展示

- 自定义图片模型的来源只会在同一条内部图片子请求中短暂保留。用户在同一会话切回原生 Gemini 后，后续原生图片请求不会再误用先前的自定义图片模型。
- 已识别的 Antigravity renderer 可从旧版图片预览补丁升级到 `image-preview-fallback:v8` 与 `image-generation-ui:v3`：支持 `generatedMedia`、`generatedImage`、`inlineData`、Base64、artifact resolver 与本地图片 URI。
- 图片生成完成后默认展开；标题会区分 GPT 图片模型、已知 Gemini 图片模型、通用 Gemini 模型和未知上游模型。生成步骤尚未提供模型时显示中性状态，不会伪造模型名称。
- 图片预览的本地文件 URI 会转换为 Workbench 可加载的 URI，避免裸 `file:` 路径被 CSP 阻止。

## 本地代理、补丁与启动稳定性

- 本地代理优先使用既有连接方式；若该端口被**非本助手**进程占用，助手不会结束未知程序，而是先绑定一个安全的五位备用端口。只有“应用全部补丁”对已发现目标全部成功后，新的连接方式才会提交为下次启动使用的状态。
- 在连接方式等待重新应用补丁期间，启动 Antigravity 与“仅 IDE 补丁”会明确停止，避免某个已安装版本仍指向旧连接；补丁失败不会把持久状态提前切到新连接。
- Windows 会从标准安装目录、已挂载固定磁盘与卸载注册表发现已验证的 Antigravity 安装；macOS 会检查 `/Applications`、`~/Applications` 与已验证的 App Bundle。发现、备份、写入、验证与回滚均为独立步骤。
- 退出助手时会优雅停止本地代理；活动流式请求在短暂等待后会被关闭，以便 Windows 托盘“退出”、macOS Dock“退出”和菜单栏“退出”均能释放本地连接。

## 模型选择与协议安全

- 自定义模型只会在已识别的模型容器、模型排序和 picker 索引都能同时校验时注入。未知的 Language Server / 模型响应结构会原样通过，并留下脱敏兼容诊断，而不是把部分模型表交给 IDE。
- 自定义模型的 slug 与占位枚举分配在完整校验通过后才会生效。一次失败的兼容探测不会更改已打开模型下拉框所依赖的旧路由。
- 工具调用结果会保留或关联真实的上一步工具调用 ID；缺失 ID 的兼容回传不再伪造一条不匹配的工具结果，从而降低 Claude / OpenAI 工具循环、重复探测与重复回复的风险。
- 这项策略覆盖目前已识别的 Antigravity、Antigravity IDE 与 2.x 安装/响应结构；未来结构发生变化时会安全跳过并保留原生模型，不承诺对未知协议进行强制注入。

## macOS 与 Windows 一致性

- 两端共享模型、账户池、图片、工具、历史恢复、更新检查和本地代理逻辑；差异仅限系统原生发现、托盘/菜单栏和安装器实现。
- macOS 新机在尚未生成 Antigravity 配置时，也可以创建最小私有配置来启用“自动同意终端命令”；补丁与历史同步操作会串行化，避免并发写入覆盖备份或回滚快照。

## 安全升级与兼容边界

- 旧版预览和界面补丁会在可识别时结构化升级；补丁继续使用原始文件备份、事务性写入、失败回滚和“恢复原始文件”流程。
- renderer 修改采用严格的结构匹配。对于未知或已改变的 Antigravity renderer，助手会安全跳过该图片界面补丁，而不会做宽泛的字符串替换或强制注入。
- 因此，本版本不承诺对所有未来或未验证的 Antigravity / IDE renderer 自动适配。发现新的结构时，应先增加脱敏 fixture、语法/运行时测试和实机验证，再扩展匹配器。

## 标准安装包

- macOS Universal：`Antigravity-WF-Assistant-macOS-universal-v1.4.24-Installer.pkg`
- Windows x64：`Antigravity-WF-Assistant-Windows-x64-v1.4.24-Setup.exe`
- 完整性校验：`SHA256SUMS.txt`

发布前和发布后的必做检查见 [发布核验清单](docs/发布核验.md)。发布页只提供上述两个标准安装包及其同次生成的校验清单。
