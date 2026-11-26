# 更新记录

[English](CHANGELOG.md) | **简体中文**

版本遵循 [SemVer](https://semver.org/)，双语条目保持相同版本章节。`0.1.0` 为首次发行版本。

## [Unreleased]

## [0.1.1] - 2026-11-26

### 调整

- 内置 Skill 的标识、源码与安装目录、CLI 帮助、界面元数据、词典和 README 链接统一为 `govmc`。
- Skill 主入口只保留关键操作边界和任务路由，9 份专题参考文档按需加载；环境／权限、Tasks／日志／审计、存储操作分别独立说明。
- 补齐 CLI／stdio MCP 克隆和文件传输的完整流程：查询、预览、确认、跟踪持久化任务、读回验收；移除旧 SOP 固定规格，默认继承母机，仅覆盖用户指定的差异。
- 明确 Codex、Cursor 和 Claude Code 的 Skill 安装路径与更新方式。升级程序不会自动替换已安装 Skill，替换需显式执行，不自动删除其他 Skill 目录；MCP 工具名仍按服务实际 schema 调用。

### 修复

- Python wheel 的元数据包含完整 README，供 PyPI 展示；图片和文档链接指向对应发行标签。

### 验证

- 验证内置 Skill 标识、引用链接及三客户端安装；对照实现检查文档中的 CLI 命令路径／标志和 MCP 示例工具名／参数，不执行示例中的设备操作。
- 验证原生程序与 wheel 的安装一致性，包括 `govmc` Skill 入口、界面元数据和专题参考文档；分发测试也支持直接使用已发布 wheel。
- 按 UTF-8 原始字节比较已安装 Skill，并兼容 LF／CRLF 示例代码块，避免 Windows 换行转换导致误报；Windows CI 覆盖 Skill 安装、文档命令和 wheel 行为。
- TUI 滚动与详情返回回归测试明确等待最后一行，避免积压按键导致状态快照不准确。
- 在 0.1.1 准备期间执行本地已发布制品／WMKS 生命周期检查、Go 竞态／模拟器回归、Linux／Windows CI 和浏览器交互回归。

## [0.1.0]

### 首次发布

- 基于 VMware `govmomi` 与共用 Go 服务，提供原生 `govmc` CLI、全屏 TUI、stdio MCP 和 Web 工作台。
- ESXi 主机与机器清单、电源操作、硬件详情、截图，以及 CPU、内存、网络和磁盘指标。
- CPU／内存、磁盘和网卡配置，提交前预览并确认；磁盘容量只能增加。
- 机器克隆：验证母机已关机，继承母机配置与磁盘置备类型，支持指定目标数据存储、只读预览及确认提交。
- 数据存储浏览、支持的文件在线预览、上传／下载、创建文件夹，以及同主机跨数据存储复制／移动。
- SQLite 持久化任务，提供列表、状态、详情、取消请求及步骤进度，CLI/TUI/Web/MCP 共用查询。
- `~/.govmc/config.yaml` 中的加密 Profile、临时 `--profile` 指定，以及改名后的历史记录归属。CLI/TUI 管理 Profile，Web 切换已有环境。
- TUI Settings 包括语言、MCP 权限、Profiles、任务、运行日志与操作审计；底栏提供版本与 GitHub 入口。
- Web 指标、机器配置、存储浏览器、任务抽屉、外观／语言设置、连接状态与按环境隔离的 IndexedDB 缓存。
- 可选 VMware HTML Console SDK（WMKS），通过 `wmks install/status/verify/uninstall` 单独安装管理；无 SDK 也可启动普通 Web，源码与发布制品均不携带 SDK 文件。
- Web 交互控制台、重新获取票据后的重连，以及导航／刷新／关闭确认。CLI 与 stdio MCP 提供基于票据、有界的键鼠输入。
- MCP 默认只读，写操作需要明确授权；内置离线 Skill 同时支持 CLI 与 stdio MCP。
- 共用中英文词典、易读终端日志、保留原始字段的 JSONL 运行记录与 SQLite 操作审计。
- 原生 ZIP／SHA-256 发布包，以及内含同一 Go 程序的 `govmc-cli` 平台 wheel；版本标签触发 GitHub／PyPI 发布并校验双语更新记录。
- 模拟器集成／竞态、浏览器交互、SDK 安装生命周期及原生／wheel 一致性测试；README 提供基于模拟器的截图与演示动图。

### 范围与限制

- 独立 ESXi 为主要目标，vCenter 的主机／数据中心选择仍有限。
- 本地 `serve` 无账号登录与 Cookie 认证，当前不含 Docker 账号／邀请码管理。
- TUI 提供控制台截图，不提供交互键鼠控制；MCP 尚无通用机器配置编辑工具。
- 任务与历史仅在使用同一本地配置／数据目录的进程间共享；取消不等于回滚，中断流程不会自动重放。
- 项目代码采用 MIT 许可证，第三方组件保留各自许可；VMware WMKS 为单独授权的外部依赖，不随源码或发布包分发。
