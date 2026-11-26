async (page) => {
  // Run from the repository root with Playwright CLI run-code --filename.
  await page.setViewportSize({width:1440,height:900});
  await page.goto("about:blank");
  await page.unrouteAll({ behavior: "wait" });
  let sdkAvailable=false;
  const sdkPrefix="/wmks/"+"a".repeat(64)+"/";
  const fixtureOrigin = "http://127.0.0.1:18765";
  const gib = 2 ** 30;
  let screenshotRequests = 0;
  const vms = [
    { id: "1", name: "Build Windows", guestOS: "", guestId: "windows11_64Guest", vbsEnabled: false, guest: { toolsStatus: "toolsOk", toolsVersion: "2147483647", toolsVersionText: "open-vm-tools 13.0.10 build 25056151", ipAddresses: ["192.0.2.1", ...Array.from({ length: 9 }, (_, i) => "2001:db8::" + (i + 1))] }, powerState: "poweredOn", connectionState: "connected", cpuPct: 25, memActivePct: 31.5, netRxMbps: 6, netTxMbps: 2 },
    { id: "2", name: "DB Ubuntu", config: { cpuHotAddEnabled: true, deviceEditable: true, devices: [{ key: 1000, type: "ParaVirtualSCSIController" }] }, powerState: "poweredOn", connectionState: "connected", cpuPct: 60, memActivePct: 48, question: { text: "Select a boot device" } },
    { id: "3", name: "Storage test machine with a long name", powerState: "poweredOff", connectionState: "connected" },
    { id: "4", name: "Archive", powerState: "poweredOff", connectionState: "disconnected" },
  ].map((vm) => ({
    numCPU: 4, memoryMB: 8192, committedBytes: Number(vm.id) * 10 * 2 ** 30, guestOS: "Fixture OS", config: { nestedHV: false, changeVersion: "fixture-v1", version: "vmx-20" },
    networks: [{ key: 4000, connected: true, startConnected: true, label: "Network adapter 1", portgroup: "VM Network", adapter: "vmxnet3", mac: "00:50:56:00:00:01", ipAddresses: ["192.0.2.1/24"] }],
    disks: [{ key: 2000, controllerKey: 1000, mode: "persistent", label: "Hard disk 1", capacityKB: 64 * 2 ** 20 }], ...vm,
  }));
  for (let i = 2; i <= 12; i++) {
    vms[0].networks.push({ label: "Network adapter " + i, portgroup: "VM Network", mac: "00:50:56:00:01:" + i });
  }
  vms[0].disks.push({ label: "Hard disk 2" });
  Object.assign(vms[0], {
    uncommittedBytes: 5 * gib, unsharedBytes: 8 * gib, quickStatsReliable: true,
    quickStats: { overallCpuUsage: 18, hostMemoryUsage: 2355, guestMemoryUsage: 327 },
  });
  vms[0].config.coresPerSocket = 2;
  const now = Date.now() + 8 * 60 * 60 * 1000;
  const makeSeries = (name, values) => ({
    name, instance: "", points: Array.from({ length: 44 }, (_, i) => ({ timeUtc: new Date(now - (44 - i) * 20000).toISOString(), value: i === 43 ? values.at(-1) : values[i % values.length] })),
  });
  const metrics = { series: [
    makeSeries("cpu.usage.average", [12, 27, 19, 31, 24, 38]),
    makeSeries("mem.active.average", [18, 19, 21, 20, 23, 24].map((n) => n * 2 ** 20)),
    makeSeries("net.bytesRx.average", [12, 22, 14, 62, 43, 32]),
    makeSeries("net.bytesTx.average", [2, 5, 8, 12, 9, 8]),
    ...["disk", "virtualDisk"].flatMap((prefix) => [
      makeSeries(prefix + ".read.average", [2000, 3000, 1200]),
      makeSeries(prefix + ".write.average", [900, 1100, 4000]),
    ]),
  ] };
  const profiles = [
    { name: "lab", url: "https://lab.example", username: "root" },
    { name: "production-environment-with-long-name", url: "https://prod.example", username: "root" },
    { name: "broken", url: "https://broken.example", username: "root" },
  ];
  const otherVMs = [{ ...vms[0], id: "1", name: "Production VM", question: null, powerState: "poweredOn" }];
  let delayLabInventory = false;
  const environmentConnections = [];
  const tickets = [];
  const patches = [];
  const powers = [];
  const errors = [];
  let mockConsole = true;
  const ask = (truth, message) => { if (!truth) throw new Error(message); };
  const captureTable = async (locator, path) => {
    // Background refresh can replace the table while Playwright waits for fonts.
    for (let attempt = 0; ; attempt++) {
      try {
        await locator.screenshot({ path, animations: "disabled" });
        return;
      } catch (error) {
        if (attempt >= 2 || !String(error).includes("Element is not attached")) throw error;
      }
    }
  };
  page.on("pageerror", (err) => errors.push(err.message));
  await page.route(fixtureOrigin + "/**", async (route) => {
    let path = new URL(route.request().url()).pathname;
    if(path==="/v1/console/capabilities")return route.fulfill({json:{available:true,state:"ready",baseURL:sdkPrefix}});
    if(path.startsWith(sdkPrefix))path="/wmks/"+path.slice(sdkPrefix.length);
    if(path.startsWith("/wmks/"))return route.fulfill({path:"web"+path}).catch(()=>route.fulfill({status:404,body:"SDK fixture absent"}));
 if(path==="/v1/tasks")return route.fulfill({json:{items:[],hasMore:false,active:0}});
    const method = route.request().method();
    const profile = route.request().headers()["x-govmc-profile"] || "lab";
    const inventory = profile === "production-environment-with-long-name" ? otherVMs : vms;
    const reply = (json, status = 200) => route.fulfill({ status, json });
    if (path === "/v1/profiles") return reply({ instance: "fixture", current: "lab", active: "lab", profiles });
    if (path === "/v1/profiles/connect") {
      const name = route.request().postDataJSON().name;
      environmentConnections.push(name);
      if (name === "production-environment-with-long-name") await new Promise((resolve) => setTimeout(resolve, 180));
      return name === "broken" ? reply({ message: "Fixture connection failed" }, 503) : reply({ name });
    }
    if (path === "/ui-preferences") return reply({ language: "zh" });
    if (path === "/v1/host") return reply({ name: profile === "lab" ? "esxi-lab-01" : "esxi-production", login: "root@192.0.2.20", uptimeSeconds: 302400, overallStatus: "gray", connectionState: "connected", numCpuCores: 16, numCpuThreads: 32, memoryMB: 65536, portgroups: [{ name: "VM Network" }, { name: "Test Network" }] });
    if (path === "/v1/datastores") return reply([{ capacity: 500 * gib, freeSpace: 180 * gib }]);
    if (path === "/v1/vms") {
      if (profile === "lab" && delayLabInventory) {
        delayLabInventory = false;
        await new Promise((resolve) => setTimeout(resolve, 600));
        return reply(inventory).catch(() => {}); // The old fetch is deliberately aborted on environment switch.
      }
      return reply(inventory);
    }
    if (path.endsWith("/metrics")) {
      const query = new URL(route.request().url()).searchParams;
      return reply({ series: query.has("end") ? [] : metrics.series.filter((s) => query.get("names").split(",").includes(s.name)) });
    }
    const match = /^\/v1\/vms\/(\d+)(?:\/(\w+))?$/.exec(path);
    if (match) {
      const vm = inventory.find((item) => item.id === match[1]);
      if (match[2] === "screenshot") {
        screenshotRequests++;
        if (screenshotRequests === 2) await new Promise((resolve) => setTimeout(resolve, 400));
        return route.fulfill({ contentType: "image/svg+xml", body: '<svg xmlns="http://www.w3.org/2000/svg" width="480" height="270"><rect width="480" height="270" fill="#21364e"/><text x="24" y="50" fill="white">Screenshot fixture</text></svg>' });
      }
      if (match[2] === "console") {
        tickets.push(vm.id);
        return reply({ ws: "/ws/console?id=" + vm.id });
      }
      if (match[2] === "power") {
        const data = route.request().postDataJSON();
        powers.push([vm.id, data, profile]);
        vm.powerState = "poweredOff";
        return reply({ status: "ok" });
      }
      if (method === "PATCH") {
        const data = route.request().postDataJSON();
        patches.push([vm.id, data]);
        Object.assign(vm, { numCPU: data.numCPU });
      }
      return reply(vm);
    }
    if (mockConsole && path === "/console.html") return route.fulfill({ contentType: "text/html", body: `<!doctype html><title>Console fixture</title><style>
      html { color-scheme: light dark; } html[data-theme=light] { color-scheme: light; }
      html[data-theme=dark] { color-scheme: dark; }
      body { margin: 0; font: 14px system-ui; color: CanvasText; background: Canvas; }
      header { display: flex; align-items: center; justify-content: space-between; padding: 8px; } main { min-height: 85vh; display: grid; place-content: center; background: #111923; color: #bac8db; }
      </style><script>
      window.operations = [];
      addEventListener("message", (event) => {
        if (event.origin !== location.origin || event.source !== parent) return;
        operations.push(event.data);
        if (event.data.type === "appearance") document.documentElement.dataset.theme = event.data.theme;
        if (event.data.type === "disconnect") parent.postMessage({ source: "govmc-console", state: "disconnected" }, location.origin);
        if (event.data.type === "connect") {
          document.querySelector("#fixture-name").textContent = event.data.name + " · 已连接";
          parent.postMessage({ source: "govmc-console", state: "connected" }, location.origin);
        }
      });
      parent.postMessage({ source: "govmc-console", state: "ready" }, location.origin);
    </script><body><header><strong id="fixture-name"></strong><button onclick="parent.postMessage({ source: 'govmc-console', state: 'disconnected' }, location.origin)">断开连接</button><button onclick="parent.postMessage({ source: 'govmc-console', state: 'reconnect' }, location.origin)">Reconnect fixture</button><button onclick="parent.postMessage({ source: 'govmc-console', state: 'disconnect-request' }, location.origin)">Request disconnect fixture</button></header><main>模拟控制台 · Console fixture</main></body>` });
    return route.fulfill({ path: "web" + (path === "/" ? "/index.html" : path) });
  });
  await page.goto(fixtureOrigin);
  sdkAvailable=await page.evaluate(()=>fetch("/wmks/wmks.min.js").then(r=>r.ok));
  await page.getByText("esxi-lab-01").waitFor();
  await page.locator("#kpis .metric-value").filter({ hasText: "24.0 GiB" }).waitFor();
  ask((await page.locator("#kpis .metric-value").allTextContents()).join("|") === "3.5 天|2|8|24.0 GiB|320.0 GiB", "wrong KPI totals or order");
  ask(await page.locator("#kpis .metric").first().getByText("已连接", { exact: true }).getAttribute("title") === "ESXi 未提供明确的健康状态", "unknown host health is not explained");
  ask(await page.locator("#kpis .metric").first().getByText("root@192.0.2.20", { exact: true }).isVisible(), "login identity is missing");
  ask(await page.locator("#kpis .metric").last().getByText("1 个 datastore", { exact: true }).isVisible(), "datastore count is missing");
  ask(await page.locator(".topbar").getByText("esxi-lab-01", { exact: true }).count() === 0, "host identity is still in the header");
  ask(await page.locator("#attention").count() === 0, "pending module was not removed");
  ask(await page.locator("#trend-title").textContent() === "指标趋势", "wrong host chart title");
  ask(/^最近采样时间: \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/.test(await page.locator("#host-sample").textContent()), "host sample label must contain only the full timestamp");
  await page.locator("#host-charts .chart-disk svg path.series").nth(1).waitFor();
  ask(await page.locator("#host-charts .chart-disk svg path.series").count() === 2, "host disk read/write did not render");
  ask(await page.locator("header nav").count() === 1, "navigation is not in the header");
  ask(await page.locator("header [data-page=console]").textContent() === "控制台", "console nav still shows a count");
  ask(await page.locator("#nav a > svg[aria-hidden=true]").count() === 3, "navigation icons are missing");
  ask(await page.locator(".topbar").evaluate((bar) => {
    const wordmark = bar.querySelector(".brand pre").getBoundingClientRect();
    const tab = bar.querySelector("#nav a[aria-current=page]").getBoundingClientRect();
    const label = bar.querySelector("#nav a[aria-current=page] span");
    const labelBounds = label.getBoundingClientRect();
    const iconBounds = label.previousElementSibling.getBoundingClientRect();
    const navFont = getComputedStyle(label);
    const trendFont = getComputedStyle(document.getElementById("trend-title"));
    const center = bounds => (bounds.top + bounds.bottom) / 2;
    return Math.abs(center(wordmark) - center(tab)) < 1 &&
      Math.abs(center(labelBounds) - center(tab)) < 1 &&
      Math.abs(center(iconBounds) - center(tab)) < 1 &&
      ["fontFamily", "fontSize", "fontWeight"].every((key) => navFont[key] === trendFont[key]);
  }), "desktop navigation is not aligned with the wordmark");
  ask(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/.test(await page.locator("#updated").textContent()), "refresh timestamp is missing its full date and time");
  ask(await page.locator(".update-status").getByText("更新时间", { exact: true }).isVisible(), "timestamp label is missing");
  ask(await page.getByRole("button", { name: "刷新", exact: true }).locator("svg[fill=currentColor]").count() === 1, "filled refresh icon is missing");
  await page.locator("#host-charts .plot").first().focus();
  await page.keyboard.press("ArrowLeft");
  ask(await page.locator("#host-charts .chart-tip").first().isVisible(), "keyboard sample tooltip is missing");
  await page.keyboard.press("Escape");
  await page.locator("#host-charts .plot").first().blur();
  for (const width of [3440, 1440, 375]) {
    for (const mode of ["light", "dark"]) {
      await page.setViewportSize({ width, height: 900 });
      await page.emulateMedia({ colorScheme: mode });
      await page.evaluate(() => window.scrollTo(0, 0));
      ask(await page.evaluate(() => document.documentElement.scrollWidth === innerWidth), "horizontal overflow at " + width);
      await page.screenshot({ path: "/tmp/govmc-web-" + width + "-" + mode + ".png", fullPage: width === 375, animations: "disabled" });
    }
  }
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.emulateMedia({ colorScheme: "light" });
  ask((await page.locator("#inventory-counts .state-chip").allTextContents()).join("|") === "共 4 台|运行中 2|已关机 2", "inventory counts are missing");
  await page.locator('[data-page="console"]').click();
  const emptyStage = page.locator("#session-stage");
  await emptyStage.waitFor({state:"visible"});
  ask((await emptyStage.boundingBox()).height > 650, "empty console does not reserve the desktop viewport");
  ask(await emptyStage.locator(".console-empty-icon[aria-hidden=true][focusable=false]").count() === 1, "empty console icon is missing or exposed to assistive technology");
  ask(await emptyStage.locator(".console-empty").evaluate(node => {
    const icon = node.querySelector("svg").getBoundingClientRect();
    const caption = node.querySelector("span").getBoundingClientRect();
    return icon.width === 44 && icon.height === 44 &&
      Math.abs(caption.top - icon.bottom - 12) < 1 &&
      Math.abs(icon.x + icon.width / 2 - caption.x - caption.width / 2) < 1;
  }), "empty console icon and caption are not centered with the intended spacing");
  ask(await page.locator("#console-machines .console-running").count() === 2, "running status is missing");
  await page.locator('[data-page="overview"]').click();
  const table = page.getByRole("table", { name: "虚拟机", exact: true });
  await page.waitForFunction(() => {
    const node=document.querySelector(".vm-table"),row=node?.tBodies[0]?.rows[0];
    if(!row)return false;
    const sizes=[row.querySelector(".table-vm-name"),row.querySelector(".meter-value"),row.querySelector(".rx"),row.querySelector(".col-numCPU")];
    return getComputedStyle(node).tableLayout==="fixed"&&sizes.every(element=>element&&getComputedStyle(element).fontSize==="12px")&&node.getBoundingClientRect().width<=node.parentElement.clientWidth;
  });
  await captureTable(table, "/tmp/govmc-vm-table.png");
  ask(await table.getByRole("row").count() === 5, "overview does not show the complete VM table");
  ask(await table.getByText("31.5%", { exact: true }).isVisible(), "memory metric is missing");
  ask(await table.getByText("Fixture OS", { exact: true }).count() === 3, "operating system column is missing");
  ask(await table.getByText("10.0 GiB / 64.0 GiB+", { exact: true }).count() === 1, "disk used/configured ratio is missing");
  ask(await table.getByText("Microsoft Windows 11 (64 位)", { exact: true }).count() === 1, "localized operating system fallback is missing");
  ask(await table.getByText("VBS 未启用", { exact: true }).count() === 1, "VBS state is missing");
  ask(await table.locator("td.col-networks").getByText("00:50:56:00:00:01 +11", { exact: true }).isVisible(), "MAC and additional NIC count are missing");
  await table.locator("th.col-disks button").click();
  ask((await table.locator("tbody tr").first().textContent()).includes("Archive"), "disk column is not sorted by used space");
  await table.locator("th.col-cpuPct button").click();
  ask((await table.locator("tbody tr").first().textContent()).includes("DB Ubuntu"), "CPU sort did not use the sampled values");
  async function filterVM(id, label) {
    const picker = page.locator("#" + id);
    await picker.locator("summary").click();
    await picker.getByRole("button", { name: label, exact: true }).click();
  }
  const filterGeometry = () => page.locator(".inventory-heading .filters").evaluate((filters) =>
    [...filters.querySelectorAll("input, summary")].map((control) => {
      const { x, width } = control.getBoundingClientRect();
      return [x, width];
    }));
  const defaultGeometry = await filterGeometry();
  await filterVM("os-filter", "Microsoft Windows 11 (64 位)");
  ask(JSON.stringify(await filterGeometry()) === JSON.stringify(defaultGeometry), "long OS name moved inventory controls");
  await filterVM("os-filter", "Fixture OS");
  ask(await table.locator("tbody tr").count() === 3, "OS filter did not apply");
  await filterVM("power-filter", "运行中");
  ask(JSON.stringify(await filterGeometry()) === JSON.stringify(defaultGeometry), "power filter moved inventory controls");
  ask(await table.locator("tbody tr").count() === 1, "OS and power filters did not combine");
  await filterVM("os-filter", "全部操作系统");
  ask(await table.locator("tbody tr").count() === 2, "power-state filter did not apply");
  await filterVM("power-filter", "全部状态");
  const poweredRow = table.locator(":scope > tbody > tr").filter({ has: page.getByRole("button", { name: /Build Windows/ }) });
  const before = (await poweredRow.boundingBox()).height;
  await poweredRow.getByRole("button", { name: /电源操作/ }).click();
  await page.locator(".power-items:popover-open").getByRole("button", { name: "强制关闭电源" }).waitFor();
  ask(Math.abs((await poweredRow.boundingBox()).height - before) < 1, "power menu changed VM row height");
  await page.keyboard.press("Escape");
  await poweredRow.getByRole("button", { name: "编辑配置" }).click();
  await page.getByRole("dialog").waitFor();
  ask(await page.getByLabel("vCPU", { exact: true }).count() === 0, "vCPU is editable without hot-add");
  ask(await page.getByText("当前虚拟机正在运行，且未启用可用的热添加。请关机后编辑配置。", { exact: true }).isVisible(), "read-only reason is missing");
  ask(await page.getByRole("button", { name: "预览变更" }).isDisabled(), "read-only dialog allows saving");
  await page.getByRole("button", { name: "取消" }).click();
  const hotRow = table.locator(":scope > tbody > tr").filter({ has: page.getByRole("button", { name: /DB Ubuntu/ }) });
  await hotRow.getByRole("button", { name: "编辑配置" }).click();
  await page.getByRole("dialog").waitFor();
  ask(await page.getByLabel("vCPU", { exact: true }).isEnabled(), "vCPU hot-add is not editable");
  ask(await page.getByLabel("内存 (MiB)", { exact: true }).count() === 0, "memory is editable without hot-add");
  await page.getByLabel("vCPU", { exact: true }).fill("5");
  const diskSettings = page.locator(".config-group").filter({ has: page.locator("summary strong").getByText("磁盘", { exact: true }) });
  await diskSettings.locator("summary").click();
  await diskSettings.locator(".config-device-list").getByLabel("新容量 (MiB)", { exact: true }).fill("66560");
  await diskSettings.locator(".config-new-device").getByLabel("新容量 (MiB)", { exact: true }).fill("2048");
  const nicSettings = page.locator(".config-group").filter({ has: page.locator("summary strong").getByText("网卡", { exact: true }) });
  await nicSettings.locator("summary").click();
  await nicSettings.locator(".config-device-list").getByRole("combobox", { name:"端口组", exact:true }).click();
  await page.getByRole("option", {name:"Test Network",exact:true}).click();
  await nicSettings.locator(".config-device-list").getByLabel("当前连接", { exact: true }).uncheck();
  await nicSettings.locator(".config-new-device").getByRole("combobox", { name:"端口组", exact:true }).click();
  await page.getByRole("option", {name:"VM Network",exact:true}).click();
  await page.getByRole("button", { name: "预览变更" }).click();
  await page.getByRole("button", { name: "应用更改" }).click();
  await page.waitForFunction(() => !document.getElementById("action-dialog").open);
  ask(patches.length === 1 && patches[0][0] === "2" && patches[0][1].numCPU === 5 && patches[0][1].requireHotAdd && !patches[0][1].requirePoweredOff, "online edit did not require hot-add validation");
  const devicePatch = patches[0][1];
  ask(devicePatch.expandDisks?.[0]?.capacityMiB === 66560 && devicePatch.addDiskMiB === 2048 &&
    devicePatch.editNICs?.[0]?.portgroup === "Test Network" && devicePatch.editNICs[0].connected === false &&
    devicePatch.addNIC?.portgroup === "VM Network", "online disk and NIC changes were not submitted together");
  patches.length = 0;
  await page.locator("#vm-list").getByRole("button", { name: /Storage test/ }).click();
  await page.locator(".expanded-row .chart-cpu .series").waitFor();
  ask(await page.locator(".expanded-row").getByRole("table", { name: "网卡" }).getByText("00:50:56:00:00:01").isVisible(), "expanded NIC table is missing");
  ask(await page.locator(".expanded-row .chart-disk svg path.series").count() === 2, "VM disk read/write did not render");
  ask(await page.locator(".expanded-details > .vm-preview-card:first-child[aria-label=当前画面]").count() === 1, "screen preview is not the first card");
  ask(await page.locator(".vm-device-grid > :nth-child(1)").getByRole("table", { name: "网卡" }).count() === 1, "NIC card is missing");
  ask(await page.locator(".vm-device-grid > :nth-child(2)").getByRole("table", { name: "磁盘" }).count() === 1, "disk card is missing");
  const chartOrder = (section) => section.locator(".charts figure h3").allTextContents();
  ask(JSON.stringify(await chartOrder(page.locator(".expanded-row"))) === JSON.stringify(await page.locator("#host-charts figure h3").allTextContents()), "VM charts are not in the host chart order");
  ask(await page.locator(".expanded-details").getByText("主机名称", { exact: true }).count() === 1, "hostname label was not changed");
  ask(await page.locator("#vms").count() === 0, "VM detail still uses a separate page");
  await page.locator("#vm-list tbody tr").filter({ has: page.getByRole("button", { name: /Storage test/ }) }).getByRole("button", { name: "编辑配置" }).click();
  await page.getByRole("dialog").waitFor();
  ask(await page.locator("dialog.config-drawer").evaluate((node) => {
    const box = node.getBoundingClientRect();
    const body = node.querySelector("#action-fields");
    return box.right === innerWidth && box.width === 840 && body.scrollWidth === body.clientWidth;
  }), "configuration drawer is too narrow or overflows horizontally");
  ask(await page.locator(".config-group[open] > summary strong").allTextContents().then((names) =>
    names.includes("CPU") && names.includes("内存")), "CPU or memory module was collapsed by default");
  const diskModule = page.locator(".config-group").filter({ has: page.getByText("磁盘", { exact: true }) });
  await diskModule.locator("summary").click();
  ask(await diskModule.getByRole("heading", { name: "Hard disk 1" }).isVisible(), "disk device card is missing");
  ask(await diskModule.locator("table").count() === 0, "drawer still uses a disk table");
  const nicModule = page.locator(".config-group").filter({ has: page.getByText("网卡", { exact: true }) });
  await nicModule.locator("summary").click();
  ask(await nicModule.getByRole("heading", { name: "Network adapter 1" }).isVisible(), "NIC device card is missing");
  await nicModule.locator("summary").click();
  await diskModule.locator("summary").click();
  await page.getByRole("button", { name: "虚拟机选项" }).click();
  ask(await page.locator(".config-tab-segments button[aria-pressed=true]").textContent() === "虚拟机选项", "segment selection is not reflected");
  ask(await page.getByLabel("虚拟机名称", { exact: true }).isVisible(), "VM options tab did not open");
  await page.getByRole("button", { name: "虚拟硬件" }).click();
  await page.getByLabel("每插槽核心数（0 为自动）").fill("2");
  await page.getByLabel("vCPU", { exact: true }).fill("6");
  await page.locator(".config-group").filter({ has: page.getByLabel("vCPU", { exact: true }) }).locator("summary").click();
  await page.getByRole("button", { name: "预览变更" }).click();
  ask(await page.getByText("4 → 6").isVisible(), "missing config diff");
  await page.getByRole("button", { name: "应用更改" }).click();
  await page.waitForFunction(() => !document.getElementById("action-dialog").open);
  ask(patches.length === 1 && patches[0][0] === "3" && patches[0][1].numCPU === 6 && patches[0][1].coresPerSocket === 2 && patches[0][1].changeVersion === "fixture-v1" && patches[0][1].requirePoweredOff, "wrong config mutation");
  const runningRow = table.locator("tbody tr").filter({ has: page.getByRole("button", { name: /Build Windows/ }) });
  await runningRow.getByRole("button", { name: /Build Windows/ }).click();
  await page.locator(".expanded-row .chart-cpu .series").waitFor();
  const frame = page.locator(".preview-image");
  await frame.locator("img").waitFor();
  const height = (await frame.boundingBox()).height;
  const refreshPreview = page.getByRole("button", { name: "刷新截图" });
  await page.mouse.move(0, 0);
  ask(await refreshPreview.evaluate((node) => getComputedStyle(node).opacity === "0"), "refresh icon is visible without hover or focus");
  await refreshPreview.focus();
  ask(await refreshPreview.evaluate((node) => getComputedStyle(node).opacity === "1"), "keyboard focus did not reveal refresh icon");
  await refreshPreview.press("Enter");
  await frame.getByRole("status").waitFor();
  ask(await frame.locator("img").isVisible(), "refresh replaced the previous image");
  ask(Math.abs((await frame.boundingBox()).height - height) < 1, "refresh changed the preview height");
  await frame.getByRole("status").waitFor({ state: "hidden" });
  const tools = page.locator(".tools-summary");
  ask(await tools.locator(".tools-version").textContent() === "(open-vm-tools 13.0.10 build 25056151)", "human-readable Tools version is missing");
  ask(await tools.locator(".state-chip").count() === 0, "Tools version still uses a badge");
  ask(await page.locator(".expanded-details").getByText("192.0.2.1/24", { exact: true }).isVisible(), "NIC IP/prefix is missing");
  const layout = await page.locator(".expanded-details").evaluate((grid) => [...grid.querySelectorAll(":scope > section, .vm-device-card")].map((card) => {
    const { top, height } = card.getBoundingClientRect();
    return { top, height };
  }));
  ask(layout[0].height === layout[1].height && layout[2].height === layout[3].height && layout[2].top > layout[0].top, "detail card rows are not aligned");
  ask(await page.getByRole("region", { name: "网卡", exact: true }).evaluate((node) => node.scrollHeight > node.clientHeight), "multiple NICs do not scroll inside their card");
  ask(await page.locator(".facts-compact > div").filter({ has: page.getByText("VBS", { exact: true }) }).locator("dd").textContent() === "未启用", "VBS is not a separate overview field");
  const overviewCard = page.locator(".vm-overview-card");
  const previewCard = page.locator(".vm-preview-card");
  const overviewHeight = (await overviewCard.boundingBox()).height;
  ask((await previewCard.boundingBox()).width === 600, "screenshot card does not have its fixed desktop width");
  ask(await overviewCard.locator(".vm-overview-columns").evaluate((node) =>
    getComputedStyle(node).gridTemplateColumns.split(" ").length === 3), "overview does not use three columns");
  const moreIPs = overviewCard.getByRole("button", { name: "查看全部 IP 地址" });
  ask(await moreIPs.textContent() === "+9", "primary IP did not collapse extra addresses");
  await moreIPs.click();
  const ipPanel = overviewCard.getByRole("region", { name: "IP 地址", exact: true });
  ask(await ipPanel.getByRole("listitem").count() === 10 && await ipPanel.isVisible(), "expanded IP list is incomplete");
  ask((await overviewCard.boundingBox()).height === overviewHeight, "additional IPs expanded the overview card");
  await page.keyboard.press("Escape");
  ask(await overviewCard.getByText("327 MiB", { exact: true }).isVisible(), "active guest memory is missing");
  ask(await overviewCard.getByText("5.0 GiB", { exact: true }).isVisible(), "potential storage growth is missing");
  await page.setViewportSize({ width: 375, height: 900 });
  ask(await overviewCard.locator(".vm-overview-columns").evaluate((node) =>
    getComputedStyle(node).gridTemplateColumns.split(" ").length === 1), "overview does not stack on mobile");
  await page.setViewportSize({ width: 1440, height: 900 });
  ask(await runningRow.locator(".guest-os").evaluate((node) => {
    const [title, chip] = [...node.children].map((child) => child.getBoundingClientRect());
    return Math.abs(title.top + title.height / 2 - chip.top - chip.height / 2) < 1;
  }), "VBS badge wrapped below the operating system");
  await captureTable(table, "/tmp/govmc-vm-expanded.png");
  ask(await table.locator(".expanded-row").getByRole("button", { name: "控制台", exact: true }).count() === 0, "console button still appears in the expanded row");
  await runningRow.getByRole("button", { name: /Build Windows/ }).click();
  ask(await table.locator(".expanded-row").count() === 0, "VM details did not collapse");
  await runningRow.getByRole("button", { name: "控制台", exact: true }).click();
  await page.locator("#connected-sessions .connected-row[data-vm=\"1\"]").waitFor();
  ask(await page.locator("#console-machines button[data-vm=\"1\"]").getAttribute("aria-pressed") === "true", "active machine is not selected");
  ask(await page.locator("#console-machines li").count() === 2, "console inventory contains non-running VMs");
  ask(await page.locator("#console-machines [data-vm='2'] .state-chip").count() === 0, "unopened VM shows connection status");
  await page.locator("#console-machines").getByRole("button", { name: /DB Ubuntu/ }).click();
  await page.locator("#connected-sessions .connected-row[data-vm=\"2\"]").waitFor();
  for (let i = 0; i < 10; i++) vms.push({ id: String(20 + i), name: "Fixture extra " + i, powerState: "poweredOn" });
  await page.locator("#refresh").click();
  await page.locator("#console-pages").getByRole("button", { name: "下一页" }).click();
  ask(await page.locator("#console-machines li").count() === 2, "console page size was not enforced");
  await page.locator("#console-pages").getByRole("button", { name: "上一页" }).click();
  ask(await page.locator("#console-machines li").count() === 10, "console first page does not show 10 VMs");
  const pickLanguage = async (language) => {
    await page.locator("#language summary").click();
    await page.locator('#language [data-language="' + language + '"]').click();
  };
  const pickTheme = (theme) => page.locator('#theme [data-theme-option="' + theme + '"]').click();
  await pickLanguage("en");
  await page.getByRole("heading", { name: "Connected sessions", exact: true }).waitFor();
  await pickTheme("dark");
  ask(await page.locator("#theme [data-theme-option=dark]").getAttribute("aria-pressed") === "true", "theme selection is not reflected");
  await page.waitForFunction(() => [...document.querySelectorAll("iframe")].every((frame) =>
    frame.contentWindow.operations.some((op) => op.type === "appearance" && op.language === "en" && op.theme === "dark")));
  await pickLanguage("zh");
  await pickTheme("system");
  await page.locator("#connected-sessions .connected-row[data-vm=\"1\"]").click();
  const ops = await page.evaluate(() => [...document.querySelectorAll("iframe")].map((frame) => frame.contentWindow.operations));
  ask(ops[0].some((op) => op.type === "active" && !op.active) && ops[1].some((op) => op.type === "connect" && op.id === "2"), "frames did not receive isolated operations");
  ask(tickets.join(",") === "1,2", "tab switches issued unexpected tickets");
  await page.emulateMedia({ reducedMotion: "reduce" });
  ask(await page.locator("#console .console-layout").evaluate((element) => getComputedStyle(element).animationName) === "none",
    "reduced-motion preference was not honored");
  await page.screenshot({ path: "/tmp/govmc-web-sessions.png", fullPage: true });
  await pickTheme("dark");
  await page.screenshot({ path: "/tmp/govmc-web-sessions-dark.png", fullPage: true });
  await pickTheme("system");
  const disconnectControl = page.locator("#connected-sessions").getByRole("button", { name: "断开与 DB Ubuntu 的连接", exact: true });
  ask(await disconnectControl.evaluate((button) => {
    const badge = button.parentElement.querySelector(".state-chip");
    return button.offsetHeight === badge.offsetHeight && getComputedStyle(button).color === getComputedStyle(document.querySelector("#disconnect-confirm .disconnect-commit")).color;
  }), "disconnect action and state badge are not aligned or the button is not red");
  await disconnectControl.click();
  const disconnectConfirm = page.getByRole("dialog", { name: "断开连接", exact: true });
  ask(await disconnectConfirm.getByText("DB Ubuntu", { exact: true }).isVisible(), "disconnect target is missing");
  await disconnectConfirm.getByRole("button", { name: "取消", exact: true }).click();
  ask(await page.locator("#connected-sessions .connected-row[data-vm='2']").isVisible(), "cancel disconnected the session");
  await disconnectControl.click();
  await disconnectConfirm.getByRole("button", { name: "确认断开", exact: true }).click();
  await page.locator("#console-machines [data-vm='2']").getByText("已断开").waitFor();
  ask(await page.locator("#connected-sessions .connected-row[data-vm=\"2\"]").count() === 0, "disconnected session is still in the connected list");
  ask(await page.locator("#console-machines [data-vm='1']").getAttribute("aria-pressed") === "true", "disconnecting another VM changed the active machine");
  const beforeReconnect = tickets.filter(id => id === "2").length;
  await page.locator("#console-machines [data-vm='2']").click();
  await page.waitForFunction(() => document.querySelector('#connected-sessions .connected-row[data-vm="2"]'));
  ask(tickets.filter(id => id === "2").length === beforeReconnect+1,"reconnecting did not request a fresh ticket");

  await page.locator("#console-machines").getByRole("button", { name: /DB Ubuntu/ }).click();
  await page.locator("#connected-sessions .connected-row[data-vm=\"2\"]").waitFor();
  ask(tickets.join(",") === "1,2,2", "reconnecting did not request a new ticket");
  const activeConsole = page.frameLocator("#session-stage iframe:not([hidden])");
  await activeConsole.getByRole("button", {name:"断开连接",exact:true}).click();
  await page.locator("#console-machines [data-vm='2']").getByText("已断开").waitFor();
  await activeConsole.getByRole("button", {name:"Reconnect fixture",exact:true}).click();
  await page.locator("#connected-sessions .connected-row[data-vm='2']").waitFor();
  ask(tickets.join(",")==="1,2,2,2","child reconnect did not obtain a fresh ticket");
  await activeConsole.getByRole("button",{name:"Request disconnect fixture",exact:true}).click();
  await disconnectConfirm.waitFor();
  ask(await disconnectConfirm.evaluate(dialog=>dialog.matches(":modal")),"toolbar disconnect confirmation is not a top-level modal");
  await page.screenshot({path:"/tmp/govmc-console-disconnect-fullpage.png",fullPage:false});
  ask(await disconnectConfirm.getByText("DB Ubuntu",{exact:true}).isVisible(),"toolbar confirmation targets wrong VM");
  await disconnectConfirm.getByRole("button",{name:"取消",exact:true}).click();
  ask(await page.locator("#connected-sessions .connected-row[data-vm='2']").isVisible(),"toolbar cancellation disconnected the VM");


  await page.locator("header nav").getByRole("link", { name: "总览" }).click();
  await page.locator('#leave-console-confirm button[value="leave"]').click();
  await page.locator("#vm-list tbody > tr").filter({ has: page.getByRole("button", { name: /Build Windows/ }) }).getByRole("button", { name: /电源操作/ }).click();
  await page.getByRole("button", { name: "强制关闭电源" }).click();
  ask(await page.locator("#action-target").textContent() === "esxi-lab-01 / Build Windows (1)", "wrong power confirmation target");
  await page.getByRole("button", { name: "确认" }).click();
  await page.waitForFunction(() => !document.getElementById("action-dialog").open);
  ask(powers.length === 1 && powers[0][0] === "1" && powers[0][1].confirm, "unexpected power action");
  for (let i = 0; i < 15; i++) vms.push({ id: String(50 + i), name: "Paged VM " + i, powerState: "poweredOff", guestOS: "Fixture OS" });
  await page.locator("#refresh").click();
  const inventoryRows = table.locator(":scope > tbody > tr:not(.expanded-row)");
  await page.locator("#inventory-pages").waitFor();
  ask(await inventoryRows.count() === 20, "inventory page exceeded 20 VMs");
  await page.locator("#inventory-next").click();
  ask(await inventoryRows.count() === 9, "remaining VMs were not shown on page 2");
  await page.locator("#search").fill("Paged VM");
  ask(await inventoryRows.count() === 15 && !(await page.locator("#inventory-pages").isVisible()), "search did not reset inventory pagination");
  // Environment selection is tab-local; IDs may overlap across targets.
  const environment = page.locator("#environment");
  const summary = environment.locator("summary");
  const pickerWidth = (await summary.boundingBox()).width;
  ask(await summary.locator(".environment-initials").textContent() === "LA", "environment trigger does not show exactly two characters");
  ask(await environment.evaluate((node) => node.nextElementSibling.id === "language"), "environment control is not immediately before language");
  ask(pickerWidth === (await page.locator("#language summary").boundingBox()).width, "environment trigger does not match toolbar icons");
  ask(await environment.locator(".environment-dot").getAttribute("data-state") === "connected", "environment connection badge is missing");
  const switchDialog = page.getByRole("dialog", { name: "切换环境", exact: true });
  const confirmEnvironment = () => switchDialog.getByRole("button", { name: "确认切换", exact: true }).click();
  await summary.click();
  await environment.locator('[data-profile="production-environment-with-long-name"]').click();
  await switchDialog.waitFor();
  ask(environmentConnections.length === 0, "target connection began before confirmation");
  await switchDialog.getByRole("button", { name: "取消", exact: true }).click();
  ask(environmentConnections.length === 0 && await summary.textContent() === "LA", "cancel changed the selected environment");
  delayLabInventory = true;
  await page.locator("#refresh").click();
  await summary.click();
  await environment.locator('[data-profile="production-environment-with-long-name"]').click();
  await confirmEnvironment();
  await page.getByText("esxi-production", { exact: true }).waitFor();
  await page.getByRole("button", { name: /Production VM/ }).waitFor();
  await page.waitForTimeout(750);
  ask(await table.locator(":scope > tbody > tr:not(.expanded-row)").count() === 1, "old environment inventory replaced the new one");
  ask(await page.locator("#connected-sessions .connected-row").count() === 0 && await page.locator("#session-stage iframe").count() === 0, "old console sessions survived an environment switch");
  ask(await page.locator("#search").inputValue() === "", "old environment search leaked to the new environment");
  ask((await summary.boundingBox()).width === pickerWidth, "profile selection changed the picker width");
  await page.reload();
  await page.getByText("esxi-production", { exact: true }).waitFor();
  ask(await summary.textContent() === "PR", "tab profile did not survive reload");
  const productionRow = table.locator("tbody tr").filter({ has: page.getByRole("button", { name: /Production VM/ }) });
  await productionRow.getByRole("button", { name: /电源操作/ }).click();
  await page.getByRole("button", { name: "强制关闭电源" }).click();
  ask(await page.locator("#action-target").textContent() === "esxi-production / Production VM (1)", "new action still names the previous target");
  await page.getByRole("button", { name: "确认", exact: true }).click();
  await page.waitForFunction(() => !document.getElementById("action-dialog").open);
  ask(powers.at(-1)[2] === "production-environment-with-long-name", "write used the previous environment despite an overlapping VM ID");
  await summary.click();
  await environment.locator('[data-profile="broken"]').click();
  await confirmEnvironment();
  await page.getByText(/Fixture connection failed/).waitFor();
  ask(await summary.textContent() === "PR" && await page.getByText("esxi-production", { exact: true }).isVisible(), "failed switch discarded the working environment");
  await summary.click();
  await environment.locator('[data-profile="lab"]').click();
  await confirmEnvironment();
  await page.getByText("esxi-lab-01", { exact: true }).waitFor();

  if(!sdkAvailable)return "Core workspace and mock console passed; authorized SDK fixture not installed";
  mockConsole = false;
  await page.goto(fixtureOrigin + "/console.html");
  await page.locator("#wmksContainer canvas").waitFor({ state: "attached" });
  await page.evaluate(() => window.postMessage({
    source: "govmc-workbench", type: "appearance", language: "en", theme: "dark",
  }, location.origin));
  await page.getByRole("button", { name: "Disconnect", exact: true }).waitFor();
  ask(await page.locator("main").evaluate((stage) => {
    stage.classList.add("is-controlling");
    return getComputedStyle(stage).borderTopWidth === "0px";
  }), "console draws a border when control is captured");
  ask(await page.locator(".console-bar .hud-metric").count() === 3, "console HUD metrics are missing");
  ask(await page.locator(".console-bar button").count() === 2, "console toolbar has extra actions");
  ask(await page.locator(".console-bar").evaluate(bar => {
    const styles=[...bar.querySelectorAll("#status,.hud-metric,button")].map(node=>getComputedStyle(node));
    return styles.every(style=>style.fontFamily===styles[0].fontFamily && style.fontSize===styles[0].fontSize && style.lineHeight===styles[0].lineHeight);
  }),"console toolbar typography is inconsistent");

  ask(await page.locator(".console-bar").evaluate((bar) => {
    const boxes = [...bar.querySelectorAll("#status,.hud-metric,button")].map((node) => node.getBoundingClientRect());
    return boxes.every((box) => Math.abs(box.height - boxes[0].height) < 1 && Math.abs(box.top - boxes[0].top) < 1);
  }), "console labels and buttons are not aligned in one row");
  ask(await page.evaluate(() => document.documentElement.dataset.theme) === "dark", "WebMKS child did not apply theme");
  await page.evaluate(() => {
    window.consoleTicketURLs = [];
    window.originalWMKS = $.fn.wmks;
    $.fn.wmks = function(command, ...args) {
      if (command === "connect") { window.consoleTicketURLs.push(args[0]); return this; }
      return window.originalWMKS.call(this, command, ...args);
    };
    window.postMessage({ source: "govmc-workbench", type: "connect", id: "1", name: "Fixture", ws: "/ws/console?id=abcdef0123456789&profile=lab" }, location.origin);
  });
  await page.waitForFunction(() => window.consoleTicketURLs.length === 1);
  ask(await page.evaluate(() => window.consoleTicketURLs[0].endsWith("&profile=lab")), "profile-scoped console ticket was rejected");
  await page.evaluate(() => {
    window.postMessage({ source: "govmc-workbench", type: "connect", id: "1", ws: "https://evil.test/ws/console?id=abcdef" }, location.origin);
    window.postMessage({ source: "govmc-workbench", type: "connect", id: "1", ws: "/ws/console?id=abcdef&unexpected=true" }, location.origin);
  });
  await page.waitForTimeout(50);
  ask(await page.evaluate(() => window.consoleTicketURLs.length) === 1, "console accepted an untrusted ticket URL");
  await page.evaluate(() => { $.fn.wmks = window.originalWMKS; });
  await page.evaluate(() => {
    const box = $("#wmksContainer");
    const decoder = box.data("wmks-wmks")._vncDecoder;
    decoder.useVMWKeyEvent = true;
    decoder.allowVMWKeyEvent2UnicodeAndRaw = false;
    window.consoleInputFixture = { scans: [] };
    decoder.onKeyVScan = (code, down) => window.consoleInputFixture.scans.push([code, Boolean(down)]);
    window.postMessage({ source: "govmc-workbench", type: "active", active: true }, location.origin);
    box.trigger("wmksconnecting");
    box.trigger("wmksconnected");
  });
  await page.getByRole("button", { name: "Ctrl+Alt+Del", exact: true }).click();
  ask(await page.evaluate(() => JSON.stringify(window.consoleInputFixture.scans.slice(0, 3))) ===
    JSON.stringify([[0x1d, true], [0x38, true], [0x153, true]]), "Ctrl+Alt+Del did not send scan codes");
  ask(await page.evaluate(() => window.consoleInputFixture.scans.filter(([, down]) => !down).length) === 3,
    "Ctrl+Alt+Del did not release all three keys");
  await page.getByRole("button", { name: "Disconnect", exact: true }).click();
  await page.getByRole("button", { name: "Confirm disconnect", exact: true }).click();
  await page.locator("#status[data-state=disconnected]").waitFor();
  await page.evaluate(() => {
    $("#wmksContainer").trigger("wmksresolutionchanged").trigger("wmksframeBufferInited").trigger("wmksconnected");
  });
  ask(await page.locator("#status").getAttribute("data-state") === "disconnected", "late display events restored a closed connection");
  ask(await page.locator("#wmksContainer canvas").evaluate((canvas) => getComputedStyle(canvas).visibility === "hidden"), "disconnected console still shows the old frame");
  await page.getByRole("heading", { name: "Session disconnected", exact: true }).waitFor();
  await page.getByRole("button", { name: "Reconnect", exact: true }).waitFor({state:"visible"});
  await page.screenshot({path:"/tmp/govmc-console-disconnected-en.png",fullPage:false});
  await page.evaluate(() => window.postMessage({source:"govmc-workbench",type:"appearance",language:"zh",theme:"light"},location.origin));
  await page.getByRole("heading", {name:"会话已断开",exact:true}).waitFor();
  await page.setViewportSize({width:390,height:844});
  ask(await page.evaluate(() => document.documentElement.scrollWidth<=innerWidth),"disconnected console overflows narrow window");
  await page.screenshot({path:"/tmp/govmc-console-disconnected-zh-mobile.png",fullPage:false});
  await page.getByRole("button",{name:"重新连接",exact:true}).click();
  await page.getByRole("heading",{name:"正在连接控制台",exact:true}).waitFor();
  ask(await page.getByRole("button",{name:"重新连接",exact:true}).isHidden(),"reconnect permits duplicate submission");
  ask(await page.locator("#clipboard, #clipboardDialog").count() === 0, "clipboard UI is still present");
  ask(errors.length === 0, errors.join("\n"));
  return "KPI, inline hardware and chart layout, paged console, session switching and reconnect, locale/theme, environment isolation, and power confirmation passed";
}
