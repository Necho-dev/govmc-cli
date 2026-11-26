# 独立 ESXi 不停机扩容

## 1. 只读检查

1. 确认 ESXi 主机、虚拟机唯一标识、当前电源状态和目标规格。列出现值 → 目标值，只检查本次要增加的资源。用户只要求评估时，到结论为止。若有多台，逐台记录检查结果。
2. 从 ESXi 读回实时属性。MCP `esxi_vm_get`／CLI `vm info` 用于确认身份、规格和设备配置；需要的字段仍缺失时记为未知并说明当前入口限制，不自行读取加密凭据或假设存在通用 PropertyCollector 工具。用虚拟机兼容级别、客户机版本和 ESXi 许可核对热添加支持。查询结果为空或无法读取时记为未知。
3. 按下表逐项判断。

| 资源 | 需要确认 |
|---|---|
| CPU | `config.cpuHotAddEnabled = true`；目标 vCPU 满足客户机和虚拟硬件限制、现有每插槽核心数约束，主机有足够资源。 |
| 内存 | `config.memoryHotAddEnabled = true`、`config.memoryReservationLockedToMax = false`；记录实际预留 `config.memoryAllocation.reservation`。核对 `config.hotPlugMemoryLimit`、`config.hotPlugMemoryIncrementSize` 及主机可用资源，确认目标和增量受支持。 |
| Windows CPU / 内存 | 在运行中的 Windows 内查询 VBS。VBS 启用时按 Broadcom 限制处理；状态未知时不能判定可热添加。 |
| 磁盘 | 确认目标 VMDK、控制器、磁盘模式和 datastore 类型及余量。核对快照链和是否需要合并，按当前 ESXi 版本和磁盘类型确认支持热扩。明确 Windows 中要扩展的卷，确认卷后有相邻未分配空间，或确认扩盘后能形成该布局。 |

Windows 可在管理员 PowerShell 执行以下只读查询。若需要通过控制台发送键鼠输入或在客户机执行命令，先说明目标和动作并获得用户明确同意：

```powershell
Get-CimInstance -Namespace root\Microsoft\Windows\DeviceGuard -ClassName Win32_DeviceGuard |
  Select-Object VirtualizationBasedSecurityStatus, SecurityServicesConfigured, SecurityServicesRunning
```

`VirtualizationBasedSecurityStatus` 的 `0` 为未启用，`1` 为已启用但未运行，`2` 为已启用且运行。仅凭 ESXi 虚拟硬件设置无法判断 Windows 内的实际状态。

## 2. 判断并执行

- 汇报每项是可在线扩、条件不满足，还是缺少证据。Hot Add 开关需要在关机状态调整。
- **停机必须获得明确答复。** 若方案需要关机、重启或关机后再开机，先告知用户涉及的虚拟机、停机原因、计划时间和预计影响。收到用户明确同意该停机方案后，才执行停机及依赖停机的变更。一般扩容授权、仅授权在线扩容、沉默或超时都不能视为停机同意。改安全策略或处理快照也须取得对应授权。
- 条件齐备且用户已授权扩容时，提交前复核目标 VM、电源状态和目标规格。当前 stdio MCP 没有配置写工具；不要绕过 `mcp_readonly`。用户选择其他入口后，CLI `vm edit`、TUI 编辑器或 REST `PATCH /v1/vms/{id}` 可提交受支持的配置。多台机器逐台执行，上一台验收通过后再继续。
- CLI 编辑使用 `govmc --profile NAME vm edit VM --file PATCH.json --hot-add --yes`；`--hot-add` 用于运行状态下受支持的变更，关机变更省略它。当前 CLI 没有编辑 dry-run，先展示查询现值与 JSON 目标差异再确认；TUI 提供预览与二次确认。不能把仅提交了 JSON 当成预览已执行。
- 调用超时或失败时，先查询任务状态与虚拟机配置，确认是否部分生效，再决定下一步。虚拟磁盘扩展不以在线缩盘作为回退。

## 磁盘和网卡配置字段

JSON 字段以实际查询返回的 device key 为准，不根据设备列表位置推断 key：

| 目的 | 配置字段 |
|---|---|
| 扩大已有磁盘 | `expandDisks: [{key, capacityMiB}]`，容量是目标总容量，必须大于现值 |
| 添加磁盘 | `addDiskMiB`，需有可用的受支持控制器及设备槽位 |
| 编辑网卡 | `editNICs: [{key, portgroup, connected, startConnected}]`，仅传要改变的字段 |
| 添加网卡 | `addNIC: {portgroup, adapter, startConnected}`，当前 adapter 为 `vmxnet3` 或 `e1000e` |

项目的在线磁盘扩容校验要求无快照、非共享基础 VMDK、persistent 模式、PVSCSI 或 NVMe 控制器；条件不满足时报告当前实现限制，不能因为 ESXi 其他入口支持而强行提交。禁止缩盘。新盘创建位置、控制器和可用槽位以服务端校验为准。

改端口组或网卡连接可能断开客户机网络，确认影响后再提交。关机网卡不支持修改运行时 `connected`；需要设置开机策略时使用 `startConnected`。

## 3. 验收

从 ESXi 读回 vCPU、内存或目标 VMDK 的容量，再从客户机核对实际识别的 CPU、内存或目标卷容量。确认虚拟机仍在运行、开机时间未变，并检查受影响服务的状态。记录每台机器的前后值和未完成项。ESXi 已接受变更但 Windows 尚未识别时，分别报告两侧结果。

[CPU 热添加](https://knowledge.broadcom.com/external/article/304616) · [内存热添加及预留锁定](https://knowledge.broadcom.com/external/article/311579) · [内存热添加上限](https://knowledge.broadcom.com/external/article/388491) · [Windows VBS 限制](https://knowledge.broadcom.com/external/article/334922) · [磁盘扩展条件](https://knowledge.broadcom.com/external/article/425168)
