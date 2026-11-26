# 指标与 SOAP 对照

监控使用 QueryPerf；`summary.quickStats` 缺失或异常不表示 VM 未运行。不要为了修复显示而自动重启、注销或重新注册机器。

- 时间戳为 UTC；展示时明确时区。
- 活动内存比例 = `mem.active` / 配置内存，计算前统一单位；不是客户机任务管理器里的全部内存含义。
- CPU% 使用 CPU MHz 用量相对于 vCPU × 主机单核 MHz；缺失分母时不生成百分比。
- 网速按 KBps × 8 / 1000 换算为 Mbps，不能把 KBps 直接当 Mbps。
- VM 各 vNIC 用量之和不等于主机 vmnic，按实例分别解释。实时采样通常为 20 秒，窗口和可用计数器以实际查询为准。

仅在需要 SOAP 对照且操作者已配置 `govc` 凭据时使用：

```bash
govc about
govc find -type m
govc vm.info -json VM_NAME
govc metric.sample -n 180 -t -instance='' VM_NAME cpu.usagemhz.average mem.active.average net.bytesRx.average
govc datastore.ls -ds DATASTORE PATH
```

不要让模型读取 `~/.govmc/config.key` 或解密保存的密码来配置 govc；不要把密码放进命令参数或输出。

缺少 guest hostname/IP 时检查 Tools，不编造字段。截图不支持、截图失败和真实黑屏分别报告。只有在另获授权后才发送按键、执行客户机程序或修改虚拟硬件。
