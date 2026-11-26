# 独立 ESXi 克隆与编排

Skill 负责规格选择、自定义编排和验收，Go 服务负责克隆操作与持久化任务。使用 `govmc vm clone --dry-run` 或 stdio MCP `esxi_vm_clone`（默认 `dryRun:true`）先预览；CLI 执行需要确认或 `--yes`，MCP 执行需要 readwrite 与 `dryRun:false, confirm:true`。

独立 ESXi 使用 datastore 普通文件复制 + VirtualDiskManager 完整磁盘复制（显式继承精简／厚置备；目录浏览会隐藏 flat extent，不能只复制描述符）→ 改 `.vmx`（生成新的 `vc.uuid`）→ 注册 → 开机回答「I copied it」的流程，不使用 `govc vm.clone`。

## 预览与选择

1. 查询母机的唯一标识、主机、datastore、目录、CPU、内存、磁盘、网卡及电源状态。记录目标名称、目标 datastore 与目标目录，先确认不会覆盖已有资源。
2. 默认继承母机规格、磁盘置备方式和设备配置，不主动套用固定规格或统一转为精简置备。只有用户明确选择既有 SOP 时才使用其规格（8 vCPU、24 GiB 内存、80 GiB 目标盘容量）；沿用用户已做出的选择，不在后续步骤重复询问。
3. 默认不传 CPU、内存等覆盖参数，以继承母机；仅将用户明确选择的覆盖值传给克隆服务（例如 24 GiB 为 `memoryMB: 24576`）。禁止缩容，不能声称克隆接口会把更大的盘缩成 80 GiB；有差异先说明。
4. 使用选定参数 dry-run，展示源／目标、规格、网卡、开机策略及计划步骤。实际提交前获得该方案的确认。

## 当前 REST 克隆服务

由操作者选择并配置的本机 `govmc serve` 提供：

- `POST /v1/custom/clone`：默认 dry-run；实际提交显式 `dryRun: false`、`confirm: true`，长任务用 `async: true`。
- `GET /v1/custom/clone/tasks/{taskId}`：查询任务及各步结果。
- `POST /v1/custom/clone/tasks/{taskId}/cancel`：取消请求，需确认。取消不等于回滚已完成步骤，不承诺自动清理资源。

此服务不具有标准 vCenter clone 的事务能力。目标数量较多时逐台编排，记录 taskId、实际创建对象和验收结果，不在失败时重复提交同名克隆。

## 子机首次开机前

- 默认保持子机关机，内存锁定／预留和 CPU／Memory Hot Add 均继承母机；不要为套用旧 SOP 自动发送 `lockMemory:false` 或开启热添加。
- 读回 `config.memoryReservationLockedToMax`、`config.memoryAllocation.reservation` 与热添加设置，和母机／用户覆盖计划比较。只有用户明确要求调整时才修改子机，改后再次读回，不顺带修改母机。
- 若用户明确选择既有 SOP，才使用其中的热添加／预留覆盖。克隆请求支持 `lockMemory`、`memoryReservationMB`、`cpuHotAddEnabled` 和 `memoryHotAddEnabled`；CLI 用对应标志，不添加不存在的字段。
- 网卡类型、端口组及身份必须与计划一致，避免重复 MAC；不能仅根据新显示名认定 UUID、网卡或磁盘已正确处理。

## 验收和失败处理

查询子机的唯一身份、vCPU、内存、目标 VMDK 容量、网卡、预留锁定和热添加设置。按已授权计划开机并处理「I copied it」，检查电源、Tools、IP 和指标；Tools 缺失时将 guest 信息记为未知。

超时或失败先查任务状态和目标对象，再决定继续、停止或补偿。删除目录、注销、停机等补偿动作须有对应授权，不把部分成功视为全部失败并盲目重跑。

## 任务跟踪

用 `esxi_tasks_list` / `esxi_task_status`（参数 `taskId`）或 CLI `tasks list/status TASK_ID` 跟踪提交返回的 `id`，需要停止时使用 `esxi_task_cancel`（确认、readwrite）或 `tasks cancel --yes`。母机必须关机；不支持的快照链、外部盘、加密和 vTPM 明确阻止。退出执行进程后任务可能中断；不自动恢复或回滚。

目标存储可由 CLI `--dst-datastore TARGET`、MCP/REST `dstDatastore` 或 Web/TUI 选择。省略则同源；源 datastore 从 VMX 推断，显式 `datastore` 必须与源一致。仅限同一 ESXi 可访问存储。先预览双端路径、容量和扩容，再确认提交；任务持久化到 data.db。目标目录必须不存在，失败不自动回滚或删除。
