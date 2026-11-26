# 查询与能力边界

## 推荐读取顺序

1. `esxi_profiles_list` / `esxi_profiles_current` / `esxi_config_show`：脱敏环境、默认环境、全局与进程默认目标的有效权限，不接受修改参数。
2. `esxi_doctor`：确认认证、实际目标及 mode；`esxi_vm_list` 后用 `esxi_vm_get` 确认唯一 VM ID、规格、设备 key、电源、Tools、IP 等。
3. `esxi_host_get` 查主机清单、端口组；`esxi_metrics_query` 查主机或 VM 的 QueryPerf 时序。
4. `esxi_ds_ls` 查 datastore 路径，`kind` 可为 `folder`、`file`、`all`；`esxi_vm_screenshot` 读取画面。

除三个本地环境查询外，以上工具可接受单次 `profile`。使用工具实际声明的字段；缺失字段记为未知，不推断 Tools 状态、guest hostname、可热添加能力或无数据时的利用率。截图不支持时报告不可用，黑画面不等于传输失败，不为获取画面自动发送唤醒按键。

## CLI 只读回退

```bash
govmc --profile lab doctor --json
govmc --profile lab vm list --json
govmc --profile lab vm info VM --json
govmc --profile lab host info --json
govmc --profile lab metrics counters --entity vm --json
govmc --profile lab metrics vm VM --names cpu.usagemhz.average,mem.active.average --json
govmc --profile lab metrics host --names net.bytesRx.average,net.bytesTx.average --instance '*' --json
govmc --profile lab datastore list --json
govmc --profile lab datastore files DATASTORE PATH --json
govmc --profile lab vm screenshot VM --out /tmp/govmc-screen.png
```

截图输出文件必须不存在，不能覆盖已有文件。先查计数器目录再选择 name/instance；datastore 名称和 VM 标识均取自实际查询，包含空格时按终端语法引用。

## 当前能力分布

| 操作 | stdio MCP（`govmc mcp serve`） | CLI / 其他入口 |
|---|---|---|
| 清单、VM 详情、主机信息、QueryPerf、目录浏览、截图 | 对应 `esxi_*` 查询工具 | 上述只读命令 |
| 电源与待答问题 | `esxi_vm_power`、`esxi_vm_answer_question`，需读写权限和确认 | `vm power ACTION VM`；API 另提供答题 |
| 控制台输入 | `esxi_vm_console_input/press_key/type_text/pointer`，需 readwrite、confirm | CLI `vm console input VM --actions FILE --dry-run` 预览；确认后 `--yes`，自动取票；Web 连接并接管键鼠；TUI 当前只显示截图，没有交互控制台 |
| 客户机后台命令 | `esxi_vm_guest_exec`，需 readwrite、confirm、Tools 和服务端凭据 | REST；当前 CLI/TUI 没有 Guest Operations 执行入口，不能把控制台输入当作有 exitCode 的后台执行 |
| datastore 复制／移动 | `esxi_ds_copy/move`，默认 dry-run，写入需读写权限和确认 | CLI `datastore copy/move DS SRC DST --dry-run`；确认后执行，TUI 在目录中 `c` 复制／`m` 移动，四端均持久化 Task |
| datastore 创建／删除 | `esxi_ds_folder_create/delete`，需读写权限和确认 | Web/API |
| CPU／内存／磁盘／网卡编辑 | 当前 stdio 没有 VM 配置写工具 | CLI `vm edit`、TUI 编辑器或 REST PATCH |
| 克隆与任务查询／取消 | `esxi_vm_clone`、`esxi_tasks_list`、`esxi_task_status`、`esxi_task_cancel` | CLI `vm clone`、`tasks list/status/cancel`；Web/TUI 共用服务 |
| 运行日志／操作审计 | `esxi_logs_list`、`esxi_audits_list`，本地只读、支持 profile 和分页 | CLI `logs/audits`、TUI Settings、Web 设置页，共用历史归属 |
| 环境／权限修改 | 没有修改工具 | 操作者 CLI `profiles` / `config set --mcp-mode` 或 TUI Settings |

MCP 只使用 `govmc mcp serve` stdio；HTTP `/mcp` 和旧 `--mcp-stdio` 已移除。克隆及文件复制／移动实际提交返回持久化任务 ID，不以提交成功替代执行成功。

`esxi_vm_guest_exec` 适用于当前实现支持的 Windows 后台程序执行；VMware Tools、目标凭据、程序权限等条件以服务端返回为准。凭据不作为 MCP 参数提供。

CLI 文件操作只预览用 `--dry-run`；交互终端预览后输入 y，脚本显式 `--yes`。目标为所选 datastore 的精确相对路径且不得存在，父目录须存在。CLI 显示任务 ID 并前台等待；可由另一端 `tasks status/cancel` 跟踪或取消。不得把预览当执行成功。

跨存储复制／移动限定同一 ESXi：CLI 增加 `--dst-datastore TARGET`，Web/TUI 表单选择目标存储，MCP `esxi_ds_copy/move` 传 `dstDatastore`。省略则与源相同。先预览双端存储与路径再确认，任务记录和锁均包含双端信息。不要用文件移动替代已注册虚拟机的存储迁移。

虚拟机跨存储克隆：CLI `vm clone BASE CHILD --dst-datastore TARGET --dry-run`，MCP `esxi_vm_clone` 使用 `dstDatastore`。源存储可从 VMX 推断，目标默认与源相同。Web/TUI 提供同样选择；预览后确认提交并跟踪 `vm.clone` Task。母机须关机，跨 Profile／ESXi 不支持。

环境重命名使用 CLI `profiles rename OLD NEW` 或 TUI Profiles 编辑。稳定 ID 保持不变，查询结果的 `profile` 显示当前名称，`originalProfile` 在改名时保留原始名称；Task ID、状态、时间、步骤及原始 JSONL 不变。历史查询用当前 Profile 即可，不用 `allProfiles`。旧名称被新环境复用时会获得新的稳定 ID，旧历史不会混入。MCP `esxi_logs_list/esxi_audits_list` 支持 `profile/source/level/outcome/search/since/until/limit/offset`，时间使用 RFC3339，无需连接 ESXi。
