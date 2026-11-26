# 独立 ESXi 克隆与验收

Skill 负责选择参数、取得授权、自定义编排和验收；CLI／MCP／Web／TUI 调用共用 Go 克隆服务。不要在 Skill 中重新拼接文件复制、改 VMX 和注册脚本作为标准克隆的替代实现。

## 形成计划

1. 读取母机唯一 ID、当前电源和连接、VMX 路径、存储、CPU、内存、磁盘置备和网卡配置。母机必须已关机且已连接；用户只要求预览时，不擅自关机来满足条件。
2. 默认继承母机配置、精简／厚置备、预留锁定和热添加设置，不套用固定规格。只有用户指定的差异才传覆盖值；不同子机需分别保留计划。
3. 确认目标名称和目标 datastore；目标名称也是新目录名，必须不存在且不能含目录分隔符。目标存储默认同源，跨存储也限于同一 ESXi；源存储从 VMX 推断，显式 `datastore` 必须与源一致。
4. 预览并展示源／目标、容量、置备、网卡、配置覆盖和开机策略，用户确认此计划后才提交。默认保持子机关机，开机必须包含在授权范围内。

快照链、外部盘、加密、vTPM 等不支持的源配置以预览校验结果为准，不为绕过校验主动删除设备或快照。磁盘只允许扩大，容量参数是目标总容量，不是增量；不能承诺缩盘。

## CLI 流程

```sh
govmc --profile lab vm info SOURCE_VM_ID --json
govmc --profile lab vm clone SOURCE_VM_ID CHILD --dst-datastore datastore2 --dry-run --json

# 仅在用户确认预览以后执行，默认保持关机。
govmc --profile lab vm clone SOURCE_VM_ID CHILD --dst-datastore datastore2 --yes --json
```

保留返回的 Task ID。CLI 前台等待，不要提交后终止进程。用户没有指定覆盖值时不传 `--cpu`、`--memory-mb` 等参数；覆盖标志以 `vm clone --help` 为准，不能把“默认 false”误当作应显式覆盖母机设置。

当前版本克隆 JSON 使用子命令 `--config`，与全局 YAML `--config` 同名，不能将配置 YAML 误传为克隆 JSON。需要自定义配置路径且遇到此冲突时说明 CLI 限制，由用户选择可用入口；不能自动改用 MCP 或其他入口绕过用户选择或权限，不编造未实现的 `--file` 参数。

## stdio MCP 流程

以下为 `tools/call` 的预览参数示例，资源值须替换为实际选择结果：

```json
{
  "name": "esxi_vm_clone",
  "arguments": {
    "profile": "lab",
    "src": "SOURCE_VM_ID",
    "dst": "CHILD",
    "dstDatastore": "datastore2",
    "powerOn": false,
    "dryRun": true
  }
}
```

提交同一确认计划时显式设 `dryRun:false`、`confirm:true`，要求有效 readwrite。预览返回 `configuration.sourceVersion` 时将该值作为提交参数 `sourceVersion`，避免母机在两次调用间变化；变化错误需重新查询、预览并核对计划。可为该次请求指定唯一 `requestKey`，同一请求重试保持 key 和参数不变；后续跟踪读 [tasks.md](tasks.md)。

覆盖字段按实际 schema 选择：`numCPU`、`memoryMB`、`portgroup`、`cpuHotAddEnabled`、`memoryHotAddEnabled`、`lockMemory`、`memoryReservationMB`、`expandDisks`。例如内存单位为 MiB，容量转换后再提交；默认省略未授权修改的字段。

## 验收及失败处理

任务成功后从 ESXi 读回子机身份、vCPU、内存、每块 VMDK 的存储路径、容量及置备方式、网卡、预留锁定和热添加设置，与母机和已确认覆盖值逐项对照。服务使用完整虚拟磁盘复制并处理新身份，不由目录列表中看不到 flat extent 推断磁盘已完整或为空。

按计划保持关机；开机获授权时再检查新 MAC、待答问题（如“I copied it”）、Tools、IP 和指标。显示名称不会自动修改客户机 hostname、DHCP 绑定或远程软件 ID，这些是独立的客户机／网络操作，需要相应计划与授权。Tools 缺失时 guest 信息记为未知。

失败／取消／超时先查询 Task 和实际目标，确认已完成步骤。默认不自动回滚、删除目录或注销子机；不重复提交同名克隆来掩盖失败，不将厚置备副本验收为继承了精简母机。

## 仅在用户选择 REST 时

现有 `serve` 提供 `POST /v1/custom/clone`、`GET /v1/custom/clone/tasks/{taskId}` 和 `POST /v1/custom/clone/tasks/{taskId}/cancel`。实际提交显式 `dryRun:false`、`confirm:true`，长任务用 `async:true`；接口和鉴权以服务 OpenAPI 为准。CLI／stdio MCP 的标准流程不依赖先启动此服务。
