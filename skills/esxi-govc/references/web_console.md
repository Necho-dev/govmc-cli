# Web 控制台组件与能力边界

WMKS SDK 是浏览器 JavaScript 资源，只供 Web 交互画面、键盘和鼠标使用。CLI/MCP 的 `vm console input`／`esxi_vm_console_*` 由 Go 直接取票和连接 ESXi WSS，不需要此 SDK，也不要求先运行 serve。TUI 截图、Web 总览／指标／截图／硬件编辑／电源／克隆／存储均独立于 SDK。

截图需要 VM 开机、ESXi 支持截图 API 和相关 datastore 权限，不要求 VMware Tools。Guest Operations 后台执行才依赖 Tools、Guest Operations ready、Windows 目标及服务端客户机凭据。不能把缺 Tools、缺浏览器 SDK、ESXi 权限不足或取票失败当成同一个问题。

## 组件安装与查询

使用实际安装版本的 `govmc wmks --help` 确认命令；旧版本没有该命令时先说明版本差异，不伪造 MCP 工具。WMKS 管理仅 CLI 提供，不连接 ESXi、不属于某个 Profile，也不自动下载或接受上游许可。

```sh
govmc wmks status --json
govmc wmks install --file /path/to/SDK.zip --dry-run --json
govmc wmks verify --json
```

实际安装使用 `govmc wmks install --file /path/to/SDK.zip --yes`，仅在用户已授权安装可信 SDK 包后执行；需要在**运行 serve 的主机上**，使用该进程相同的用户与 `--config` 配置文件。浏览器访问者的客户端安装无效。SDK 源码和声明按原包保留，摘要表示完整性，不代表来源或再分发授权已验证；不能从 ZIP 文件名推断 SDK 版本。

SDK 安装／更新后重启 serve，再刷新页面或点击重新检测；重启会断开该服务的 Web 会话，VM 继续运行。不要仅因用户允许安装就替其停止服务。运行中的服务固定启动快照；单独刷新不能让旧进程采用新组件。使用中的 SDK 不可卸载；`wmks uninstall --yes` 需要明确授权。

ZIP 可含外层目录，但整个包只能有一份 `wmks.min.js`；同一 SDK 根目录必须有非空的 `wmks.min.js`、`css/wmks-all.css`、`img/touch_sprite.png`、`img/touch_sprite_feedback.png`。保留上游声明。拒绝加密、重名、穿越和符号链接；ZIP／单文件解压／累计解压上限分别为 32／16／128 MiB，最多 1,024 个条目。不需要 plugin.json 或另打包 jQuery，结构校验也不证明发布者、版本或兼容性。

## 排障顺序

1. 控制台入口仍保留。未安装或校验失败时应看到安装／修复指引，普通 Web 页面继续可用；不为检测组件发送控制台输入。
2. 在 serve 所在配置目录查询 `wmks status`／`verify`。CLI 状态反映磁盘安装；Web `/v1/console/capabilities` 反映该服务启动快照。两者不同先确认同一配置目录及进程是否重启，不修改 Profile 凭据。
3. 组件 ready 后还需要 VM 开机、ESXi 允许出票、代理 WSS 和浏览器握手。SDK ready 不代表设备权限或会话已经成功。API 401 先处理服务认证；404 可能是旧 serve，不能断言 SDK 未安装。
4. 输入后重新截图验收。取票／输入失败不自动换入口绕过限制；仅后续截图失败也不能盲目重发已发送的输入。

本地 SDK 组件状态不改变 MCP readonly/readwrite；Web/REST 的访问控制也不由 MCP mode 决定。

SDK 目录使用 YAML `wmks.directory`（相对配置目录）。`govmc serve --wmks /path/to/sdk` 可临时覆盖，支持托管安装或解压目录（直接含 wmks.min.js、css/、img/），不写回配置；相对参数以工作目录解析。优先级：参数 > YAML > 配置目录下 wmks/。
