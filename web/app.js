(async () => {
  "use strict";
  const { t, init, setLanguage, getLanguage } = window.govmcI18n;
  await init();
  const $ = (id) => document.getElementById(id);
  const state = {
    environment: "", environments: [], environmentInstance: "", environmentEpoch: 0, environmentReady: false, environmentConnection: "connecting", switchingEnvironment: false, snapshotDirty:false, snapshotPending:false, mutationEpoch:0,
    page: null, vms: null, host: null, stores: null, metrics: null,
    selected: null, vm: null, vmMetrics: null,
    active: null, sessions: new Map(), sessionStates: new Map(), consolePage: 1, inventoryPage: 1, token: (()=>{try{return sessionStorage.getItem("govmc-api-token")||""}catch{return ""}})(), refreshing: false,
    busy: new Set(), detailRequest: 0, hostRequest: 0, sort: { key: "name", direction: 1 },
  };
  const powerLabels = { get poweredOn() { return t("运行中"); }, get poweredOff() { return t("已关机"); }, get suspended() { return t("已挂起"); } };
  const operations = { get on() { return t("开机"); }, get off() { return t("硬关机"); }, get reset() { return t("硬重置"); }, get suspend() { return t("挂起"); } };
  const sessionLabels = { get connecting() { return t("正在连接"); }, get connected() { return t("已连接"); }, get disconnected() { return t("已断开"); }, get error() { return t("连接失败"); } };
  const noData = "—";
  const count = { format: (value) => new Intl.NumberFormat(document.documentElement.lang).format(value) };
  const time = { format: (value) => formatTime(value, { hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23" }) };
  const stamp = (value) => value && Number.isFinite(+new Date(value)) ? time.format(new Date(value)) : noData;
  const has = (v) => typeof v === "number" && Number.isFinite(v);
  const units = (v, suffix, digits = 1) => has(v) ? v.toFixed(digits) + suffix : noData;
  const gib = (v) => has(v) ? units(v / 2 ** 30, " GiB") : noData;
  const endpoint = (id, suffix = "") => "/v1/vms/" + encodeURIComponent(id) + suffix;

  function el(tag, text, className) {
    const node = document.createElement(tag);
    if (text != null) node.textContent = text;
    if (className) node.className = className;
    return node;
  }

  function button(label, onClick, className, disabled = false) {
    const node = el("button", label, className);
    node.type = "button";
    node.disabled = disabled;
    node.addEventListener("click", onClick);
    return node;
  }

  function empty(text) { return el("p", text, "empty"); }

  function notice(message) {
    $("notice").textContent = message;
    $("notice").hidden = !message;
  }

  let environmentController = new AbortController();
  async function api(path, options = {}, format = "json") {
    const epoch = state.environmentEpoch,profile=state.environment,instance=state.environmentInstance;
    const headers = new Headers(options.headers);
    if (state.environment) headers.set("X-Govmc-Profile", state.environment);
    if (state.token) headers.set("Authorization", "Bearer " + state.token);
    if (options.body) headers.set("Content-Type", "application/json");
    const response = await fetch(path, { ...options, headers, cache: "no-store", signal: AbortSignal.any([environmentController.signal, options.signal || AbortSignal.timeout(35000)]) });
    if (response.status === 401 && !$("token-dialog").open) $("token-dialog").showModal();
    if (!response.ok) {
      const err = await response.json().catch(() => ({}));
      const failure = new Error(err.message || err.error || t("请求失败 (") + response.status + ")");
      failure.status = response.status;
      throw failure;
    }
    if(options.method&&!["GET","HEAD"].includes(options.method)&&!path.startsWith("/v1/profiles")&&!path.endsWith("/console")){
      let preview=false;try{preview=JSON.parse(options.body||"{}").dryRun===true;}catch{}
      if(!preview){await window.govmcCache.invalidate(instance,profile);if(epoch===state.environmentEpoch){state.snapshotDirty=true;state.mutationEpoch++;}}
    }
    const value = await (format === "blob" ? response.blob() : response.json());
    if (epoch !== state.environmentEpoch) throw new DOMException("Environment changed", "AbortError");
    return value;
  }

  function saveSnapshot(){
    if(!state.environmentReady||state.snapshotDirty||state.snapshotPending||(!state.host&&!state.vms))return;
    window.govmcCache.write(state.environmentInstance,state.environment,"workbench:"+window.govmcCache.tab,{
      host:state.host,vms:state.vms,stores:state.stores,metrics:state.metrics,hostRange:state.hostRange,updated:state.updated,
      ui:{search:$("search").value,os:state["os-filter"],power:state["power-filter"],page:state.inventoryPage,sort:state.sort,range:$("host-range").value}
    });
  }
  async function restoreSnapshot(){
    const profile=state.environment,instance=state.environmentInstance;
    const cached=(await window.govmcCache.read(instance,profile,"workbench:"+window.govmcCache.tab))?.value;
    if(!cached||profile!==state.environment||instance!==state.environmentInstance)return;
    state.snapshotPending=true;
    for(const key of ["host","vms","stores","metrics","hostRange"])state[key]=cached[key];
    state.updated=cached.updated?new Date(cached.updated):null;
    const ui=cached.ui||{};$("search").value=ui.search||"";state["os-filter"]=ui.os||"";state["power-filter"]=ui.power||"";state.inventoryPage=ui.page||1;if(ui.sort)state.sort=ui.sort;
    if(["15","30","60"].includes(ui.range)){$("host-range").value=ui.range;window.govmcSelect?.sync($("host-range"));}
    renderOverview();renderList();renderConsoleMachines();renderUpdated();
  }
  window.addEventListener("pagehide",saveSnapshot);
  let snapshotTimer;
  for(const event of ["input","change","click"])document.addEventListener(event,()=>{clearTimeout(snapshotTimer);snapshotTimer=setTimeout(saveSnapshot,100);});

  function rememberEnvironment() {
    try { sessionStorage.setItem("govmc-web-profile", JSON.stringify({ instance: state.environmentInstance, name: state.environment, connection: state.environmentConnection })); } catch {}
  }

  function environmentAddress(profile) {
    let address = profile.url;
    try { address = new URL(profile.url).host; } catch {}
    return profile.username + "@" + address;
  }

  function renderEnvironments() {
    if (state.environment && ["connected", "error", "disconnected"].includes(state.environmentConnection)) rememberEnvironment();
    const picker = $("environment");
    picker.hidden = !state.environments.length;
    picker.setAttribute("aria-busy", String(state.switchingEnvironment));
    const current = state.environments.find((profile) => profile.name === state.environment);
    const summary = picker.querySelector("summary");
    const status = t(({ connected: "已连接", connecting: "正在连接", error: "连接异常", disconnected: "已断开" })[state.environmentConnection] || "未知");
    summary.firstElementChild.textContent = current ? Array.from(current.name).slice(0, 2).join("").toUpperCase() : "··";
    picker.querySelector(".environment-dot").dataset.state = state.environmentConnection;
    summary.title = current ? current.name + " · " + environmentAddress(current) + " · " + status : t("选择环境");
    summary.setAttribute("aria-label", t("切换环境") + " · " + (current?.name || t("选择环境")) + " · " + status);
    picker.querySelector(".dropdown-options").replaceChildren(...state.environments.map((profile) => {
      const option = button("", () => confirmEnvironmentSwitch(profile.name), "", state.switchingEnvironment);
      option.dataset.profile = profile.name;
      option.title = profile.name + " · " + environmentAddress(profile);
      option.setAttribute("aria-pressed", String(profile.name === state.environment));
      option.append(el("strong", profile.name), el("small", environmentAddress(profile)));
      return option;
    }));
  }

  async function loadEnvironments() {
    try {
      const data = await api("/v1/profiles");
      state.environments = data.profiles || [];
      state.environmentInstance = data.instance || "";
      let remembered;
      try { remembered = JSON.parse(sessionStorage.getItem("govmc-web-profile")); } catch {}
      const chosen = remembered?.instance === state.environmentInstance && state.environments.some((p) => p.name === remembered?.name) ? remembered.name : data.current;
      if (chosen && !state.environments.some((p) => p.name === chosen)) throw new Error(t("选择环境"));
      state.environment = chosen || "";
      renderEnvironments();
      if (chosen && chosen !== data.current) await api("/v1/profiles/connect", { method: "POST", body: JSON.stringify({ name: chosen }) });
      state.environmentReady = true;
      await restoreSnapshot();
      state.environmentConnection = "connected";
      renderEnvironments();
      rememberEnvironment();
    } catch (error) {
      if (error.status === 404) { state.environmentReady = true; return; } // Compatibility with fixed-target servers.
      state.environmentReady = false;
      state.environmentConnection = "error";
      renderEnvironments();
      notice(t("环境初始化失败：") + error.message);
    }
  }

  const environmentDialog = $("environment-confirm");
  let pendingEnvironmentChoice = null;
  function confirmEnvironmentSwitch(name) {
    if (state.switchingEnvironment) return;
    if (state.environment === name && state.environmentReady) { $("environment").open = false; return; }
    if (state.busy.size) { notice(t("请等待当前操作完成后切换环境")); return; }
    const target = state.environments.find((profile) => profile.name === name);
    if (!target) return;
    pendingEnvironmentChoice = name;
    const current = state.environments.find((profile) => profile.name === state.environment);
    $("environment-from-name").textContent = current?.name || t("未知");
    $("environment-from-address").textContent = current ? environmentAddress(current) : "";
    $("environment-to-name").textContent = target.name;
    $("environment-to-address").textContent = environmentAddress(target);
    $("environment").open = false;
    environmentDialog.returnValue = "";
    environmentDialog.showModal();
  }
  environmentDialog.addEventListener("close", () => {
    const chosen = pendingEnvironmentChoice;
    pendingEnvironmentChoice = null;
    if (environmentDialog.returnValue === "switch" && chosen) switchEnvironment(chosen);
  });

  async function switchEnvironment(name) {
    if (state.switchingEnvironment || (name === state.environment && state.environmentReady)) return;
    if (state.busy.size) { notice(t("请等待当前操作完成后切换环境")); return; }
    const previousConnection = state.environmentConnection;
    state.switchingEnvironment = true;
    state.environmentConnection = "connecting";
    $("main").inert = true;
    renderEnvironments();
    try {
      await api("/v1/profiles/connect", { method: "POST", body: JSON.stringify({ name }) });
      rememberConsoleSessions();
      try {sessionStorage.removeItem(consoleHistoryKey());} catch {}
      consoleRestorePending=false;
      // Commit only after the new profile authenticates; old requests cannot reach this target.
      environmentController.abort();
      environmentController = new AbortController();
      state.environmentEpoch++;
      state.environment = name;
      state.environmentReady = true;
      state.environmentConnection = "connected";
      state.detailRequest++;
      state.hostRequest++;
      detailController?.abort();
      clearScreenshot();
      action = null;
      pendingDisconnect = null;
      for (const dialog of document.querySelectorAll("dialog[open]")) dialog.close();
      for (const session of state.sessions.values()) { send(session, "disconnect"); clearTimeout(session.timeout); session.frame.remove(); }
      state.sessions.clear();
      state.sessionStates.clear();
      state.active = state.selected = state.vm = state.vmMetrics = state.vms = state.host = state.stores = state.metrics = null;
      state.hostRange = state.vmRange = state.updated = null;
      state.hostUnavailable = false;
      state.detailError = state.vmMetricError = "";
      state.vmMetricLoading = false;
      state.consolePage = state.inventoryPage = 1;
      state["os-filter"] = state["power-filter"] = "";
      $("search").value = $("console-search").value = "";
      $("updated").textContent = "";
      $("updated").parentElement.hidden = true;
      state.refreshing = false;
      $("refresh").disabled = false;
    $("refresh").setAttribute("aria-busy","false");
      $("environment").open = false;
      rememberEnvironment();
      renderOverview(); renderList(); renderSessions();
      notice(t("环境已切换：") + name);
    } catch (error) {
      state.environmentConnection = previousConnection;
      notice(t("环境切换失败：") + error.message);
    } finally {
      state.switchingEnvironment = false;
      $("main").inert = false;
      renderEnvironments();
    }
    if (state.environment === name && state.environmentReady) await refresh();
  }

  window.addEventListener("govmc-tasks-updated",()=>{if(!state.refreshing)refresh()});
  function power(vm) {
    return el("span", powerLabels[vm.powerState] || vm.powerState || t("未知"), "state-chip " + (vm.powerState === "poweredOn" ? "online" : ""));
  }

  function problem(vm) {
    if (vm.question) return t("等待回答");
    if (vm.connectionState && vm.connectionState !== "connected") return t("连接异常");
    return "";
  }

  function readSeries(metrics, name) {
    const item = metrics?.series?.find((series) => series.name === name && !series.instance);
    return (item?.points || []).filter((point) => point.timeUtc && has(point.value) && point.value >= 0 && Number.isFinite(+new Date(point.timeUtc))).sort((a, b) => +new Date(a.timeUtc) - +new Date(b.timeUtc));
  }

  const plotObserver = new ResizeObserver((entries) => {
    for (const { target } of entries) chartDraws.get(target)?.();
  });
  const chartDraws = new WeakMap();
  const timeRange = (minutes) => {
    const to = Date.now();
    return { from: to - minutes * 60000, to };
  };
  const formatTime = (value, options) => new Intl.DateTimeFormat(document.documentElement.lang, options).format(value);
  const shortTime = (value) => formatTime(value, { hour: "2-digit", minute: "2-digit", hourCycle: "h23" });

  function chart(title, series, suffix, minimumMax, range, tone) {
    const formatValue = (value) => units(value, suffix, tone === "chart-disk" ? 3 : 1);
    const figure = el("figure", null, "chart " + tone);
    const caption = el("figcaption");
    const headings = el("div", null, "chart-legend");
    caption.append(el("h3", title), headings);
    for (const [index, line] of series.entries()) {
      const label = el("span", null, index ? "legend alt" : "legend");
      label.append(el("small", line.name), el("strong", formatValue(line.points.at(-1)?.value)));
      headings.append(label);
    }
    figure.append(caption);
    const plot = el("div", null, "plot");
    const values = series.flatMap((line) => line.points);
    if (!values.length) {
      plot.append(empty(t("暂无采样")));
      figure.append(plot);
      return figure;
    }
    const tooltip = el("div", null, "chart-tip");
    tooltip.hidden = true;
    plot.tabIndex = 0;
    plot.setAttribute("aria-label", title + " — " + series.map((line) => line.name + " " + formatValue(line.points.at(-1)?.value)).join(", "));
    const ns = "http://www.w3.org/2000/svg";
    const svg = document.createElementNS(ns, "svg");
    svg.setAttribute("aria-hidden", "true");
    const make = (tag, attributes, label) => {
      const node = document.createElementNS(ns, tag);
      for (const [name, value] of Object.entries(attributes)) node.setAttribute(name, value);
      if (label != null) node.textContent = label;
      return node;
    };
    const ticks = [...new Set(values.map((point) => +new Date(point.timeUtc)))].sort((a, b) => a - b);
    const byTime = series.map((line) => new Map(line.points.map((point) => [+new Date(point.timeUtc), point.value])));
    const span = range.to - range.from;
    const top = Math.max(minimumMax || 0, ...values.map((point) => point.value), 0.01);
    const magnitude = 10 ** Math.floor(Math.log10(top));
    const ceiling = Math.ceil(top / magnitude * 2) / 2 * magnitude;
    let active = -1;
    let x, left, right;
    function draw() {
      const w = plot.clientWidth, h = plot.clientHeight;
      if (!w || !h) return;
      left = 46;
      right = w - 10;
      const floor = h - 26, chartHeight = floor - 12;
      x = (at) => left + (Math.min(range.to, Math.max(range.from, at)) - range.from) / span * (right - left);
      const y = (v) => floor - v / ceiling * chartHeight;
      svg.setAttribute("viewBox", `0 0 ${w} ${h}`);
      const nodes = [];
      for (let i = 0; i <= 4; i++) {
        const v = ceiling * i / 4, yy = y(v);
        nodes.push(make("line", { x1: left, x2: right, y1: yy, y2: yy, class: "gridline" }));
        nodes.push(make("text", { x: left - 6, y: yy + 4, "text-anchor": "end", class: "axis" }, units(v, suffix.trim() === "%" ? "%" : "", ceiling >= 10 ? 0 : ceiling >= 1 ? 1 : ceiling >= 0.1 ? 2 : 3)));
      }
      const divisions = Math.max(2, Math.min(6, Math.floor((right - left) / 85)));
      for (let i = 0; i <= divisions; i++) {
        const at = range.from + span * i / divisions;
        nodes.push(make("text", { x: x(at), y: h - 7, "text-anchor": i === 0 ? "start" : i === divisions ? "end" : "middle", class: "axis" }, shortTime(at)));
      }
      series.forEach((line, index) => {
        const segments = [];
        let last;
        for (const point of line.points) {
          const at = +new Date(point.timeUtc);
          if (last == null || at - last > 50000) segments.push([]);
          segments.at(-1).push([x(at), y(point.value)]);
          last = at;
        }
        for (const points of segments) {
          const d = points.map(([px, py], i) => (i ? "L" : "M") + px + " " + py).join(" ");
          if (!index && points.length > 1) nodes.push(make("path", { d: d + ` L${points.at(-1)[0]} ${floor} L${points[0][0]} ${floor} Z`, class: "area" }));
          if (points.length > 1) nodes.push(make("path", { d, class: index ? "series alt" : "series" }));
          else nodes.push(make("circle", { cx: points[0][0], cy: points[0][1], r: 2, class: index ? "point alt" : "point" }));
        }
      });
      tooltip.hidden = active < 0;
      if (active >= 0) {
        const at = ticks[active], px = x(at);
        nodes.push(make("line", { x1: px, x2: px, y1: 12, y2: floor, class: "chart-cursor" }));
        tooltip.replaceChildren(el("small", stamp(at)));
        byTime.forEach((line, i) => {
          const value = line.get(at);
          if (has(value)) nodes.push(make("circle", { cx: px, cy: y(value), r: 3.5, class: i ? "point alt" : "point" }));
          const row = el("div", null, "tip-row");
          row.append(el("span", series[i].name), el("strong", formatValue(value)));
          tooltip.append(row);
        });
        tooltip.style.left = Math.max(0, Math.min(w - tooltip.offsetWidth, px + 12)) + "px";
      }
      svg.replaceChildren(...nodes);
    }
    plot.addEventListener("pointermove", (event) => {
      if (!x) return;
      const px = event.clientX - plot.getBoundingClientRect().left;
      active = ticks.reduce((nearest, at, index) => Math.abs(x(at) - px) < Math.abs(x(ticks[nearest]) - px) ? index : nearest, 0);
      draw();
    });
    const hideTip = () => { active = -1; draw(); };
    plot.addEventListener("pointerleave", hideTip);
    plot.addEventListener("blur", hideTip);
    plot.addEventListener("keydown", (event) => {
      if (event.key === "Escape") { hideTip(); return; }
      if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
      event.preventDefault();
      active = active < 0 ? ticks.length - 1 : Math.max(0, Math.min(ticks.length - 1, active + (event.key === "ArrowLeft" ? -1 : 1)));
      draw();
    });
    plot.append(svg, tooltip);
    chartDraws.set(plot, draw);
    plotObserver.observe(plot);
    figure.append(plot);
    return figure;
  }

  function charts(container, metrics, range) {
    container.querySelectorAll(".plot").forEach((plot) => plotObserver.unobserve(plot));
    const cpu = readSeries(metrics, "cpu.usage.average");
    const memory = readSeries(metrics, "mem.active.average").map((p) => ({ ...p, value: p.value / 2 ** 20 }));
    const rx = readSeries(metrics, "net.bytesRx.average");
    const tx = readSeries(metrics, "net.bytesTx.average");
    const diskPrefix = range.diskPrefix || "disk";
    const diskSeries = (direction) => readSeries(metrics, diskPrefix + "." + direction + ".average")
      .map((point) => ({ ...point, value: point.value / 1000 }));
    const network = () => chart(t("网络"), [{ name: t("接收"), points: rx }, { name: t("发送（虚线）"), points: tx }], " Mbps", 0, range, "chart-network");
    container.replaceChildren(
      chart("CPU", [{ name: "CPU", points: cpu }], "%", 100, range, "chart-cpu"),
      chart(t("活动内存"), [{ name: t("活动内存"), points: memory }], " GiB", 0, range, "chart-memory"),
      network(),
      chart(t("磁盘吞吐"), [{ name: t("读取"), points: diskSeries("read") }, { name: t("写入（虚线）"), points: diskSeries("write") }], " MB/s", 0, range, "chart-disk"),
    );
  }

  function renderOverview() {
    const h = state.host;
    const running = state.vms?.filter((vm) => vm.powerState === "poweredOn").length;
    const availableStores = state.stores?.filter((ds) => has(ds.capacity) && has(ds.freeSpace));
    const storage = availableStores?.reduce((a, ds) => [a[0] + ds.capacity - ds.freeSpace, a[1] + ds.capacity], [0, 0]);
    const mem = readSeries(state.metrics, "mem.active.average").at(-1);
    const latest = state.hostRange?.latest;
    $("host-sample").textContent = t("最近采样时间: {time}", {
      time: latest ? fullTimestamp(new Date(latest)) : noData,
    });
    const runningVMs = state.vms?.filter((vm) => vm.powerState === "poweredOn");
    const totalVCPU = (vms) => vms?.every((vm) => has(vm.numCPU) && vm.numCPU > 0) ?
      count.format(vms.reduce((total, vm) => total + vm.numCPU, 0)) : noData;
    const status = state.hostUnavailable || (h?.connectionState && h.connectionState !== "connected") ? ["未连接", ""] :
      h?.maintenanceMode ? ["维护中", "warning"] : h?.overallStatus === "green" ? ["正常", "running-chip"] :
        h?.overallStatus === "red" ? ["异常", "warning"] : h?.overallStatus === "yellow" ? ["警告", "warning"] :
          h?.connectionState === "connected" ? ["已连接", "connected-chip", "ESXi 未提供明确的健康状态"] :
            [h ? "未知" : "正在连接", ""];
    const items = [
      [t("正常运行天数"), has(h?.uptimeSeconds) ? t("{days} 天", {
        days: new Intl.NumberFormat(document.documentElement.lang, { maximumFractionDigits: 1 }).format(h.uptimeSeconds / 86400),
      }) : noData, [h?.name, h?.login].filter(Boolean), status],
      [t("运行中 VM"), running == null ? noData : count.format(running), [state.vms ? t("共 {count} 台", { count: count.format(state.vms.length) }) : noData]],
      ["vCPU", totalVCPU(runningVMs), [t("运行中 VM 分配 / 总计 {count}", { count: totalVCPU(state.vms) })]],
      [t("活动内存"), has(mem?.value) ? units(mem.value / 2 ** 20, " GiB") : noData, [has(h?.memoryMB) ? t("总计 ") + units(h.memoryMB / 1024, " GiB") : noData]],
      [t("数据存储已用"), availableStores?.length ? gib(storage[0]) : noData, [
        availableStores?.length ? t("总计 ") + gib(storage[1]) : "",
        Array.isArray(state.stores) ? t("{count} 个 datastore", { count: count.format(state.stores.length) }) : "",
      ].filter(Boolean)],
    ];
    $("kpis").replaceChildren(...items.map(([label, value, notes, health]) => {
      const card = el("div", null, "metric");
      const heading = el("dt", label);
      if (health) {
        const badge = el("span", t(health[0]), "state-chip " + health[1]);
        if (health[2]) badge.title = t(health[2]);
        heading.append(badge);
      }
      const foot = el("dd", null, "metric-note");
      for (const note of notes.length ? notes : [noData]) {
        const detail = el("span", note);
        detail.title = note;
        foot.append(detail);
      }
      card.append(heading, el("dd", value, "metric-value"), foot);
      return card;
    }));
    charts($("host-charts"), state.metrics, state.hostRange || timeRange(Number($("host-range").value)));
  }

  const tableColumns = [
    ["name", "名称"], ["guestOS", "操作系统"], ["powerState", "电源状态"], ["networks", "网卡"], ["numCPU", "规格"],
    ["cpuPct", "CPU"], ["memActivePct", "活动内存"], ["disks", "磁盘"],
    ["netRxMbps", "接收速率"], ["netTxMbps", "发送速率"], ["actions", "操作"],
  ];

  const meterHistory = new Map();

  function meter(value, tone, previous) {
    const widget = el("div", null, "meter " + tone);
    const bar = el("span", null, "meter-track");
    bar.setAttribute("aria-hidden", "true");
    if (has(value)) {
      const fill = el("i");
      const width = (pct) => Math.min(100, Math.max(0, pct)) + "%";
      const animate = has(previous) && previous !== value && !matchMedia("(prefers-reduced-motion: reduce)").matches;
      fill.style.width = width(animate ? previous : value);
      if (animate) requestAnimationFrame(() => requestAnimationFrame(() => {
        if (fill.isConnected) fill.style.width = width(value);
      }));
      if (value >= 95) fill.classList.add("critical");
      else if (value >= 80) fill.classList.add("warn");
      bar.append(fill);
    }
    widget.append(bar, el("span", units(value, "%"), "meter-value"));
    return widget;
  }

  function guestOSName(vm) {
    const names = {
      windows11_64Guest: "Microsoft Windows 11 (64-bit)",
      windows9_64Guest: "Microsoft Windows 10 (64-bit)",
      windows9Guest: "Microsoft Windows 10 (32-bit)",
      ubuntu64Guest: "Ubuntu Linux (64-bit)",
      ubuntuGuest: "Ubuntu Linux (32-bit)",
      otherLinux64Guest: "Other Linux (64-bit)",
      otherLinuxGuest: "Other Linux (32-bit)",
    };
    const name = vm.guestOS || names[vm.guestId] || vm.guestId || noData;
    if (window.govmcI18n.getLanguage() !== "zh") return name;
    return name.replace(/Other Linux|\b(?:32|64)[- ]bit\b/gi, (part) => {
      if (part.toLowerCase() === "other linux") return "其他 Linux";
      return part.slice(0, 2) + " 位";
    });
  }

  function vbsState(vm) {
    return vm.vbsEnabled ?? vm.config?.vbs;
  }

  function osDisplay(vm) {
    const name = guestOSName(vm);
    const display = el("div", null, "guest-os");
    const title = el("span", name);
    title.title = name;
    display.append(title);
    const vbs = vbsState(vm);
    if (typeof vbs === "boolean" || /windows/i.test(name)) {
      const label = vbs === true ? "VBS 已启用" : vbs === false ? "VBS 未启用" : "VBS 未知";
      display.append(el("small", t(label), "state-chip" + (vbs === true ? " good" : "")));
    }
    return display;
  }

  function toolsDisplay(guest = {}) {
    const absent = guest.toolsStatus === "toolsNotInstalled";
    const installed = !absent && (guest.toolsVersionText || guest.toolsVersion && guest.toolsVersion !== "0" ||
      ["toolsOk", "toolsOld", "toolsNotRunning"].includes(guest.toolsStatus) ||
      ["guestToolsRunning", "guestToolsExecutingScripts"].includes(guest.toolsRunningStatus));
    const display = el("span", t(absent ? "未安装" : installed ? "已安装" : "未知"), "tools-summary");
    if (installed) {
      const version = guest.toolsVersionText || t("版本未上报");
      const detail = el("small", "(" + version + ")", "tools-version");
      detail.title = version;
      display.append(detail);
    }
    return display;
  }

  const osFilterKey = (vm) => vm.guestId || vm.guestOS || "unknown";
  function renderFilter(id, label, choices) {
    const picker = $(id);
    if (state[id] && !choices.some(([key]) => key === state[id])) state[id] = "";
    const active = state[id] || "";
    const selected = choices.find(([key]) => key === active)[1];
    const summary = picker.querySelector("summary");
    summary.firstElementChild.textContent = selected;
    summary.setAttribute("aria-label", t(label) + " · " + selected);
    summary.title = selected;
    const menu = picker.querySelector(".dropdown-options");
    const signature = JSON.stringify(choices);
    if (menu.dataset.choices !== signature) {
      menu.dataset.choices = signature;
      menu.replaceChildren(...choices.map(([value, text]) => {
        const option = button(text, () => {
          state[id] = value;
          state.inventoryPage = 1;
          picker.open = false;
          summary.focus();
          renderList();
        });
        option.value = value;
        return option;
      }));
    }
    for (const option of menu.children) option.setAttribute("aria-pressed", String(option.value === active));
  }

  function renderList() {
    const needle = $("search").value.trim().toLocaleLowerCase();
    const systems = new Map();
    for (const vm of state.vms || []) {
      const key = osFilterKey(vm);
      if (!systems.has(key) || vm.guestOS) systems.set(key, guestOSName(vm) === noData ? t("未知") : guestOSName(vm));
    }
    renderFilter("os-filter", "操作系统", [["", t("全部操作系统")], ...[...systems].sort((a, b) => a[1].localeCompare(b[1], getLanguage()))]);
    renderFilter("power-filter", "电源状态", [["", t("全部状态")], ...Object.entries(powerLabels)]);
    const status = state["power-filter"];
    const system = state["os-filter"];
    const list = $("vm-list");
    const active = list.contains(document.activeElement) ? document.activeElement.dataset.focusKey : "";
    const vms = state.vms?.filter((vm) => (!status || vm.powerState === status) && (!system || osFilterKey(vm) === system) &&
      (vm.name + " " + vm.id).toLocaleLowerCase().includes(needle));
    list.querySelectorAll(".plot").forEach((plot) => plotObserver.unobserve(plot));
    const counts = $("inventory-counts");
    counts.replaceChildren();
    if (state.vms) {
      counts.append(el("span", t("共 {count} 台", { count: count.format(state.vms.length) }), "state-chip"));
      for (const status of ["poweredOn", "poweredOff", "suspended"]) {
        const amount = state.vms.filter((vm) => vm.powerState === status).length;
        if (status !== "suspended" || amount) {
          counts.append(el("span", powerLabels[status] + " " + count.format(amount),
            "state-chip" + (status === "poweredOn" ? " running-chip" : "")));
        }
      }
    }
    if (!vms) { $("inventory-pages").hidden = true; list.replaceChildren(empty(t("正在读取虚拟机"))); return; }
    const currentIDs = new Set(state.vms.map((vm) => vm.id));
    for (const id of meterHistory.keys()) if (!currentIDs.has(id)) meterHistory.delete(id);
    const { key, direction } = state.sort;
    const sortValue = (vm) => {
      if (key === "guestOS") return guestOSName(vm);
      if (key === "disks") return vm.committedBytes;
      if (key === "networks") return vm.networks?.length || 0;
      return vm[key];
    };
    vms.sort((a, b) => {
      const av = sortValue(a), bv = sortValue(b);
      if (av == null || bv == null) return av == null ? (bv == null ? 0 : 1) : -1;
      const comparison = typeof av === "number" ? av - bv : String(av).localeCompare(String(bv), document.documentElement.lang);
      return direction * comparison || a.name.localeCompare(b.name);
    });
    const perPage = 20;
    const pages = Math.max(1, Math.ceil(vms.length / perPage));
    state.inventoryPage = Math.min(pages, Math.max(1, state.inventoryPage));
    const offset = (state.inventoryPage - 1) * perPage;
    const visible = vms.slice(offset, offset + perPage);
    if (state.selected && !visible.some((vm) => vm.id === state.selected)) closeVM();
    $("inventory-pages").hidden = pages === 1;
    $("inventory-prev").disabled = state.inventoryPage === 1;
    $("inventory-next").disabled = state.inventoryPage === pages;
    $("inventory-page-number").textContent = count.format(state.inventoryPage) + " / " + count.format(pages);
    $("inventory-page-range").textContent = t("显示 {from}–{to} 台，共 {total} 台", {
      from: count.format(offset + 1), to: count.format(offset + visible.length), total: count.format(vms.length),
    });
    const table = el("table", null, "sys vm-table");
    table.setAttribute("aria-labelledby", "inventory-title");
    const columns = el("colgroup", null, "vm-table-columns");
    const head = el("thead"), headings = el("tr"), body = el("tbody");
    for (const [field, label] of tableColumns) {
      columns.append(el("col", null, "col-" + field));
      const cell = el("th", null, "col-" + field);
      cell.scope = "col";
      if (field === "actions") {
        cell.textContent = t(label);
        headings.append(cell);
        continue;
      }
      if (key === field) cell.setAttribute("aria-sort", direction === 1 ? "ascending" : "descending");
      const sort = button(t(label), () => {
        state.inventoryPage = 1;
        state.sort = { key: field, direction: key === field ? -direction : (["name", "guestOS", "powerState"].includes(field) ? 1 : -1) };
        renderList();
      });
      sort.dataset.focusKey = "sort:" + field;
      if (field === "disks") sort.title = t("按已用空间排序");
      if (field === "networks") sort.title = t("按网卡数量排序");
      sort.append(el("span", key === field ? (direction === 1 ? " ↑" : " ↓") : " ↕", "sort-arrow"));
      cell.append(sort);
      headings.append(cell);
    }
    head.append(headings);
    for (const vm of visible) {
      const row = el("tr");
      const name = button("", () => openVM(vm.id), "table-vm-name");
      const expanded = state.selected === vm.id;
      name.dataset.focusKey = "vm:" + vm.id;
      name.setAttribute("aria-expanded", String(expanded));
      if (expanded) name.setAttribute("aria-controls", "vm-detail-" + vm.id);
      const identity = el("span", null, "vm-identity");
      identity.title = vm.name;
      const metadata = el("span", null, "vm-identity-meta");
      metadata.append(el("small", vm.id, "muted"));
      if (problem(vm)) metadata.append(el("span", problem(vm), "state-chip warning"));
      identity.append(el("strong", vm.name), metadata);
      const dot = el("i", null, "vm-dot " + (problem(vm) ? "issue" : vm.powerState === "poweredOn" ? "running" : ""));
      dot.setAttribute("aria-hidden", "true");
      const chevron = el("span", expanded ? "▾" : "▸", "row-chevron");
      chevron.setAttribute("aria-hidden", "true");
      name.title = t(expanded ? "收起详情" : "展开详情");
      name.append(chevron, dot, identity);
      const previous = meterHistory.get(vm.id);
      meterHistory.set(vm.id, { cpu: vm.cpuPct, memory: vm.memActivePct });
      const nics = vm.networks || [];
      const firstNIC = nics[0];
      const nicLabel = el("div", null, "device-summary");
      nicLabel.append(el("span", (firstNIC?.mac || noData) + (nics.length > 1 ? " +" + (nics.length - 1) : "")));
      if (firstNIC) {
        nicLabel.append(el("small", firstNIC.portgroup || noData));
        nicLabel.title = nics.map((nic) => [nic.label, nic.portgroup, nic.mac].filter(Boolean).join(" / ")).join("\n");
      }
      const disks = vm.disks || [];
      const capacities = disks.filter((disk) => has(disk.capacityKB));
      const bytes = capacities.length ? capacities.reduce((total, disk) => total + disk.capacityKB * 1024, 0) : null;
      const configured = gib(bytes) + (capacities.length && capacities.length !== disks.length ? "+" : "");
      const diskLabel = el("div", null, "device-summary");
      diskLabel.append(el("span", gib(vm.committedBytes) + " / " + configured),
        el("small", t("{count} 块磁盘", { count: count.format(disks.length) })));
      diskLabel.title = t("已用空间是 ESXi 报告的整台虚拟机数据存储占用，包括快照等文件") +
        (disks.length ? "\n" + disks.map((disk) => disk.label + " / " + gib(has(disk.capacityKB) ? disk.capacityKB * 1024 : null)).join("\n") : "");
      const actions = el("div", null, "table-actions");
      const edit = button(t("编辑配置"), async () => {
        if (state.busy.has(vm.id)) return;
        state.busy.add(vm.id);
        edit.disabled = true;
        try { editVM(await api(endpoint(vm.id))); }
        catch (error) { notice(error.message); }
        finally {
          state.busy.delete(vm.id);
          if (edit.isConnected) edit.disabled = false;
          else renderList();
        }
      }, "vm-edit-action", state.busy.has(vm.id)||state.snapshotPending);
      const consoleButton = button(t("控制台"), () => openSession(vm), "vm-console-action", vm.powerState !== "poweredOn"||state.snapshotPending);
      if (consoleButton.disabled) consoleButton.title = t("虚拟机开机后可连接");
      consoleButton.dataset.focusKey = "console:" + vm.id;
      const clone=button(t("Clone machine"),()=>window.govmcTasks?.clone(vm,state.environment),"vm-clone-action",vm.powerState!=="poweredOff"||vm.connectionState!=="connected"||state.snapshotPending);
 clone.setAttribute("aria-label",t("Clone machine"));clone.innerHTML='<svg viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.7"><path d="M8 8h13v13H8ZM3 16V3h13"/></svg>';clone.setAttribute("aria-label",t("Clone machine"));clone.innerHTML='<svg viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M8 8h13v13H8ZM3 16V3h13"/></svg>';
 clone.title=t(vm.powerState!=="poweredOff"?"Clone source must be powered off and connected":"Clone machine");actions.append(consoleButton, edit, clone, powerMenu(vm));
      edit.dataset.focusKey = "edit:" + vm.id;
      const values = [
        name, osDisplay(vm), power(vm), nicLabel,
        el("span", [has(vm.numCPU) ? vm.numCPU + " vCPU" : noData, units(has(vm.memoryMB) ? vm.memoryMB / 1024 : null, " GiB")].join(" / ")),
        meter(vm.cpuPct, "cpu", previous?.cpu), meter(vm.memActivePct, "memory", previous?.memory), diskLabel,
        el("span", units(vm.netRxMbps, " Mbps"), "rx"), el("span", units(vm.netTxMbps, " Mbps"), "tx"), actions,
      ];
      values.forEach((value, index) => {
        const [field, label] = tableColumns[index];
        const cell = el("td", null, "col-" + field);
        if (field !== "name" && field !== "powerState") cell.dataset.label = t(label);
        cell.append(value);
        row.append(cell);
      });
      row.addEventListener("click", (event) => { if (!event.target.closest("button, details, .table-actions") && !window.getSelection()?.toString()) openVM(vm.id); });
      body.append(row);
      if (expanded) {
        const details = el("tr", null, "expanded-row");
        const cell = el("td");
        cell.colSpan = tableColumns.length;
        cell.append(renderVM(vm.id));
        details.append(cell);
        body.append(details);
      }
    }
    if (!vms.length) {
      const row = el("tr"), cell = el("td", t("没有符合条件的虚拟机"), "empty table-empty");
      cell.colSpan = tableColumns.length;
      row.append(cell);
      body.append(row);
    }
    table.append(columns, head, body);
    list.replaceChildren(table);
    if (active) [...list.querySelectorAll("button")].find((b) => b.dataset.focusKey === active)?.focus({ preventScroll: true });
  }

  function metricPath(path, minutes, names) {
    return path + "?" + new URLSearchParams({ maxSample: String(minutes * 3), names });
  }

  async function fetchTrend(path, minutes, diskPrefix, options) {
    const names = "cpu.usage.average,mem.active.average,net.bytesRx.average,net.bytesTx.average";
    // An unavailable disk counter must not hide the CPU, memory or network charts.
    const disk = api(metricPath(path, minutes, diskPrefix + ".read.average," + diskPrefix + ".write.average"), options).catch(() => null);
    const metrics = await api(metricPath(path, minutes, names), options);
    const storage = await disk;
    return { ...metrics, series: [...(metrics.series || []), ...(storage?.series || [])] };
  }

  function sampleRange(metrics, minutes) {
    const points = (metrics?.series || []).flatMap((series) => series.points || []);
    const latest = points.reduce((best, point) => {
      const at = +new Date(point.timeUtc);
      return point.timeUtc && Number.isFinite(at) && has(point.value) && point.value >= 0 ? Math.max(best, at) : best;
    }, 0);
    const to = latest || Date.now();
    return { from: to - minutes * 60000, to, latest };
  }

  async function updateHostMetrics() {
    const id = ++state.hostRequest;
    const minutes = Number($("host-range").value);
    const value = await fetchTrend("/v1/host/metrics", minutes, "disk");
    if (id === state.hostRequest) {
      state.metrics = value;
      state.hostRange = sampleRange(value, minutes);
    }
  }

  async function refresh() {
    if (state.refreshing || !state.environmentReady || state.switchingEnvironment) return;
    const epoch = state.environmentEpoch,mutationEpoch=state.mutationEpoch;
    state.refreshing = true;
    $("refresh").disabled = true;
    $("refresh").setAttribute("aria-busy","true");
    const requests = [
      api("/v1/vms?view=full").then((list) => {
        state.vms = list.sort((a, b) => a.name.localeCompare(b.name));
        const known = new Set(list.map((vm) => vm.id));
        for (const id of state.sessionStates.keys()) if (!known.has(id) && !state.sessions.has(id)) state.sessionStates.delete(id);
      }),
    ];
    if (state.page === "overview" || !state.host) {
      requests.push(api("/v1/host").then((h) => {
        state.host = h;
        state.hostUnavailable = false;
      }, (error) => {
        state.hostUnavailable = true;
        throw error;
      }));
    }
    if (state.page === "overview") {
      requests.push(api("/v1/datastores").then((list) => { state.stores = list; }), updateHostMetrics());
    }
    const results = await Promise.allSettled(requests);
    if (epoch !== state.environmentEpoch) return;
    const failed = results.filter((r) => r.status === "rejected");
    state.environmentConnection = failed.length ? "error" : "connected";
    renderEnvironments();
    if (!failed.length) { state.updated = new Date(); renderUpdated(); }
    if(mutationEpoch===state.mutationEpoch&&results[0].status==="fulfilled")state.snapshotPending=false;
    if (results[0].status === "fulfilled") await restoreConsoleSessions();
    if(!failed.length&&mutationEpoch===state.mutationEpoch)state.snapshotDirty=false;
    saveSnapshot();
    notice(failed.length ? t("刷新失败：") + failed[0].reason.message : "");
    renderOverview();
    renderList();
    for (const session of state.sessions.values()) {
      const latest = state.vms?.find((vm) => vm.id === session.vm.id);
      if (latest) session.vm = latest;
    }
    renderSessions();
    if (state.page === "overview" && state.selected) await loadVM(state.selected);
    state.refreshing = false;
    $("refresh").disabled = false;
    $("refresh").setAttribute("aria-busy","false");
  }

  function facts(rows, compact = false) {
    const dl = el("dl", null, compact ? "facts facts-compact" : "facts");
    for (const [label, value, hint] of rows) {
      const name = el("dt", label);
      if (hint) name.title = hint;
      const description = el("dd");
      if (value instanceof Node) description.append(value);
      else {
        description.textContent = value == null || value === "" ? noData : value;
        if (compact && typeof value === "string" && value && value !== noData) description.title = value;
      }
      if (compact) {
        const pair = el("div");
        pair.append(name, description);
        dl.append(pair);
      } else dl.append(name, description);
    }
    return dl;
  }

  let positionOpenPowerMenu;
  function powerMenu(vm) {
    const items = vm.powerState === "poweredOn" ? ["off", "reset", "suspend"] :
      vm.powerState === "poweredOff" || vm.powerState === "suspended" ? ["on"] : [];
    const menu = el("div", null, "power-menu");
    const actions = el("div", null, "power-items");
    actions.popover = "auto";
    const trigger = button(t("电源操作") + " ▾", () => {
      if (actions.matches(":popover-open")) { actions.hidePopover(); return; }
      actions.showPopover({ source: trigger });
      positionOpenPowerMenu = () => {
        if (!actions.matches(":popover-open") || !trigger.isConnected) return;
        const anchor = trigger.getBoundingClientRect();
        const width = actions.offsetWidth, height = actions.offsetHeight;
        actions.style.left = Math.max(6, Math.min(innerWidth - width - 6, anchor.right - width)) + "px";
        actions.style.top = Math.max(6, anchor.bottom + height + 6 <= innerHeight ? anchor.bottom + 4 : anchor.top - height - 4) + "px";
      };
      positionOpenPowerMenu();
      actions.querySelector("button")?.focus({ preventScroll: true });
    }, "", !items.length || state.busy.has(vm.id)||state.snapshotPending);
    trigger.setAttribute("aria-expanded", "false");
    actions.addEventListener("toggle", (event) => {
      trigger.setAttribute("aria-expanded", String(event.newState === "open"));
    });
    for (const action of items) {
      actions.append(button(operations[action], () => { actions.hidePopover(); confirmPower(vm, action); }));
    }
    menu.append(trigger, actions);
    return menu;
  }

  function hardwareTable(label, columns, rows) {
    if (!rows?.length) return empty(t("无记录"));
    const wrapper = el("div", null, "hardware-table-wrap");
    const table = el("table", null, "hardware-table");
    table.setAttribute("aria-label", t(label));
    const headings = el("thead"), header = el("tr"), body = el("tbody");
    for (const column of columns) {
      const th = el("th", t(column));
      th.scope = "col";
      header.append(th);
    }
    headings.append(header);
    for (const cells of rows) {
      const row = el("tr");
      for (const value of cells) row.append(el("td", value == null || value === "" ? noData : value));
      body.append(row);
    }
    table.append(headings, body);
    wrapper.append(table);
    return wrapper;
  }

  let screenshotController;
  function clearScreenshot() {
    screenshotController?.abort();
    if (state.vmScreenshot) URL.revokeObjectURL(state.vmScreenshot);
    state.vmScreenshot = "";
    state.screenshotError = "";
    state.screenshotRatio = null;
    state.screenshotLoading = false;
  }

  async function loadScreenshot(id) {
    if (state.screenshotLoading) return;
    screenshotController?.abort();
    state.screenshotError = "";
    const controller = new AbortController();
    screenshotController = controller;
    state.screenshotLoading = true;
    renderList();
    try {
      const image = await api(endpoint(id, "/screenshot"), {
        signal: AbortSignal.any([controller.signal, AbortSignal.timeout(20000)]),
      }, "blob");
      const url = URL.createObjectURL(image);
      try {
        const preview = new Image();
        preview.src = url;
        await preview.decode();
        if (!controller.signal.aborted && id === state.selected) {
          if (state.vmScreenshot) URL.revokeObjectURL(state.vmScreenshot);
          state.vmScreenshot = url;
          state.screenshotRatio = Math.max(1, preview.naturalWidth / preview.naturalHeight);
        }
      } finally {
        if (state.vmScreenshot !== url) URL.revokeObjectURL(url);
      }
    } catch {
      if (!controller.signal.aborted && id === state.selected) state.screenshotError = "截图获取失败，请重试";
    } finally {
      if (!controller.signal.aborted && id === state.selected) {
        state.screenshotLoading = false;
        renderList();
      }
    }
  }

  function deviceRows(vm) {
    return [
      ["网卡", ["网卡", "端口组", "类型", "MAC 地址", "IP 地址", "已连接", "开机时连接", "地址分配"],
        vm.networks?.map((nic) => [nic.label, nic.portgroup, nic.adapter, nic.mac, nic.ipAddresses?.join("\n"), boolLabel(nic.connected), boolLabel(nic.startConnected), nic.addressType ? t(({ manual: "手动", generated: "自动生成", assigned: "集中分配" })[nic.addressType] || nic.addressType) : null]) || []],
      ["磁盘", ["磁盘", "配置容量", "文件路径", "置备方式", "磁盘模式", "控制器 / 单元", "IOPS 限制"],
        vm.disks?.map((disk) => [disk.label, gib(has(disk.capacityKB) ? disk.capacityKB * 1024 : null), disk.fileName,
          disk.thin === true ? t("精简置备") : disk.eagerlyScrub === true ? t("厚置备置零") : disk.thin === false ? t("厚置备") : noData,
          disk.mode, [disk.controllerKey ?? noData, disk.unitNumber ?? noData].join(" / "), disk.iopsLimit === -1 ? t("无限制") : disk.iopsLimit]) || []],
    ];
  }

  function deviceTables(vm) {
    return deviceRows(vm).map(([label, columns, rows]) => hardwareTable(label, columns, rows));
  }

  function deviceCards(label, columns, rows, editDevice) {
    if (!rows.length) return empty(t("无记录"));
    const list = el("ul", null, "config-device-list");
    const labels = columns.slice(1).map((column) => t(column));
    rows.forEach(([name, ...values], index) => {
      const card = el("li", null, "config-device");
      card.append(el("h4", name || t(label)), facts(labels.map((field, index) => [field, values[index]])));
      editDevice?.(card, index);
      list.append(card);
    });
    return list;
  }

  function ipDisplay(guest) {
    const addresses = [...new Set([guest?.ipAddress, ...(guest?.ipAddresses || [])].filter(Boolean))];
    if (addresses.length <= 1) return addresses[0];
    const summary = el("div", null, "overview-addresses");
    const primary = el("span", addresses[0]);
    primary.title = addresses[0];
    const more = el("button", "+" + (addresses.length - 1), "overview-ip-more");
    more.type = "button";
    more.title = t("查看全部 IP 地址");
    more.setAttribute("aria-label", t("查看全部 IP 地址"));
    more.dataset.focusKey = "vm-addresses";
    more.setAttribute("popovertarget", "vm-ip-addresses");
    const panel = el("div", null, "overview-ip-popover");
    panel.id = "vm-ip-addresses";
    panel.popover = "auto";
    panel.setAttribute("role", "region");
    panel.setAttribute("aria-label", t("IP 地址"));
    const list = el("ul");
    addresses.forEach((address) => list.append(el("li", address)));
    panel.append(el("strong", t("IP 地址")), list);
    summary.append(primary, more, panel);
    return summary;
  }

  function renderVM(id) {
    const vm = state.vm;
    const content = el("section", null, "vm-expanded");
    content.id = "vm-detail-" + id;
    content.setAttribute("aria-label", t("虚拟机详情"));
    if (!vm) {
      content.append(empty(state.detailError || t("正在读取虚拟机详情")));
      return content;
    }
    const details = el("div", null, "expanded-details");
    const overview = el("section", null, "vm-overview-card");
    const config = vm.config;
    const stats = vm.quickStatsReliable ? vm.quickStats : null;
    const video = config?.devices?.find((device) => device.type === "VirtualMachineVideoCard");
    const cores = config?.coresPerSocket;
    const sockets = cores > 0 && vm.numCPU > 0 && vm.numCPU % cores === 0 ? vm.numCPU / cores : null;
    const diskSizes = vm.disks?.map((disk) => disk.capacityKB);
    const capacity = diskSizes?.length && diskSizes.every(has) ?
      diskSizes.reduce((total, size) => total + size, 0) * 1024 : null;
    const memory = (mb) => !has(mb) || mb < 0 ? noData : mb < 1024 ? units(mb, " MiB", 0) : gib(mb * 2 ** 20);
    const groups = [
      ["系统与状态", [
        [t("电源状态"), powerLabels[vm.powerState] || vm.powerState],
        [t("连接状态"), sessionLabels[vm.connectionState] || vm.connectionState],
        [t("主机名称"), vm.guest?.hostName],
        [t("IP 地址"), ipDisplay(vm.guest)],
        [t("操作系统"), guestOSName(vm)],
        ["VMware Tools", toolsDisplay(vm.guest)],
        [t("虚拟硬件版本"), config?.version],
        [t("固件"), config?.firmware],
        ["VBS", t(vbsState(vm) === true ? "已启用" : vbsState(vm) === false ? "未启用" : "未知")],
        [t("安全启动"), boolLabel(config?.secureBoot)],
      ]],
      ["计算与内存", [
        ["vCPU", vm.numCPU || null],
        [t("CPU 插槽数"), sockets],
        [t("每插槽核心数"), cores === 0 ? t("自动") : cores],
        [t("已配置内存"), memory(vm.memoryMB)],
        [t("主机 CPU 用量"), units(stats?.overallCpuUsage >= 0 ? stats.overallCpuUsage : null, " MHz", 0)],
        [t("主机内存用量"), memory(stats?.hostMemoryUsage)],
        [t("活动内存"), memory(stats?.guestMemoryUsage)],
        [t("嵌套虚拟化"), boolLabel(config?.nestedHV)],
        [t("vCPU 热添加"), boolLabel(config?.cpuHotAddEnabled)],
        [t("内存热添加"), boolLabel(config?.memoryHotAddEnabled)],
      ]],
      ["存储与设备", [
        [t("配置容量"), gib(capacity)],
        [t("已用存储"), gib(vm.committedBytes)],
        [t("潜在新增占用"), gib(vm.uncommittedBytes), t("虚拟机可能额外占用的数据存储空间")],
        [t("未共享占用"), gib(vm.unsharedBytes)],
        [t("虚拟 IOMMU"), boolLabel(config?.iommu)],
        [t("显存 (MiB)"), has(video?.videoRAMKB) ? units(video.videoRAMKB / 1024, "", 0) : null],
        [t("显示器"), video?.displays],
        ["3D", boolLabel(video?.enable3D)],
        [t("虚拟机配置文件"), config?.vmx],
        [t("备注"), config?.annotation],
      ]],
    ];
    const columns = el("div", null, "vm-overview-columns");
    for (const [heading, rows] of groups) {
      const column = el("div");
      column.append(el("h4", t(heading)), facts(rows, true));
      columns.append(column);
    }
    overview.append(el("h3", t("概览")), columns);
    if (problem(vm)) overview.append(el("p", vm.question?.text || t("连接异常"), "notice"));
    const preview = el("section", null, "vm-preview-card");
    preview.setAttribute("aria-label", t("当前画面"));
    const frame = el("div", null, "preview-image");
    const poweredOn = vm.powerState === "poweredOn";
    if (poweredOn) {
      const refresh = button("↻", () => loadScreenshot(id), "preview-refresh");
      refresh.setAttribute("aria-label", t("刷新截图"));
      refresh.title = t("刷新截图");
      refresh.dataset.focusKey = "screenshot:" + id;
      if (state.screenshotLoading) refresh.setAttribute("aria-disabled", "true");
      frame.append(refresh);
    }
    const showingImage = poweredOn && state.vmScreenshot;
    if (showingImage) {
      const image = el("img");
      image.src = state.vmScreenshot;
      image.alt = vm.name + " · " + t("当前画面");
      frame.style.aspectRatio = state.screenshotRatio || "16 / 9";
      frame.append(image);
    }
    const message = !poweredOn ? t("开机后可查看截图") :
      state.screenshotLoading ? t(showingImage ? "正在刷新截图" : "正在获取截图") :
        (state.screenshotError && t(state.screenshotError)) || (showingImage ? "" : t("暂无截图"));
    if (message) {
      const status = el("span", message, "preview-status");
      status.setAttribute("role", "status");
      frame.append(status);
    }
    preview.append(frame);
    details.append(preview, overview);
    const [nicTable, diskTable] = deviceTables(vm);
    const devices = el("div", null, "vm-device-grid");
    for (const [title, table] of [[t("网卡"), nicTable], [t("磁盘"), diskTable]]) {
      const card = el("section", null, "vm-device-card");
      if (table.classList.contains("hardware-table-wrap")) {
        table.tabIndex = 0;
        table.setAttribute("role", "region");
        table.setAttribute("aria-label", title);
      }
      card.append(el("h3", title), table);
      devices.append(card);
    }
    details.append(devices);
    const range = state.vmRange || timeRange(15);
    const trends = el("section");
    trends.append(el("h3", t("指标趋势")));
    if (state.vmMetricError) trends.append(el("p", state.vmMetricError, "notice"));
    if (state.vmMetrics || !state.vmMetricLoading) {
      const stack = el("div", null, "charts");
      charts(stack, state.vmMetrics, range);
      trends.append(stack);
    } else {
      trends.append(empty(t("正在读取指标")));
    }
    content.append(details, trends);
    return content;
  }

  let detailController;
  async function loadVM(id) {
    detailController?.abort();
    const controller = new AbortController();
    detailController = controller;
    const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(35000)]);
    const request = ++state.detailRequest;
    const current = () => request === state.detailRequest && id === state.selected;
    const detail = api(endpoint(id), { signal }).then((vm) => {
      if (!current()) return;
      state.detailError = "";
      state.vm = vm;
      renderList();
    }, (error) => {
      if (!current()) return;
      state.detailError = error.message;
      renderList();
    });
    const trend = fetchTrend(endpoint(id, "/metrics"), 15, "virtualDisk", { signal }).then((metrics) => {
      if (!current()) return;
      state.vmMetricLoading = false;
      state.vmMetricError = "";
      state.vmMetrics = metrics;
      state.vmRange = { ...sampleRange(metrics, 15), diskPrefix: "virtualDisk" };
      renderList();
    }, (error) => {
      if (!current()) return;
      state.vmMetricLoading = false;
      state.vmMetricError = error.message;
      renderList();
    });
    await Promise.all([detail, trend]);
  }

  function closeVM() {
    clearScreenshot();
    state.selected = null;
    state.detailRequest++;
    detailController?.abort();
  }

  function openVM(id) {
    if (state.selected === id) {
      closeVM();
      renderList();
      return;
    }
    clearScreenshot();
    state.selected = id;
    state.vm = null;
    state.detailError = "";
    state.vmMetrics = null;
    state.vmRange = null;
    state.vmMetricError = "";
    state.vmMetricLoading = true;
    renderList();
    loadVM(id);
    if (state.vms?.some((vm) => vm.id === id && vm.powerState === "poweredOn")) loadScreenshot(id);
  }

  function boolLabel(value) { return value === true ? t("开启") : value === false ? t("已关闭") : t("未知"); }

  let action = null;
  function openAction(title, vm, fields, run, { drawer = false } = {}) {
    if (state.switchingEnvironment) return;
    action = { vm, run, epoch: state.environmentEpoch };
    $("action-title").textContent = title;
    $("action-target").textContent = (state.host?.name || "ESXi") + " / " + vm.name + " (" + vm.id + ")";
    $("action-fields").replaceChildren(fields);
    $("action-error").textContent = "";
    $("action-submit").textContent = t("确认");
    $("action-submit").disabled = false;
    $("action-form").noValidate = drawer;
    $("action-dialog").classList.toggle("config-drawer", drawer);
    $("action-dialog").showModal();
  }

  function confirmPower(vm, name) {
    const hint = name === "off" || name === "reset" ? t("此操作会中断虚拟机，未保存的数据可能丢失。") : "";
    openAction(operations[name], vm, el("p", hint), async () => {
      await api(endpoint(vm.id, "/power"), { method: "POST", body: JSON.stringify({ action: name, confirm: true }) });
    });
  }

  function editVM(vm) {
    const panel = el("div", null, "config-panel");
    const old = { name: vm.name, numCPU: vm.numCPU, memoryMB: vm.memoryMB, ...vm.config };
    const online = vm.powerState === "poweredOn";
    const deviceEditable = (target) => target.config?.deviceEditable === true && ["poweredOn", "poweredOff"].includes(target.powerState);
    const hardwareKeys = new Set(["expandDisks", "addDiskMiB", "editNICs", "addNIC"]);
    const hardwareEdits = [];
    const legacyFields = ["numCPU", "memoryMB", "nestedHV", "iommu"];
    const supportsConfigEdit = (target) => target.config?.editable === true || !!target.config?.changeVersion;
    const canEdit = (target, key) => {
      if (hardwareKeys.has(key)) return deviceEditable(target);
      if (target.powerState === "poweredOff") return legacyFields.includes(key) || supportsConfigEdit(target);
      return target.powerState === "poweredOn" && (key === "numCPU" ? target.config?.cpuHotAddEnabled === true :
        key === "memoryMB" && target.config?.memoryHotAddEnabled === true);
    };
    const controls = [];
    const diff = el("div", null, "diff config-diff");
    let reviewed = false;
    const tabs = el("div", null, "config-tabs");
    const hardware = el("div", null, "config-sections");
    const options = el("div", null, "config-sections");
    const segments = el("div", null, "config-tab-segments");
    function tab(label, isHardware, path) {
      const control = button(t(label), () => switchTab(isHardware));
      const namespace = "http://www.w3.org/2000/svg";
      const icon = document.createElementNS(namespace, "svg");
      icon.setAttribute("viewBox", "0 0 24 24");
      icon.setAttribute("aria-hidden", "true");
      const shape = document.createElementNS(namespace, "path");
      shape.setAttribute("d", path);
      icon.append(shape);
      control.prepend(icon);
      return control;
    }
    const hardwareTab = tab("虚拟硬件", true,
      "M8 3v3m4-3v3m4-3v3M8 18v3m4-3v3m4-3v3M3 8h3m-3 4h3m-3 4h3m12-8h3m-3 4h3m-3 4h3M6 6h12v12H6z");
    const optionsTab = tab("虚拟机选项", false,
      "M4 7h7m4 0h5M4 17h3m4 0h9M11 4v6m4-6v6M7 14v6m4-6v6");
    function switchTab(isHardware) {
      hardware.hidden = !isHardware;
      options.hidden = isHardware;
      hardwareTab.setAttribute("aria-pressed", String(isHardware));
      optionsTab.setAttribute("aria-pressed", String(!isHardware));
    }
    segments.append(hardwareTab, optionsTab);
    tabs.append(segments);
    panel.append(tabs);
    switchTab(true);
    const hint = online && deviceEditable(vm) ?
      "设备支持在线变更时可在运行中编辑。CPU 和内存仅可增加已启用热添加的资源。" : online ? (canEdit(vm, "numCPU") || canEdit(vm, "memoryMB") ?
      "运行时仅可增加已开启热添加的 vCPU 或内存。其他设置需要关机。" :
      "当前虚拟机正在运行，且未启用可用的热添加。请关机后编辑配置。") :
      vm.powerState !== "poweredOff" ? "当前电源状态仅可查看配置。请关机后编辑。" :
        !supportsConfigEdit(vm) ? "更新 govmc 服务后可编辑更多字段" : "展开模块编辑配置，保存前可预览变更。";
    panel.append(el("p", t(hint), "config-hint"));
    function invalidatePreview() {
      reviewed = false;
      diff.replaceChildren();
      $("action-error").textContent = "";
      $("action-submit").textContent = t("预览更改");
    }
    function group(parent, title, fields, readOnly = [], expanded = false, editableDevices = false) {
      const module = el("details", null, "config-group");
      module.open = expanded;
      const summary = el("summary", null, "config-module-heading");
      const editable = fields.some(([key]) => canEdit(vm, key));
      summary.append(el("strong", t(title)), el("span", t(editable || editableDevices ? "可编辑" : "只读"),
        "state-chip" + (editable || editableDevices ? " online" : "")));
      const section = el("div", null, "config-module-body");
      module.append(summary, section);
      const grid = el("div", null, "edit-fields");
      const locked = [];
      for (const [key, label, type, min = 1] of fields) {
        if (!canEdit(vm, key)) {
          locked.push([t(label), type === "boolean" ? boolLabel(old[key]) : old[key]]);
          continue;
        }
        const row = el("label", t(label));
        const input = el(type === "boolean" ? "select" : "input");
        if (type === "boolean") {
          for (const [value, text] of [["", boolLabel(old[key]) + t("（保持）")], ["true", t("开启")], ["false", t("已关闭")]]) {
            const option = el("option", text);
            option.value = value;
            input.append(option);
          }
        } else {
          input.type = type;
          if (type === "number") {
            input.min = online && has(old[key]) ? String(old[key]) : String(min);
            input.max = "2147483647";
            input.step = "1";
          }
          if (type === "text") { input.maxLength = 80; input.required = true; }
          input.value = old[key] ?? "";
        }
        input.addEventListener("input", invalidatePreview);
        row.append(input);
        grid.append(row);
        controls.push([key, t(label), type, input]);
      }
      if (editable) section.append(grid);
      if (locked.length || readOnly.length) section.append(facts([...locked, ...readOnly]));
      parent.append(module);
      return section;
    }
    group(hardware, "CPU", [
      ["numCPU", "vCPU", "number"],
      ["coresPerSocket", "每插槽核心数（0 为自动）", "number", 0],
      ["cpuHotAddEnabled", "vCPU 热添加", "boolean"],
      ["cpuReservationMHz", "CPU 预留 (MHz)", "number", 0],
      ["cpuLimitMHz", "CPU 限制 (MHz，-1 为不限)", "number", -1],
      ["nestedHV", "嵌套虚拟化", "boolean"],
      ["iommu", "虚拟 IOMMU", "boolean"],
      ["vpmc", "CPU 性能计数器", "boolean"],
    ], [[t("CPU 份额"), vm.config?.cpuShares]], true);
    group(hardware, "内存", [
      ["memoryMB", "内存 (MiB)", "number"],
      ["memoryHotAddEnabled", "内存热添加", "boolean"],
      ["memoryReservationMB", "内存预留 (MiB)", "number", 0],
      ["memoryLimitMB", "内存限制 (MiB，-1 为不限)", "number", -1],
    ], [[t("内存份额"), vm.config?.memoryShares], [t("预留所有来宾内存"), boolLabel(vm.config?.memoryLocked)]], true);
    const portgroups = [...new Set((state.host?.portgroups || []).map((network) => network.name).filter(Boolean))].sort();
    function field(grid, label, input) {
      const row = el("label", t(label));
      input.setAttribute("aria-label", t(label));
      row.append(input);
      grid.append(row);
      input.addEventListener("input", invalidatePreview);
      return input;
    }
    function select(choices) {
      const input = el("select");
      for (const [value, label] of choices) {
        const option = el("option", label);
        option.value = value;
        input.append(option);
      }
      return input;
    }
    const portgroupChoices = portgroups.map((name) => [name, name]);
    function check(grid, label, checked) {
      const input = el("input");
      input.type = "checkbox";
      input.checked = checked;
      return field(grid, label, input);
    }
    function sizeInput(grid, min) {
      const input = el("input");
      input.type = "number";
      input.min = String(min);
      input.max = String(64 * 1024 * 1024);
      input.step = "1";
      return field(grid, "新容量 (MiB)", input);
    }
    function valueOf(input) {
      if (!input.checkValidity()) {
        switchTab(true);
        input.closest("details").open = true;
        input.focus();
        throw new Error(input.validationMessage);
      }
      return Number(input.value);
    }
    const resizeDisk = (card, index) => {
      const disk = vm.disks[index];
      const controller = vm.config?.devices?.find((device) => device.key === disk.controllerKey);
      if (!has(disk.key) || !has(disk.capacityKB) || (online &&
          (!["ParaVirtualSCSIController", "VirtualNVMEController"].includes(controller?.type) || disk.mode !== "persistent"))) return;
      const grid = el("div", null, "edit-fields device-fields");
      const size = sizeInput(grid, Math.floor(disk.capacityKB / 1024) + 1);
      card.append(grid);
      hardwareEdits.push((changes, rows) => {
        if (!size.value) return;
        const capacityMiB = valueOf(size);
        (changes.expandDisks ||= []).push({ key: disk.key, capacityMiB });
        rows.push([disk.label, gib(disk.capacityKB * 1024) + " → " + gib(capacityMiB * 2 ** 20)]);
      });
    };
    const editNIC = (card, index) => {
      const nic = vm.networks[index];
      if (!has(nic.key)) return;
      const grid = el("div", null, "edit-fields device-fields");
      const network = portgroups.length ? field(grid, "端口组",
        select([["", t("保持当前端口组")], ...portgroupChoices.filter(([name]) => name !== nic.portgroup)])) : null;
      const live = online && typeof nic.connected === "boolean" ? check(grid, "当前连接", nic.connected) : null;
      const start = typeof nic.startConnected === "boolean" ? check(grid, "开机时连接", nic.startConnected) : null;
      if (!grid.childElementCount) return;
      card.append(grid);
      hardwareEdits.push((changes, rows) => {
        const patch = { key: nic.key };
        if (network?.value) {
          patch.portgroup = network.value;
          rows.push([nic.label + " · " + t("端口组"), (nic.portgroup || noData) + " → " + network.value]);
        }
        for (const [input, key, label] of [[live, "connected", "当前连接"], [start, "startConnected", "开机时连接"]]) {
          if (!input || input.checked === nic[key]) continue;
          patch[key] = input.checked;
          rows.push([nic.label + " · " + t(label), boolLabel(nic[key]) + " → " + boolLabel(input.checked)]);
        }
        if (Object.keys(patch).length > 1) (changes.editNICs ||= []).push(patch);
      });
    };
    for (const [label, columns, rows] of deviceRows(vm).reverse()) {
      const disks = label === "磁盘";
      const enabled = deviceEditable(vm);
      const section = group(hardware, label, [], [], false, enabled);
      section.append(deviceCards(label, columns, rows, enabled ? (disks ? resizeDisk : editNIC) : null));
      if (!enabled || (!disks && !portgroups.length)) continue;
      const add = el("section", null, "config-device config-new-device");
      const title = t(disks ? "添加磁盘" : "添加网卡");
      const grid = el("div", null, "edit-fields device-fields");
      add.append(el("h4", title), grid);
      section.append(add);
      if (disks) {
        const size = sizeInput(grid, 1);
        grid.append(el("p", t("新磁盘使用虚拟机所在的数据存储和现有 SCSI 控制器。"), "muted"));
        hardwareEdits.push((changes, rows) => {
          if (!size.value) return;
          changes.addDiskMiB = valueOf(size);
          rows.push([title, gib(changes.addDiskMiB * 2 ** 20)]);
        });
      } else {
        const network = field(grid, "端口组", select([["", t("选择端口组")], ...portgroupChoices]));
        const adapter = field(grid, "类型", select([["vmxnet3", "VMXNET 3"], ["e1000e", "E1000e"]]));
        const start = check(grid, "开机时连接", true);
        hardwareEdits.push((changes, rows) => {
          if (!network.value) return;
          changes.addNIC = { portgroup: network.value, adapter: adapter.value, startConnected: start.checked };
          rows.push([title, [network.value, adapter.selectedOptions[0].textContent, t("开机时连接") + " " + boolLabel(start.checked)].join(" · ")]);
        });
      }
    }
    const other = vm.config?.devices?.filter((device) => ![...(vm.disks || []), ...(vm.networks || [])].some((item) => item.key === device.key));
    if (other?.length) group(hardware, "其他设备", []).append(
      hardwareTable("其他设备", ["名称", "类型", "显示器", "显存 (MiB)", "3D"], other.map((device) => [device.label, device.type, device.displays, has(device.videoRAMKB) ? units(device.videoRAMKB / 1024, "") : null, device.enable3D == null ? null : boolLabel(device.enable3D)])));
    group(options, "常规选项", [["name", "虚拟机名称", "text"]], [
      [t("操作系统"), osDisplay(vm)], [t("虚拟硬件版本"), vm.config?.version],
      [t("虚拟机配置文件"), vm.config?.vmx], [t("挂起目录"), vm.config?.suspendDirectory], [t("备注"), vm.config?.annotation],
    ], true);
    group(options, "控制台选项", [], [
      [t("会话上限"), vm.config?.maxMksConnections], [t("断开后锁定来宾"), boolLabel(vm.config?.guestAutoLock)],
    ]);
    group(options, "引导与安全", [], [
      [t("固件"), vm.config?.firmware], [t("安全启动"), boolLabel(vm.config?.secureBoot)], ["VBS", boolLabel(vm.config?.vbs)], [t("引导延迟 (ms)"), vm.config?.bootDelayMS], [t("引导重试"), boolLabel(vm.config?.bootRetry)],
    ]);
    group(options, "VMware Tools 与电源管理", [], [[t("Tools 升级策略"), vm.config?.toolsUpgradePolicy], [t("待机动作"), vm.config?.standbyAction]]);
    panel.append(hardware, options, diff);
    openAction(t("编辑配置"), vm, panel, async () => {
      const changes = {}, rows = [];
      for (const [key, label, type, input] of controls) {
        if (!input.checkValidity()) {
          switchTab(hardware.contains(input));
          input.closest("details").open = true;
          input.focus();
          throw new Error(label + " · " + input.validationMessage);
        }
        if (!input.value) continue;
        const value = type === "number" ? Number(input.value) : type === "boolean" ? input.value === "true" : input.value.trim();
        if (type === "text" && !value) throw new Error(t("虚拟机名称不能为空"));
        if (value === old[key]) continue;
        changes[key] = value;
        const before = type === "boolean" ? boolLabel(old[key]) : old[key] ?? noData;
        rows.push([label, before + " → " + (type === "boolean" ? boolLabel(value) : value)]);
      }
      hardwareEdits.forEach((collect) => collect(changes, rows));
      if (!rows.length) throw new Error(t("没有配置变更"));
      if (!reviewed) {
        diff.replaceChildren(facts(rows));
        diff.scrollIntoView({ block: "nearest" });
        $("action-submit").textContent = t("应用更改");
        reviewed = true;
        return false;
      }
      const latest = await api(endpoint(vm.id));
      if (latest.powerState !== vm.powerState || Object.keys(changes).some((key) => !canEdit(latest, key))) {
        throw new Error(t("电源状态或热添加能力已变化，请关闭窗口并重新编辑"));
      }
      const fresh = { name: latest.name, numCPU: latest.numCPU, memoryMB: latest.memoryMB, ...latest.config };
      if ((old.changeVersion && old.changeVersion !== fresh.changeVersion) ||
          Object.keys(changes).some((key) => !hardwareKeys.has(key) && fresh[key] !== old[key])) {
        throw new Error(t("配置已变化，请关闭窗口并重新编辑"));
      }
      await api(endpoint(vm.id), { method: "PATCH", body: JSON.stringify({
        ...changes, changeVersion: fresh.changeVersion, confirm: true,
        ...(online ? { requireHotAdd: true } : { requirePoweredOff: true }),
      }) });
    }, { drawer: true });
    $("action-submit").textContent = t("预览更改");
    $("action-submit").disabled = controls.length === 0 && hardwareEdits.length === 0;
  }

  async function updateVMState(id) {
    const latest = await api(endpoint(id));
    if (state.vms) state.vms = state.vms.map((vm) => vm.id === id ? (latest.powerState === "poweredOn" && vm.powerState === "poweredOn" ? { ...vm, ...latest } : latest) : vm);
    if (state.selected === id) state.vm = latest;
    const session = state.sessions.get(id);
    if (session) session.vm = latest;
    renderOverview();
    renderList();
    renderSessions();
  }

  let consoleRestorePending = true;
  let pageExiting = false;
  let allowPageExit = false;
  const liveSessions = () => [...state.sessions.values()].some(session => ["connecting", "connected"].includes(session.state));
  const consoleHistoryKey = () => "govmc-console:v1:" + JSON.stringify([state.environmentInstance, state.environment]);
  function rememberConsoleSessions() {
    if (!state.environmentReady || consoleRestorePending || pageExiting) return;
    const sessions = [...state.sessions.values()].filter(session => ["connecting", "connected"].includes(session.state)).slice(0,6)
      .map(({vm}) => ({id:vm.id,name:vm.name,uuid:vm.uuid || vm.instanceUuid || ""}));
    try {
      if (!sessions.length) sessionStorage.removeItem(consoleHistoryKey());
      else sessionStorage.setItem(consoleHistoryKey(),JSON.stringify({sessions,active:state.active,savedAt:Date.now()}));
    } catch {}
  }
  async function restoreConsoleSessions() {
    if (!consoleRestorePending || !state.environmentReady || state.snapshotPending || !state.vms) return;
    consoleRestorePending = false;
    let saved;
    try { saved=JSON.parse(sessionStorage.getItem(consoleHistoryKey())); } catch {}
    if (!saved || !Array.isArray(saved.sessions) || saved.sessions.length>6 || !Number.isFinite(saved.savedAt) || Date.now()-saved.savedAt>12*60*60*1000) return;
    const epoch=state.environmentEpoch;
    for (const item of saved.sessions) {
      if (epoch!==state.environmentEpoch || state.switchingEnvironment) return;
      const vm=state.vms.find(vm=>vm.id===item?.id && vm.name===item.name && (!item.uuid || (vm.uuid || vm.instanceUuid)===item.uuid));
      if (!vm || vm.powerState!=="poweredOn") continue;
      await openSession(vm,true);
    }
    if (epoch===state.environmentEpoch && state.sessions.has(saved.active)) activate(saved.active);
    rememberConsoleSessions();
  }
  window.addEventListener("beforeunload", event=>{
    if (allowPageExit || !liveSessions()) return;
    rememberConsoleSessions();
    event.preventDefault();
    event.returnValue=true;
  });
  window.addEventListener("pagehide",()=>{rememberConsoleSessions();pageExiting=true;});
  window.addEventListener("pageshow",event=>{if(event.persisted){
    pageExiting=false;allowPageExit=false;consoleRestorePending=true;
    for (const session of state.sessions.values()) {clearTimeout(session.timeout);session.frame.remove();}
    state.sessions.clear();state.active=null;refresh();
  }});
  const leaveConsoleDialog = $("leave-console-confirm");
  let pendingPageAction;
  let historyIndex = Number.isInteger(history.state?.govmcPageIndex) ? history.state.govmcPageIndex : 0;
  let restoringHistory = false;
  let approvedHistory = false;
  let pendingHistoryConfirmation;
  history.replaceState({...history.state, govmcPageIndex: historyIndex}, "", location.href);
  function confirmPageLeave(action, sameDocument = false) {
    if (leaveConsoleDialog.open) return;
    pendingPageAction = action;
    $("leave-console-detail").textContent = t(sameDocument ? "console.leave_background" : "console.leave_detail");
    leaveConsoleDialog.returnValue = "";
    leaveConsoleDialog.showModal();
  }
  function sameWorkspace(url) {
    return url.origin === location.origin && url.pathname === location.pathname && url.search === location.search;
  }
  function workspacePage(url) { return url.hash === "#console" ? "console" : "overview"; }
  function visitWorkspace(url) {
    history.pushState({...history.state, govmcPageIndex: ++historyIndex}, "", url);
    navigate(workspacePage(new URL(url, location.href)));
  }
  document.addEventListener("click",event=>{
    const link=event.target.closest("a[href]");
    if (!link || event.defaultPrevented || event.button!==0 || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey || (link.target && link.target!=="_self") || link.hasAttribute("download")) return;
    const url=new URL(link.href,location.href);
    if (!["http:", "https:"].includes(url.protocol)) return;
    const internal = sameWorkspace(url);
    if (internal) {
      event.preventDefault();
      if (workspacePage(url) === state.page) return;
      if (state.page === "console" && liveSessions()) confirmPageLeave(()=>visitWorkspace(url.href), true);
      else visitWorkspace(url.href);
      return;
    }
    if (!liveSessions()) return;
    event.preventDefault();
    confirmPageLeave(()=>{
      rememberConsoleSessions();allowPageExit=true;location.assign(url.href);
    });
  }, true);
  // Consume the action synchronously. The dialog's close event is queued and may
  // arrive after a second navigation has already opened another confirmation.
  leaveConsoleDialog.querySelector("form").addEventListener("submit",event=>{
    event.preventDefault();
    const action=pendingPageAction;pendingPageAction=null;
    const result=event.submitter?.value || "cancel";
    leaveConsoleDialog.close(result);
    if (result==="leave") action?.();
  });
  leaveConsoleDialog.addEventListener("cancel",()=>{pendingPageAction=null;});

  function send(session, type, data = {}) {
    if (!session.ready) return;
    session.frame.contentWindow.postMessage({ source: "govmc-workbench", type, ...data }, location.origin);
  }

  function activate(id) {
    state.active = id;
    renderSessions();
  }

  async function connect(session) {
    if (!session.ready || session.requesting) return;
    session.requesting = true;
    session.state = "connecting";
    send(session, "machine", { id: session.vm.id, name: session.vm.name });
    renderSessions();
    try {
      const { ws } = await api(endpoint(session.vm.id, "/console"), { method: "POST" });
      if (state.sessions.get(session.vm.id) !== session) return;
      send(session, "connect", { ws, id: session.vm.id, name: session.vm.name });
    } catch (error) {
      session.state = "error";
      state.sessionStates.set(session.vm.id, "error");
      send(session, "ticket-error", { message: error.message });
    } finally {
      session.requesting = false;
      if (state.sessions.get(session.vm.id) === session) renderSessions();
    }
  }

  async function openSession(vm, background = false) {
    if (!background) navigate("console");
    const epoch = state.environmentEpoch;
    const capability = await window.govmcWMKS.check();
    if (epoch !== state.environmentEpoch) return;
    state.consoleSDK = capability;
    if (!capability.available) { renderSessions(); return; }
    const existing = state.sessions.get(vm.id);
    if (existing) {
      activate(vm.id);
      if (!background) navigate("console");
      if (["disconnected", "error"].includes(existing.state) && !existing.requesting) connect(existing);
      return;
    }
    for (const [id, session] of state.sessions) {
      if (!session.requesting && ["disconnected", "error"].includes(session.state)) {
        state.sessions.delete(id);
        send(session, "disconnect");
        session.frame.remove();
      }
    }
    if (state.sessions.size >= 6) { notice(t("已达到 6 个会话，请先断开一个会话")); return; }
    const frame = el("iframe", null, "console-frame");
    frame.title = t("控制台：") + vm.name;
    frame.hidden = true;
    const session = { vm, frame, ready: false, state: "connecting" };
    state.sessions.set(vm.id, session);
    frame.src = "/console.html?lang=" + getLanguage();
    session.timeout = setTimeout(() => {
      if (state.sessions.get(vm.id)!==session || session.ready) return;
      state.sessions.delete(vm.id); frame.remove(); state.consoleSDK={state:"ready",available:false}; renderSessions();
    }, 20000);
    $("session-stage").append(frame);
    activate(vm.id);
    if (!background) navigate("console");
  }

  function renderConsoleMachines() {
    const term = $("console-search").value.trim().toLocaleLowerCase();
    const container = $("console-machines"), pager = $("console-pages");
    const focused = container.contains(document.activeElement) ? document.activeElement.dataset.vm : "";
    const items = state.vms?.filter((vm) => vm.powerState === "poweredOn" &&
      (vm.name + " " + vm.id).toLocaleLowerCase().includes(term)) || [];
    const perPage = 10;
    const pages = Math.max(1, Math.ceil(items.length / perPage));
    state.consolePage = Math.min(pages, state.consolePage);
    const list = el("ul", null, "rows");
    for (const vm of items.slice((state.consolePage - 1) * perPage, state.consolePage * perPage)) {
      const row = el("li");
      const control = button("", () => openSession(vm), "vm-row",state.snapshotPending);
      control.dataset.vm = vm.id;
      control.setAttribute("aria-pressed", String(state.active === vm.id));
      const identity = el("span", null, "identity");
      const specs = [has(vm.numCPU) ? vm.numCPU + " vCPU" : noData,
        units(has(vm.memoryMB) ? vm.memoryMB / 1024 : null, " GiB")].join(" / ");
      const meta = el("small", null, "console-machine-meta");
      meta.append(el("span", t("运行中"), "console-running"), el("span", specs));
      identity.append(el("strong", vm.name), meta);
      control.append(identity);
      const connection = state.sessions.get(vm.id)?.state || state.sessionStates.get(vm.id);
      if (["connected", "disconnected", "error"].includes(connection)) {
        control.append(el("span", sessionLabels[connection], "state-chip" + (connection === "connected" ? " online" : "")));
      }
      row.append(control);
      list.append(row);
    }
    container.replaceChildren(items.length ? list : empty(t(state.vms ? "没有符合条件的虚拟机" : "正在读取虚拟机")));
    if (focused) [...container.querySelectorAll("button")].find((b) => b.dataset.vm === focused)?.focus({ preventScroll: true });
    const pageFocus = pager.contains(document.activeElement) ? document.activeElement.dataset.step : "";
    pager.hidden = pages < 2;
    pager.replaceChildren();
    if (pages > 1) {
      const step = (label, amount, disabled) => {
        const control = button(t(label), () => { state.consolePage += amount; renderConsoleMachines(); }, "", disabled);
        control.dataset.step = String(amount);
        return control;
      };
      pager.append(step("上一页", -1, state.consolePage === 1),
        el("span", state.consolePage + " / " + pages, "muted"), step("下一页", 1, state.consolePage === pages));
      if (pageFocus) [...pager.querySelectorAll("button")].find((b) => b.dataset.step === pageFocus && !b.disabled)?.focus();
    }
  }

  const disconnectDialog = $("disconnect-confirm");
  let pendingDisconnect;
  function confirmDisconnect(session) {
    if (disconnectDialog.open || state.sessions.get(session.vm.id) !== session || session.state !== "connected") return;
    pendingDisconnect = session;
    $("disconnect-target").textContent = session.vm.name;
    disconnectDialog.returnValue = "";
    disconnectDialog.showModal();
  }
  disconnectDialog.addEventListener("close", () => {
    const session = pendingDisconnect;
    pendingDisconnect = null;
    if (disconnectDialog.returnValue === "disconnect" &&
        state.sessions.get(session?.vm.id) === session && session?.state === "connected") {
      send(session, "disconnect");
    }
  });

  function renderSessions() {
    rememberConsoleSessions();
    renderConsoleMachines();
    const connected = $("connected-sessions"), stage = $("session-stage");
    const focused = connected.contains(document.activeElement) ? document.activeElement.dataset.vm : "";
    const disconnectFocused = connected.contains(document.activeElement) ? document.activeElement.dataset.disconnectVM : "";
    const list = el("ul", null, "rows");
    for (const [id, session] of state.sessions) {
      if (session.state === "connected") {
        const row = el("li");
        const select = button("", () => activate(id), "vm-row connected-row");
        select.dataset.vm = id;
        select.setAttribute("aria-pressed", String(id === state.active));
        const dot = el("i", null, "session-dot connected");
        dot.setAttribute("aria-hidden", "true");
        select.append(dot, el("strong", session.vm.name, "identity"), el("span", t("已连接"), "state-chip online"));
        const disconnect = button("×", () => confirmDisconnect(session), "session-disconnect");
        const label = t("断开与 {name} 的连接", { name: session.vm.name });
        disconnect.setAttribute("aria-label", label);
        disconnect.title = label;
        disconnect.dataset.disconnectVM = id;
        row.className = "connected-session";
        row.append(select, disconnect);
        list.append(row);
      }
      const active = id === state.active && state.page === "console" && !document.hidden;
      if (session.active !== active) {
        session.active = active;
        send(session, "active", { active });
      }
      session.frame.hidden = !active;
    }
    connected.replaceChildren(list.childElementCount ? list : empty(t("暂无连接会话")));
    if (focused) [...connected.querySelectorAll("button")].find((b) => b.dataset.vm === focused)?.focus({ preventScroll: true });
    if (disconnectFocused) {
      const control = [...connected.querySelectorAll("button")].find((b) => b.dataset.disconnectVM === disconnectFocused) ||
        [...$("console-machines").querySelectorAll("button")].find((b) => b.dataset.vm === disconnectFocused);
      control?.focus({ preventScroll: true });
    }
    stage.querySelector(":scope > .empty")?.remove();
    stage.querySelector(":scope > .wmks-guide")?.remove();
    if (state.page === "console" && !state.consoleSDK && !state.consoleChecking) {
      state.consoleChecking=true;
      window.govmcWMKS.check().then(value=>{state.consoleChecking=false;state.consoleSDK=value;renderSessions();});
    }
    if (!state.sessions.size && state.page === "console" && state.consoleSDK && !state.consoleSDK.available) {
      window.govmcWMKS.guide(stage,state.consoleSDK,value=>{state.consoleSDK=value;renderSessions();}); return;
    }
    if (!state.sessions.has(state.active)) {
      const placeholder=empty("");
      placeholder.classList.add("console-empty");
      const icon=document.querySelector('[data-page="console"] svg').cloneNode(true);
      icon.classList.add("console-empty-icon");
      icon.setAttribute("focusable","false");
      placeholder.append(icon,el("span",t("选择运行中的虚拟机，打开远程控制台")));
      stage.append(placeholder);
    }
  }

  function navigate(page) {
    const changed = state.page !== page;
    state.page = page;
    window.govmcI18n.setPageTitle(page === "console" ? "控制台" : "总览");
    if (location.hash !== "#" + page) history.replaceState(null, "", "#" + page);
    for (const key of ["overview", "console"]) {
      const section = $(key);
      section.hidden = key !== page;
      section.classList.toggle("view-enter", changed && key === page);
      const link = document.querySelector('[data-page="' + key + '"]');
      if (!link) continue;
      if (key === page) link.setAttribute("aria-current", "page");
      else link.removeAttribute("aria-current");
    }
    if (changed) window.scrollTo(0, 0);
    $("menu").setAttribute("aria-expanded", "false");
    $("nav").classList.remove("open");
    renderSessions();
    if (changed) refresh();
  }

  window.addEventListener("message", (event) => {
    if (event.origin !== location.origin || event.data?.source !== "govmc-console") return;
    const session = [...state.sessions.values()].find((item) => item.frame.contentWindow === event.source);
    if (!session) return;
    if (event.data.state === "disconnect-request") {
      if (state.active === session.vm.id) confirmDisconnect(session);
      return;
    }
    if (event.data.state === "reconnect") {
      if (state.active === session.vm.id && ["disconnected", "error"].includes(session.state) && !session.requesting) connect(session);
      return;
    }
    if (event.data.state === "sdk-error") {
      clearTimeout(session.timeout);state.sessions.delete(session.vm.id);session.frame.remove();
      state.consoleSDK={state:"ready",available:false};renderSessions();return;
    }
    if (event.data.state === "ready" && !session.ready) {
      clearTimeout(session.timeout);
      session.ready = true;
      send(session, "appearance", { language: getLanguage(), theme });
      send(session, "active", { active: session.active });
      connect(session);
    } else if (Object.hasOwn(sessionLabels, event.data.state) && session.state !== event.data.state) {
      session.state = event.data.state;
      if (session.state !== "connecting") state.sessionStates.set(session.vm.id, session.state);
      renderSessions();
    }
  });
  const repositionPowerMenu = () => positionOpenPowerMenu?.();
  window.addEventListener("scroll", repositionPowerMenu, { capture: true, passive: true });
  window.addEventListener("resize", repositionPowerMenu);
  document.querySelectorAll("dialog [data-close]").forEach((b) => b.addEventListener("click", () => b.closest("dialog").close()));
  $("token-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    state.token = $("token-input").value.trim();
 try {sessionStorage.setItem("govmc-api-token",state.token);}catch {}
    $("token-input").value = "";
    $("token-dialog").close();
    await loadEnvironments();
    refresh();
  });
  $("action-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    if (!action || action.epoch !== state.environmentEpoch || state.switchingEnvironment || state.busy.has(action.vm.id)) return;
    const pending = action;
    $("action-error").textContent = "";
    $("action-submit").disabled = true;
    state.busy.add(pending.vm.id);
    try {
      if (await pending.run() === false || pending.epoch !== state.environmentEpoch) return;
      $("action-dialog").close();
      notice(t("操作完成"));
      await updateVMState(pending.vm.id);
    } catch (error) {
      if (pending.epoch !== state.environmentEpoch) return;
      $("action-error").textContent = error.message;
      try { await updateVMState(pending.vm.id); } catch { notice(t("无法读回虚拟机状态，请先刷新")); }
    } finally {
      if (pending.epoch !== state.environmentEpoch) return;
      state.busy.delete(pending.vm.id);
      $("action-submit").disabled = false;
      renderList();
      renderSessions();
    }
  });
  $("action-dialog").addEventListener("cancel", (event) => {
    if (action && state.busy.has(action.vm.id)) event.preventDefault();
  });
  $("action-dialog").querySelector("[data-close]").addEventListener("click", (event) => {
    if (action && state.busy.has(action.vm.id)) event.stopImmediatePropagation();
  }, true);
  $("host-range").addEventListener("change", async () => {
    state.metrics = null;
    state.hostRange = null;
    renderOverview();
    try { await updateHostMetrics(); renderOverview(); } catch (error) { notice(error.message); }
  });
  $("refresh").addEventListener("click", refresh);
  $("search").addEventListener("input", () => {
    state.inventoryPage = 1;
    renderList();
  });
  $("inventory-prev").addEventListener("click", () => { state.inventoryPage--; renderList(); });
  $("inventory-next").addEventListener("click", () => { state.inventoryPage++; renderList(); });
  $("console-search").addEventListener("input", () => { state.consolePage = 1; renderConsoleMachines(); });
  const save = (key, value) => { try { localStorage.setItem(key, value); } catch {} };
  const saved = (key) => { try { return localStorage.getItem(key); } catch { return null; } };
  let theme = window.govmcTheme.read();
  const languagePicker = $("language");
  const languageTrigger = languagePicker.querySelector("summary");
  function fullTimestamp(date) {
    const pad = (value) => String(value).padStart(2, "0");
    const calendar = [date.getFullYear(), pad(date.getMonth() + 1), pad(date.getDate())].join("-");
    const clock = [date.getHours(), date.getMinutes(), date.getSeconds()].map(pad).join(":");
    return calendar + " " + clock;
  }
  function renderUpdated() {
    if (!state.updated) return;
    $("updated").dateTime = state.updated.toISOString();
    $("updated").textContent = fullTimestamp(state.updated);
    $("updated").parentElement.hidden = false;
  }
  function appearance() {
    renderEnvironments();
    window.govmcTheme.apply(theme);
    languagePicker.querySelectorAll("button").forEach((button) =>
      button.setAttribute("aria-pressed", String(button.dataset.language === getLanguage())));
    for (const session of state.sessions.values()) {
      session.frame.title = t("控制台：") + session.vm.name;
      send(session, "appearance", { language: getLanguage(), theme });
    }
  }
  $("theme").addEventListener("click", (event) => {
    const button = event.target.closest("button[data-theme-option]");
    if (!button) return;
    theme = button.dataset.themeOption;
    save("govmc-theme", theme);
    appearance();
  });
  window.addEventListener("storage", event => {
    if (event.key === "govmc-theme") { theme = window.govmcTheme.normalize(event.newValue); appearance(); }
  });
  languagePicker.addEventListener("click", (event) => {
    const button = event.target.closest("button[data-language]");
    if (!button) return;
    setLanguage(button.dataset.language);
    save("govmc-language", getLanguage());
    languagePicker.open = false;
    languageTrigger.focus();
    renderUpdated();
    notice("");
    appearance(); renderOverview(); renderList(); renderSessions();
  });
  document.addEventListener("click", (event) => {
    for (const picker of document.querySelectorAll(".dropdown[open]")) {
      if (!picker.contains(event.target)) picker.open = false;
    }
  });
  appearance();
  $("menu").addEventListener("click", () => {
    const open = $("nav").classList.toggle("open");
    $("menu").setAttribute("aria-expanded", String(open));
  });
  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape") return;
    const picker = document.querySelector(".dropdown[open]");
    if (picker) {
      picker.open = false;
      picker.querySelector("summary").focus();
      return;
    }
    if ($("nav").classList.contains("open")) {
      $("nav").classList.remove("open");
      $("menu").setAttribute("aria-expanded", "false");
      $("menu").focus();
    }
  });
  const fromHash = () => {
    if (restoringHistory) return;
    const page = location.hash.slice(1);
    if (state.page === "console" && page !== "console" && liveSessions()) {
      const destination = location.href;
      history.replaceState({...history.state, govmcPageIndex: historyIndex}, "", "#console");
      confirmPageLeave(()=>visitWorkspace(destination), true);
      return;
    }
    navigate(page === "console" ? page : "overview");
  };
  window.addEventListener("popstate", event=>{
    if (restoringHistory) {
      restoringHistory=false;
      const action=pendingHistoryConfirmation;pendingHistoryConfirmation=null;
      if (action) confirmPageLeave(action, true);
      return;
    }
    const targetIndex = event.state?.govmcPageIndex;
    const delta = Number.isInteger(targetIndex) ? targetIndex-historyIndex : 0;
    if (!approvedHistory && state.page === "console" && workspacePage(new URL(location.href)) !== "console" && liveSessions() && delta) {
      restoringHistory=true;
      pendingHistoryConfirmation=()=>{approvedHistory=true;history.go(delta);};
      history.go(-delta);
      return;
    }
    if (Number.isInteger(targetIndex)) historyIndex=targetIndex;
    if (approvedHistory) {
      approvedHistory=false;
      navigate(workspacePage(new URL(location.href)));
    }
  });
  window.addEventListener("hashchange", fromHash);
  document.addEventListener("visibilitychange", () => {
    if (state.page === "console") {
      renderSessions();
    } else if (!document.hidden) refresh();
  });
  setInterval(() => { if (!document.hidden && state.page !== "console") refresh(); }, 20000);
  renderOverview();
  renderList();
  fromHash();
  await loadEnvironments();
  refresh();
})();
