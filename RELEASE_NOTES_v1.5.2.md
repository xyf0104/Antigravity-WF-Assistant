# Antigravity WF助手 v1.5.2

v1.5.2 同时提供 Windows x64 与 macOS Universal 安装包。Windows 沿用已经验收通过的 v1.5.1 功能逻辑，仅把应用、安装器和更新识别所需的版本元数据同步为 1.5.2 后重新构建；没有改动 Windows 业务功能。macOS 在保留原有账户、模型、代理、历史和桌面交互能力的基础上，对齐 Windows 已验收的连接与图片使用效果。

## macOS v1.5.2

- 首页提供“全部连接”“仅连接 IDE”“仅连接 Antigravity 2.0”和“恢复原机配置”，连接过程显示阶段、百分比和完成结果。
- 自动发现覆盖 `/Applications`、`/System/Applications`、`~/Applications`、运行中的应用、Spotlight、自定义路径和符号链接去重。
- IDE 优先使用官方 `jetski.cloudCodeUrl` 用户设置链连接本地代理，不再为了代理接入修改 Electron 主进程、扩展或 IDE Language Server。
- JSONC 设置编辑保留注释、格式和其他字段；已有第三方 endpoint 不会被覆盖，恢复时只移除仍由本助手持有的值。
- 已识别的图片 renderer、`product.json` checksum 与用户设置处于同一事务；任一步失败会完整回滚，缺少可信原始备份时不会叠加写入。
- 图片界面支持已验证的旧版与 combined renderer：显示实际图片模型名、默认展开生成结果、保留 Prompt 缩略图，并对同一 URI 的正文重复大图进行十分钟精确去重。
- Antigravity 2.0 使用独立结构检查，验证 `app.asar`、Language Server、内嵌图片界面和对应完整性信息；未知布局会显示不支持原因并保持零写入。
- 修改嵌套 Language Server 时只对该二进制执行必要签名处理，不对整个 Antigravity App 做 ad-hoc 深度重签，避免改变原有应用身份和登录凭据访问。
- 保留账户池、OAuth、API Key / Token / JSON 导入、模型同步、推理等级、识图、文件、工具、图片路由、会话历史、菜单栏、Dock 和退出释放本地代理等功能。

## Windows v1.5.2

- 功能逻辑与已验收的 Windows v1.5.1 保持一致。
- 仅同步应用版本、安装器版本、界面版本和更新识别等发布元数据为 `1.5.2`，并重新生成标准安装包。
- 继续支持 Windows 10/11 x64、桌面快捷方式、通知区域驻留、退出释放本地代理、自动发现 Antigravity、账户池、模型同步、图片生成与显示、补丁备份和失败回滚。
- Windows 功能源码相对 v1.5.1 的差异必须为空；发布前通过测试、安装、启动、通知区域退出和升级覆盖核验。

## 安装包

v1.5.2 Release 上传以下三个文件：

- `Antigravity-WF-Assistant-macOS-universal-v1.5.2-Installer.pkg`
- `Antigravity-WF-Assistant-Windows-x64-v1.5.2-Setup.exe`
- `SHA256SUMS.txt`

不额外发布 portable、独立 EXE、ZIP、配置、日志或测试材料。`SHA256SUMS.txt` 是软件内安全更新的必需校验清单，同时可供手动下载校验使用。GitHub 对公开仓库自动生成的 Source code 归档不是本项目额外上传的发布附件。

## 兼容说明

助手以实际安装结构为准，不以版本号盲目写入。已识别结构会进入备份、候选文件校验、原子替换和失败回滚流程；未知 renderer、未知内嵌界面或完整性信息不匹配时会安全跳过并显示原因。更新 Antigravity 后如结构发生变化，请等待对应兼容更新，不要手动复制其他版本文件。
