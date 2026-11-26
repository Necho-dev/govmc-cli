# 远程控制与验收

## 选择入口

| 需求 | CLI | stdio MCP | Web / TUI |
|---|---|---|---|
| 读取画面 | `vm screenshot VM --out PATH` | `esxi_vm_screenshot` | Web / TUI 详情截图 |
| 控制台键鼠 | `vm console input VM --actions FILE` | `esxi_vm_console_press_key/type_text/pointer/input` | Web 控制台接管；TUI 当前无键鼠控制 |
| Windows 后台执行并获取输出／退出码 | 当前无 CLI 命令 | `esxi_vm_guest_exec` | REST；要求 Tools 与服务端客户机凭据 |

控制台输入不依赖 VMware Tools，也不需要客户机密码，但客户机必须开机；锁屏、登录、UAC 和输入法均是客户机自身状态。没有 Tools 不能据此推断控制台不可用，也不要为操作控制台自行安装 Tools 或改变 VM 硬件。

CLI 和 stdio MCP 均直接登录所选 ESXi、内部申请新 WebMKS 票据、建立 WSS 再发送动作，不要求 `serve`，不返回可复用的原始票据。没有 `esxi_vm_console_ticket` 工具。MCP 默认 readonly：输入必须由操作者通过 CLI/TUI 开启适用的 readwrite，并在工具参数指定 `confirm:true`。不得因 MCP 拒绝而自动换 CLI 绕过权限；用户明确选择 CLI 的独立操作按其授权执行。

浏览器 WMKS SDK 与 ESXi 控制台服务是两项依赖：CLI/MCP 的 Go 取票／WSS 输入不读取本地 SDK 文件；Web 交互控制台才需要安装 SDK。SDK 缺失不影响截图、配置、电源、克隆或文件管理。ESXi 出票或 WSS 失败仍会影响 CLI/MCP 控制台输入，安装浏览器 SDK 不能修复这些服务端问题。组件排障见 [web_console.md](web_console.md)。

## 标准操作顺序

1. 核对 Profile、唯一 VM ID、名称、电源与必要时的实例 UUID，再截图确认当前窗口和焦点。仅授权截图不等于授权发送输入；只读客户机命令也属于远程输入。
2. 形成短动作计划，说明将在哪个窗口执行什么；沿用用户已明确授权的范围。优先单动作工具；相关组合键与文本可放在一个批次，减少重新连接。
3. 确认客户机输入法与键盘布局。文本按 US 键盘发送，中文输入法可能吞空格、改变字符；不通过猜测输入反复执行命令。Windows Run 对命令长度有限制，长命令应先打开终端再分段输入。
4. 发送后等待画面更新并重新截图，核对实际输出、主机名、IP 或对应状态。`completed` 仅表示完整发送的动作数；RFB 没有命令执行回执。短暂未见变化先重新截图，不能直接重发。
5. 连接中断或报部分发送时，先读取画面确认已生效范围。`completed:0` 也可能部分发送了当前动作；配置、重启等命令禁止盲目重试。若仅后续截图失败，输入仍可能已成功。

普通查看无需自动发唤醒按键。绝对坐标从截图左上角起按客户机像素计算；分辨率变更后重新截图。无 USB Tablet/Tools 时绝对鼠标可能不生效，可评估相对指针或键盘导航；这与取票失败不同。

## CLI

先将动作数组保存为本地 JSON 文件；可能含敏感输入时文件权限设为 0600，完成后按需要清理，避免把密码放到命令行参数。

以下示例**仅用于已经确认焦点位于可用命令行的客户机**：

```json
[
  {"type":"text","text":"hostname"},
  {"type":"key","key":"enter"},
  {"type":"wait","ms":1000}
]
```

```bash
govmc --profile lab vm console input VM --actions actions.json --dry-run --json
govmc --profile lab vm console input VM --actions actions.json --yes --json
govmc --profile lab vm screenshot VM --out /tmp/govmc-after-input.png --json
```

`--dry-run` 只验证动作和目标、显示目标与动作类型，不申请票据或发送输入，不显示文本内容。交互执行可省略 `--yes` 并在提示时确认；非交互执行必须显式 `--yes`。截图输出文件必须不存在。CLI 预览、输入与截图使用同一个显式 Profile。

动作和 MCP 批量参数相同：`key`（`key`, 可选 `modifiers`）、`text`（`text`）、`wait`（`ms`）；指针 `move/click/doubleClick/drag/scroll` 与 `relativeMove/relativeClick/relativeDrag/relativeScroll`。修饰键为 `ctrl/shift/alt/meta`，例如 `{"type":"key","key":"r","modifiers":["meta"]}`；文本本身不包含 Enter。

每批 1–32 个动作，单段文本 1–256 个可打印 ASCII 字符；单次 wait 1–2000ms，总显式等待 ≤5 秒、总事件 ≤2048。CLI 文件 ≤64 KiB，未知字段和无效动作会在连接前拒绝；实际坐标边界在握手取得分辨率后再验证。

## stdio MCP

用实际客户端调用工具；下面只是 `tools/call` 的参数示例，不是要求模型自行启动第二个 MCP 客户端或修改配置。

```json
{
  "name":"esxi_vm_console_input",
  "arguments":{
    "profile":"lab","id":"VM_ID","confirm":true,"screenshot":true,
    "actions":[
      {"type":"text","text":"hostname"},
      {"type":"key","key":"enter"},
      {"type":"wait","ms":1000}
    ]
  }
}
```

单次按键用 `esxi_vm_console_press_key`（`key`, `modifiers`），文本用 `esxi_vm_console_type_text`（`text`），指针用 `esxi_vm_console_pointer`（`type` 和坐标／位移字段）；都带 `id/profile/confirm`，可选 `screenshot:true`。同一批返回动作数及分辨率；可选截图为图像内容。不要把返回文本里的 mode/Profile 元数据当作执行结果。

`esxi_vm_guest_exec` 是另一条 Guest Operations 链路：只有 Tools 运行、Guest Operations ready、服务端已为该 VM 配置客户机凭据时才可用；MCP 参数不接收密码。当前 CLI 没有 `vm guest exec`。缺失条件应报告实际错误，不用控制台偷偷执行同一命令作为重试。

## Web 票据边界

`POST /v1/vms/{id}/console` 只返回本机代理 `/ws/console?id=…&profile=…`，一次性、有效 2 分钟。获取成功只证明出票；还需 WebSocket 升级、WMKS 握手、显示画面、接管后输入并核验结果。过期或已消费时重新取票，不复用旧 URL。Profile 随票据固定，切换环境时关闭旧连接，不能将票据挪到另一环境。

票据和 ESXi WSS URL 都是敏感凭证，不打印到日志、Skill、验收报告或最终答复。排障只记录 VM、环境、连接阶段、错误码和脱敏结果。
