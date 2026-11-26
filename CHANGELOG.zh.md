# 更新记录

[English](CHANGELOG.md) | **简体中文**

版本遵循 [SemVer](https://semver.org/)，双语条目保持相同版本章节。`0.1.0` 为首次发行版本。

## [Unreleased]

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
