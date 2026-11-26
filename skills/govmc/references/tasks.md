# Tasks、日志与审计

## 查询不依赖 ESXi 在线

只有实际提交的 `vm.clone`、`file.copy`、`file.move` 创建执行任务；普通查询、截图、控制台输入、电源和 dry-run 不自动生成这类 Task，不代表它们没有日志或审计。提交返回任务 ID 不代表执行完成。

```sh
govmc --profile lab tasks list --status active --json
govmc --profile lab tasks status TASK_ID --json
govmc --profile lab tasks status TASK_ID --watch --timeout 10m --json
```

`tasks list` 无 TASK_ID 参数。它支持 `--type`、`--source`、`--search`、`--since`、`--until`、`--limit`、`--offset`；`--all-profiles` 显示全部环境。MCP 对应 `esxi_tasks_list`（`kind` 对应 CLI 的 `--type`）及 `esxi_task_status`（`taskId`），均可带 `profile`。详情查询应显式选择任务所属环境。

`--watch` 输出连续 JSON 快照，不是一个 JSON 数组；看到命令正常退出仍需检查终态，不能将它等同于任务成功。MCP 没有 watch 参数，按需轮询状态，任务进入终态后停止。

| 状态 | 处理 |
|---|---|
| `queued`／`running` | 跟踪步骤和已知进度；没有百分比时不推算 |
| `cancelling` | 取消处理中，继续查询 |
| `succeeded` | 检查结果并读回实际对象验收 |
| `failed`／`cancelled`／`interrupted` | 查看错误和已完成步骤，再查目标是否部分创建或变更 |

## 取消与进程生命周期

用户授权取消后：

```sh
govmc --profile lab tasks cancel TASK_ID --yes --json
```

MCP 用 `esxi_task_cancel`，传 `profile`、`taskId`、`confirm:true`，要求有效 readwrite。收到 `cancel_requested` 不代表已取消；继续查终态。取消不会回滚完成步骤，也不自动删除文件或注销 VM。

CLI 克隆和文件传输前台等待，不能提交后立即杀掉命令。MCP 任务由该 stdio 进程执行，serve 任务由服务进程执行；SQLite 持久化的是跟踪记录，不是可在进程退出后自动继续执行的后台调度器。执行进程退出／失联可能产生 `interrupted`。停止或重启进程前确认活动任务和影响。

## 跨端和重试

CLI／TUI／Web／MCP 必须使用同一个配置目录下的 `data.db`。同名 Profile 处于不同目录，不会自动同步；另一个主机或 Docker 挂载中的数据库也不会自动合并。查不到记录时先核对配置路径、当前 Profile、状态筛选和分页，再考虑 `--all-profiles`，不要直接重提任务。

环境重命名保留稳定 ID，用新名称查询即可；记录的 `originalProfile` 是原名，不是要求切回旧环境。不要手工更改 ID、数据库或 JSONL 来拼接历史。

MCP 克隆与复制／移动支持 `requestKey`。同一已确认请求的重试复用相同 key 和参数；更改参数不复用 key。已取得任务 ID 时优先查该任务。失去响应但没有 ID 时，先查询列表及目标对象，再决定是否重试；CLI 当前不提供同名 `--request-key` 标志，不编造。失败后的新执行计划需要重新检查已生效范围。

## 日志与审计

```sh
govmc --profile lab logs --source mcp --limit 50 --json
govmc --profile lab audits --outcome failed --limit 50 --json
```

MCP 用 `esxi_logs_list`／`esxi_audits_list`，支持 `profile/source/level/outcome/search/since/until/limit/offset`；时间范围使用 RFC3339（包含时区）。CLI 同名筛选通常用 `--`，`--profile` 筛选环境。日志中的错误与任务步骤需结合读取，输出仅使用脱敏元数据，不粘贴凭据、票据或客户机敏感内容。
