# 清单、指标与入口能力

## 设备只读查询

先确认 Profile 与连接，再用 `esxi_vm_list`／`esxi_vm_get` 选定唯一 VM ID；不要仅凭近似名称执行写操作。`esxi_host_get` 返回主机清单和端口组，`esxi_metrics_query` 才是 QueryPerf 时序；主机指标使用 `host:true`，VM 指标使用 `id`。设备工具可带单次 `profile`。

CLI 对应示例：

```sh
govmc --profile lab doctor --json
govmc --profile lab vm list --json
govmc --profile lab vm info VM_ID --json
govmc --profile lab host info --json
govmc --profile lab metrics counters --entity vm --json
govmc --profile lab metrics vm VM_ID --names cpu.usagemhz.average,mem.active.average --json
govmc --profile lab metrics host --names net.bytesRx.average,net.bytesTx.average --instance '*' --json
govmc --profile lab vm screenshot VM_ID --out /tmp/govmc-screen.png --json
```

计数器与实例以当前服务可用目录为准，按需选择，不能将 QuickStats 当作时序替代。Tools、IP、客户机名称或指标缺失时记为未知，不补造数值。时间／单位异常才读取 [troubleshooting.md](troubleshooting.md)。

截图路径必须不存在，不能覆盖已有文件。截图不要求 Tools，但要求 VM 开机、ESXi API 和 datastore 权限；服务在 datastore 临时落盘并尝试清理。API 不支持、传输失败和真实黑屏分别报告，不自动发送唤醒按键。

## 能力对照

| 操作 | stdio MCP | CLI／其他入口 |
|---|---|---|
| 清单、详情、指标、截图 | `esxi_vm_list/get/screenshot`、`esxi_host_get`、`esxi_metrics_query` | 上述命令 |
| 电源、待答问题 | `esxi_vm_power`、`esxi_vm_answer_question` | `vm power ACTION VM_ID`；REST 另提供答题 |
| 控制台键鼠 | `esxi_vm_console_input/press_key/type_text/pointer` | `vm console input`；Web 交互控制台；TUI 仅截图 |
| Windows 后台执行 | `esxi_vm_guest_exec` | REST；当前 CLI/TUI 没有 Guest Operations 命令 |
| 存储浏览、复制／移动 | `esxi_ds_ls/copy/move` | `datastore list/files/copy/move`、Web/TUI |
| 创建／删除文件夹 | `esxi_ds_folder_create/delete` | Web/REST |
| CPU／内存／磁盘／网卡配置 | 当前无通用配置写工具 | `vm edit`、TUI、Web/REST |
| 克隆 | `esxi_vm_clone` | `vm clone`、Web/TUI |
| Tasks、运行日志、审计 | `esxi_tasks_list`、`esxi_task_status/cancel`、`esxi_logs_list`、`esxi_audits_list` | `tasks list/status/cancel`、`logs`、`audits`、Web/TUI |

表中斜杠表示多个工具名，不是可执行名称；按实际 schema 调用，不按别的入口补造 MCP 工具。

## 电源与下一步

`vm power on|off|reset|suspend VM_ID` 对应开机、**硬关机、硬重置**和挂起，`off/reset` 不是客户机正常关机／重启。操作前确认用户授权包含相应影响；MCP 需要 readwrite 与 `confirm:true`，CLI 非交互执行要 `--yes`。需要正常关机时，先评估已授权的客户机操作入口，不能将硬关机作为默认替代。

配置变更读 [hot_expand.md](hot_expand.md)，控制台或后台执行读 [remote_control.md](remote_control.md)；普通清单查询不需要加载这些流程。
