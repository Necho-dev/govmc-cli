# 数据存储浏览与文件传输

## 只读定位

```sh
govmc --profile lab datastore list --json
govmc --profile lab datastore files datastore1 templates --json
```

MCP 用 `esxi_ds_ls`，参数 `profile`、`datastore`、`path`，按需指定 `kind: folder|file|all`。区分目录和文件，名称、精确路径和目标余量均取自当前查询。路径按所选 datastore 的相对路径提供；含空格时引用，不把客户机路径或主机本地路径当作 datastore 路径。

## CLI 复制／移动

以下是跨 datastore 的同一文件复制计划；同存储省略 `--dst-datastore`。目标父目录须存在，目标路径不得已存在。

```sh
# 先预览，不创建执行任务。
govmc --profile lab datastore copy datastore1 templates/base.iso imports/base.iso --dst-datastore datastore2 --dry-run --json

# 用户确认该计划后提交；命令前台等待完成。
govmc --profile lab datastore copy datastore1 templates/base.iso imports/base.iso --dst-datastore datastore2 --yes --json
```

移动使用同样参数，将 `copy` 改为 `move`。移动会改变源路径，可能影响引用该文件的对象，确认影响后才提交。交互终端可省略 `--yes` 并在预览后确认；非交互脚本必须显式确认。

## stdio MCP

以实际工具调用参数提交预览；先确认目标 Profile，不使用以下示例中的占位资源直接执行：

```json
{
  "name": "esxi_ds_copy",
  "arguments": {
    "profile": "lab",
    "datastore": "datastore1",
    "src": "templates/base.iso",
    "dstDatastore": "datastore2",
    "dst": "imports/base.iso",
    "dryRun": true
  }
}
```

实际提交保持已确认的路径和目标存储，传 `dryRun:false`、`confirm:true`；可为该次请求指定唯一 `requestKey`，重试规则见 [tasks.md](tasks.md)。移动工具为 `esxi_ds_move`。只读模式允许预览，实际写入要求 readwrite。

## 验收与边界

提交后跟踪返回的 Task ID，终态及步骤处理见 [tasks.md](tasks.md)。复制成功后读取目标类型和大小；移动成功后还需确认源路径已移除。目录逐项验收重要内容，不能仅凭提交回执宣布成功。

仅支持同一 ESXi 可访问的同／跨 datastore，不支持跨 Profile／主机文件迁移。不要用普通文件复制／移动替代已注册 VM 的存储迁移，也不要只复制 VMDK 描述符就声称完成了虚拟磁盘克隆。机器克隆读取 [clone_workflow.md](clone_workflow.md)。失败、取消后先检查双端实际文件，再形成补偿计划，不自动清理资源。

创建／删除文件夹通过 MCP `esxi_ds_folder_create`／`esxi_ds_folder_delete` 或 Web/REST，需授权、readwrite（MCP）及确认；当前不编造 CLI 同名命令。其他预览、上传、下载能力以所选入口的实际帮助和 schema 为准。
