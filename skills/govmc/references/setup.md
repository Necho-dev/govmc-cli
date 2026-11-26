# 环境、权限与 Skill 安装

## 选择入口和版本

优先用已连接的 stdio MCP；没有配置 MCP 时可直接使用 CLI；原生二进制不需要 Python／Go 编译器，两种入口均不要求 Web 服务。先根据当前安装版本的 `govmc --version`、命令 `--help` 和实际工具 schema 确认可用能力。MCP 工具名称仍使用服务声明的 `esxi_*`，不要从 Skill 名称推导或改写工具名。

仅在用户需要安装或配置时使用：

```sh
govmc skill show
govmc skill install --client codex
govmc mcp json --client codex
```

Cursor／Claude Code 对应 `--client cursor`／`--client claude-code`。Skill 名称和安装目录均为 `govmc`：`~/.agents/skills/govmc`、`~/.cursor/skills/govmc`、`~/.claude/skills/govmc`。所有引用文档随可执行程序内嵌，安装不需要网络或仓库源码。

升级程序不会自动更新已安装的 Skill；需要更新时用 `govmc skill install --client CLIENT --force`。它替换该客户端的整个 `govmc` Skill 目录；先确认有无需要保留的本地修改。旧版本以其他名称安装的目录不会被此命令清除，不自动删除用户的其他 Skill。

MCP 由客户端启动 `govmc mcp serve` 子进程，stdout 用于 JSON-RPC；无 HTTP MCP、`--mcp-stdio` 或 `--mode` 进程覆盖。不要为普通工具调用自行另起第二个 MCP 客户端。

## 环境定位

MCP 用 `esxi_profiles_list`、`esxi_profiles_current`、`esxi_config_show`；CLI 对应：

```sh
govmc profiles list --json
govmc profiles current --json
govmc config show --json
```

这些本地查询不连接 ESXi，也不返回密码或密文。只有需要访问设备时才运行 `esxi_doctor` 或 `govmc --profile NAME doctor --json`。

默认配置为 `~/.govmc/config.yaml`；跨端状态跟随配置文件所在目录，详见 [tasks.md](tasks.md)。环境凭据由 CLI/TUI 输入并加密保存，无需模型读密码、`config.key` 或设置密码环境变量。

CLI 单次 `--profile NAME` 与 MCP 单次 `profile` 不修改 active；进程 `govmc --profile NAME mcp serve` 只固定默认目标，单次参数仍优先。不要为一次操作运行 `profiles use`。Profile 新增、编辑、删除和权限管理由 CLI/TUI 提供，Web 仅切换已有环境，MCP 没有这些修改工具。

CLI `profiles rename OLD NEW` 或 TUI 编辑名称保留稳定 ID；新名称可直接查询历史，`originalProfile` 保留记录原名。不要手改原始 JSONL 或复制其他环境 ID。

## MCP 权限

默认 `readonly`。全局 `mcp.mode` 为上限，Profile 可进一步设为 `readonly`，缺省继承；每次调用重新读取。只读下工具列表保持稳定，设备写调用返回 `mcp_readonly`，克隆／复制／移动的 dry-run 仍可用。

只有用户要求调整权限时，说明影响并由 CLI 或 TUI Settings → MCP 设置；MCP/Web 不能提高权限：

```sh
govmc mcp privilege --json
govmc config set --mcp-mode readwrite
govmc profiles update NAME --mcp-mode readonly
govmc profiles update NAME --mcp-mode inherit
```

全局也可设回 `readonly`；Profile 还支持 `readwrite`，但不能突破全局上限。设备写调用仍需 `confirm:true`，模式变更不取消已经开始的任务。不要因工具拒绝而切换入口绕过权限。
