---
name: esxi-govc
description: >-
  Operate VMware ESXi hosts and virtual machines through GoVMC CLI and MCP:
  inspect inventory and QueryPerf metrics, browse datastores, view screenshots,
  control the VM console through ticket-authenticated keyboard and pointer input,
  and plan authorized configuration changes or standalone-ESXi directory clones.
  Use for govmc profiles, MCP permissions, hot expansion, and ESXi troubleshooting.
  Use govc only for a requested SOAP cross-check.
---

# GoVMC ESXi 运维

## 入口和环境

支持 GoVMC CLI 和 stdio MCP 两个入口；优先使用已配置的 MCP，用户指定 CLI 时按 CLI 的确认机制执行。客户端启动 `govmc mcp serve` stdio 子进程，不需要 Web 服务。未配置时，用 `govmc mcp json --client codex` 生成配置；`govmc skill install --client codex` 安装本 Skill。Cursor、Claude Code 分别使用 `--client cursor`、`--client claude-code`。Skill 安装名保留为 `esxi-govc`。

先读 `esxi_profiles_list`、`esxi_profiles_current`、`esxi_config_show`，确认环境与有效权限，再用 `esxi_doctor` 检查目标连接。这三个本地查询不连接 ESXi，也不输出密码或密文。

- 环境及加密凭据保存在 `~/.govmc/config.yaml`，无需模型读取密码、解密密钥或设置密码环境变量。
- 环境具有跨重命名不变的稳定 ID。CLI `govmc profiles rename OLD NEW` 或 TUI 编辑名称会自动关联任务、日志及审计历史；用新名称查询即可，详情中的 `originalProfile` 保留原名称，不要求切换全部环境。不要手工改写原始 JSONL 或复制其他环境的 ID。
- 设备工具的 `profile` 参数仅为本次选择环境，不改变 active；省略时跟随 active，启动时显式 `--profile NAME` 则固定进程默认目标。并行调用可分别指定环境。
- 不运行 `profiles use` 来为后续 MCP 调用切换目标；持久切换应由操作者执行。
- CLI 使用 `govmc --profile NAME ... --json`；先查 `profiles list/current`、`config show`，再 `doctor`、`vm list/info`。命令及字段见 [查询与能力边界](references/usage.md)。没有配置 MCP 不影响 CLI 使用，CLI 也不依赖 Web 服务。

## 权限和设备操作

MCP 每次调用重读配置，默认 `readonly`。全局 `mcp.mode` 是权限上限；全局 `readwrite` 时，Profile 的 `mcp.mode` 可进一步限制为 `readonly`，缺省继承。单次 `profile` 不提高权限。工具列表稳定，写调用在只读模式下返回 `mcp_readonly`。

权限调整由操作者通过 CLI 或 TUI 的 Settings → MCP 完成，TUI 支持全局和当前环境模式，保存前需确认。CLI 命令：`govmc config set --mcp-mode readonly|readwrite`、`govmc profiles update NAME --mcp-mode inherit|readonly|readwrite`。`govmc mcp privilege` 查看权限表。没有 `full`，没有进程 `--mode` 覆盖，没有 MCP 权限修改工具。CLI/TUI 调整后下一次调用生效，不追溯取消已开始的操作。

- 只读清单、状态、配置、指标及画面查询可直接执行。截图会在 datastore 临时落盘，服务尝试清理；失败或黑屏按真实结果报告。
- 更改配置、电源、克隆、注册、发送控制台输入、客户机命令执行前，说明目标、动作及影响并获得明确授权；沿用已明确授权的同一范围。只读客户机命令也属于设备操作。
- `readwrite` 只表示服务允许写，不替代用户授权；实际写工具仍需 `confirm=true`。停机或重启需要对应的明确授权，在线扩容授权不包含停机。
- 不为绕过 `mcp_readonly` 自动改用 CLI、REST、govc 或读取密钥。工具未提供所需能力时说明缺口；用户明确选择其他入口后，按该入口的授权执行。
- 操作超时或失败先读回任务、配置与电源状态，确认有无部分生效，不盲目重试写入。

## 按任务读取

- 清单、指标、datastore、截图、工具名称与 CLI 回退：读 [usage.md](references/usage.md)。主机 `Host` 清单不是时序；监控使用 QueryPerf，不能用 QuickStats 代替。
- Web 控制台组件安装、入口提示与依赖判断：读 [web_console.md](references/web_console.md)。WMKS SDK 只服务浏览器交互控制台，不能据此禁用 CLI/MCP/TUI 或其他 Web 操作。
- 远程控制、票据链路、键盘／鼠标、CLI `vm console input`、MCP `esxi_vm_console_*` 与客户机执行条件：读 [remote_control.md](references/remote_control.md)。取票、连接、发送输入和客户机内实际生效是不同验收层级；不要把发送计数当作命令执行回执。
- CPU／内存热添加、磁盘扩容与添加、网卡编辑与添加：读 [hot_expand.md](references/hot_expand.md)。先形成当前值 → 目标值的预览，用户确认后提交，再读回验收。
- 独立 ESXi 克隆与自定义编排：读 [clone_workflow.md](references/clone_workflow.md)。这里定义工作流与验收，底层能力以当前工具列表为准，不编造 CLI 命令或 MCP 工具。
- SOAP 对照或数据口径排查：读 [troubleshooting.md](references/troubleshooting.md)。独立 ESXi 不使用 `govc vm.clone`、链接克隆、内容库或依赖 vCenter 的迁移能力。
