---
name: govmc
description: >-
  Manage VMware ESXi with govmc CLI or stdio MCP. Use for machines, metrics,
  console input, datastore transfers, cloning, profiles, permissions, tasks,
  logs, audits, and WMKS setup.
---

# govmc

优先使用已配置的 GoVMC stdio MCP；用户指定 CLI 时使用 CLI。两者均不要求先启动 Web。命令以当前 `govmc --version`、相关 `--help` 和 MCP 工具 schema 为准，不编造缺失能力。

## 开始前

- 明确本次 Profile；CLI 用 `--profile NAME`，MCP 用单次 `profile`，不要靠修改全局 active 切换后续调用的目标。新环境、权限和安装问题才读取 [setup.md](references/setup.md)。
- Tasks／日志／审计是本地查询，不要求 ESXi 在线；设备操作先确认连接及唯一 VM 标识。只读取当前任务需要的数据，不默认遍历所有环境和全部指标。
- 写操作遵循用户已授权范围，先展示目标与影响，再按入口提交确认。`readwrite` 不代表用户授权；MCP 只读拒绝后不能自动换 CLI／REST 绕过。停机、重启及失败后的删除／补偿须包含在授权范围内。
- 不读取或展示密码、配置密钥、原始票据。失败后先读回状态，不盲目重试。SDK 缺失只影响 Web 交互控制台。

## 按需加载

先选择与请求相符的文档；只有流程涉及另一领域时才继续读取链接，不一次加载全部参考文档。

| 当前任务 | 读取文档 |
|---|---|
| 安装／更新 Skill、选择 Profile、配置 MCP 或权限 | [setup.md](references/setup.md) |
| 主机／机器清单、指标、截图、电源、入口能力对照 | [usage.md](references/usage.md) |
| 查询／跟踪／取消任务，日志、审计或跨端记录不一致 | [tasks.md](references/tasks.md) |
| 存储浏览、同／跨 datastore 复制和移动 | [datastores.md](references/datastores.md) |
| 克隆、继承母机配置、预览和验收 | [clone_workflow.md](references/clone_workflow.md) |
| CPU／内存／磁盘／网卡配置与在线扩容 | [hot_expand.md](references/hot_expand.md) |
| 远控画面、键鼠输入、客户机后台执行 | [remote_control.md](references/remote_control.md) |
| Web WMKS 安装、依赖或控制台组件不可用 | [web_console.md](references/web_console.md) |
| 时间／指标口径异常或用户要求 SOAP 对照 | [troubleshooting.md](references/troubleshooting.md) |
