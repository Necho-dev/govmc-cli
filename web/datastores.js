(async () => {
  "use strict";
  const { t, init, apply, setLanguage, getLanguage } = window.govmcI18n;
  await init();
  const $ = (id) => document.getElementById(id),
    node = (tag, text, className) => {
      const n = document.createElement(tag);
      if (text != null) n.textContent = text;
      if (className) n.className = className;
      return n;
    };
  const paths = {
    all: "M3 3h7v7H3ZM14 3h7v7h-7ZM3 14h7v7H3ZM14 14h7v7h-7Z",
    folder: "M3 7h7l2-3h9v16H3Z",
    file: "M5 3h9l5 5v13H5ZM14 3v6h5M8 13h8M8 17h6",
    image: "M3 3h18v18H3ZM3 17l6-7 5 5 3-3 4 5M15 7h.01",
    disk: "M4 3h16v18H4ZM7 16h10M8 7h8M8 11h8",
    storage:
      "M4 5c0-4 16-4 16 0v14c0 4-16 4-16 0ZM4 5c0 4 16 4 16 0M4 12c0 4 16 4 16 0",
    refresh: "M20 8a8 8 0 1 0 0 8M20 3v5h-5",
    back: "M14 5l-7 7 7 7",
    up: "M5 11l7-7 7 7M12 4v16",
    close: "M6 6l12 12M6 18 18 6",
    copy: "M8 8h13v13H8ZM3 16V3h13",
    move: "M3 12h18M16 7l5 5-5 5M5 4h7M5 20h7",
    rename: "m4 16 12-12 4 4L8 20H4ZM14 6l4 4",
    delete: "M3 6h18M9 6V3h6v3M6 6l1 15h10l1-15M10 10v7M14 10v7",
    download: "M12 3v12M7 10l5 5 5-5M4 17v4h16v-4",
    upload: "M12 16V4M7 9l5-5 5 5M4 17v4h16v-4",
    preview:
      "M2 12c5-9 15-9 20 0-5 9-15 9-20 0ZM12 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6",
    more: "M5 12h.01M12 12h.01M19 12h.01",
    "folder-plus": "M3 7h7l2-3h9v16H3ZM12 10v7M8.5 13.5h7",
  };
  function icon(name) {
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("viewBox", "0 0 24 24");
    svg.setAttribute("fill", "none");
    svg.setAttribute("stroke", "currentColor");
    svg.setAttribute("stroke-width", "1.7");
    svg.setAttribute("stroke-linecap", "round");
    svg.setAttribute("stroke-linejoin", "round");
    svg.setAttribute("aria-hidden", "true");
    const path = document.createElementNS(svg.namespaceURI, "path");
    path.setAttribute("d", paths[name] || paths.file);
    svg.append(path);
    return svg;
  }
  function button(label, fn, name, compact = false, raw = false) {
    const text = raw ? label : t(label);
    const b = node(
      "button",
      compact ? null : text,
      compact ? "file-icon-button" : "",
    );
    b.type = "button";
    b.title = text;
    b.setAttribute("aria-label", text);
    if (name) b.prepend(icon(name));
    b.addEventListener("click", fn);
    return b;
  }
  const state = {
    profile: "",
    instance: "",
    profiles: [],
    connection: "unknown",
    stores: [],
    ds: "",
    path: "",
    files: [],
    search: "",
    kind: "all",
    sort: "name",
    direction: 1,
    page: 1,
    loading: false,
    storeLoading: false,
    fileError: "",
    busy: false,
    epoch: 0,
    history: [],
    controller: new AbortController(),
  };
  window.addEventListener("govmc-tasks-updated", async event => {
    const profile=event.detail?.task?.profile||state.profile;
    await window.govmcCache.invalidate(state.instance,profile);
    if(profile===state.profile&&!state.loading)loadFiles();
  });
  let token = "",
    operation = null,
    previewController = null,
    previewURL = null,
    pendingProfile = null;
  try {
    token = sessionStorage.getItem("govmc-api-token") || "";
  } catch {}
  const notice = (message) => {
    $("notice").textContent = message;
    $("notice").hidden = !message;
  };
  const join = (a, b) => [a, b].filter(Boolean).join("/"),
    parent = (path) =>
      path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : "";
  const bytes = (n) => {
    if (!Number.isFinite(n) || n < 0) return "—";
    const u = ["B", "KiB", "MiB", "GiB", "TiB"];
    let i = 0;
    while (n >= 1024 && i < 4) {
      n /= 1024;
      i++;
    }
    return (
      n.toLocaleString(document.documentElement.lang, {
        maximumFractionDigits: i ? 1 : 0,
      }) +
      " " +
      u[i]
    );
  };
  const validName = (name) =>
    !!name.trim() &&
    !/[\\/\x00-\x1f]/.test(name) &&
    name !== "." &&
    name !== "..";
  const query = (path, extra = {}) =>
    path +
    "?" +
    new URLSearchParams({ ds: state.ds, path: state.path, ...extra });
  function remember() {
    try {
      sessionStorage.setItem(
        "govmc-web-profile",
        JSON.stringify({
          instance: state.instance,
          name: state.profile,
          connection: state.connection,
        }),
      );
    } catch {}
  }
  async function request(url, options = {}) {
    const headers = new Headers(options.headers || {});
    if (token) headers.set("Authorization", "Bearer " + token);
    if (state.profile) headers.set("X-Govmc-Profile", state.profile);
    if (options.body && !(options.body instanceof File))
      headers.set("Content-Type", "application/json");
    const response = await fetch(url, {
      cache: "no-store",
      signal: state.controller.signal,
      ...options,
      headers,
    });
    if (response.status === 401) {
      $("token-dialog").showModal();
      throw new Error(t("files.auth_required"));
    }
    if (!response.ok) {
      const text = await response.text();
      if (response.status === 413) throw new Error(t("files.preview_large"));
      if (response.status === 415)
        throw new Error(t("files.preview_unsupported"));
      let message;
      try {
        const error = JSON.parse(text);
        message = error.message || error.error;
      } catch {}
      const error = new Error(
        message || t("files.request_failed", { status: response.status }),
      );
      error.status = response.status;
      throw error;
    }
    return response;
  }
  const json = async (url, options) => {
    const response = await request(url, options);
    return response.json();
  };
  let combinedEntries = true;
  async function entries(ds, path) {
    if (combinedEntries) {
      try {
        return await json(query("/v1/datastores/entries", { ds, path }));
      } catch (error) {
        if (error.status !== 404) throw error;
        combinedEntries = false;
        notice(t("files.server_update"));
      }
    }
    const [folders, files] = await Promise.all([
      json(query("/v1/datastores/folder", { ds, path })),
      json(query("/v1/datastores/file", { ds, path })),
    ]);
    return [...(folders || []), ...(files || [])];
  }
  function kind(file) {
    if (file.folder) return "folder";
    const ext = file.path.split(".").pop().toLowerCase();
    if (["png", "jpg", "jpeg", "gif", "webp"].includes(ext)) return "image";
    if (ext === "vmdk") return "disk";
    if (ext === "pdf") return "pdf";
    if (
      [
        "txt",
        "log",
        "json",
        "yaml",
        "yml",
        "xml",
        "vmx",
        "vmxf",
        "vmsd",
        "ini",
        "cfg",
        "conf",
        "md",
        "csv",
      ].includes(ext)
    )
      return "text";
    return "file";
  }
  function renderIcons() {
    document.querySelectorAll("[data-icon]").forEach((el) => {
      const text = el.dataset.i18n ? t(el.dataset.i18n) : null;
      el.replaceChildren(icon(el.dataset.icon));
      if (text) el.append(node("span", text));
    });
  }
  function renderProfiles() {
    const picker = $("environment");
    picker.hidden = !state.profiles.length;
    const profile = state.profiles.find((p) => p.name === state.profile);
    picker.querySelector(".environment-initials").textContent = profile
      ? Array.from(profile.name).slice(0, 2).join("").toUpperCase()
      : "··";
    picker.querySelector(".environment-dot").dataset.state = state.connection;
    const status = t(
      { connected: "已连接", error: "连接异常", connecting: "正在连接" }[
        state.connection
      ] || "未知",
    );
    picker.querySelector("summary").title =
      (profile
        ? profile.name +
          " · " +
          profile.username +
          "@" +
          new URL(profile.url).host
        : "") +
      " · " +
      status;
    const menu = picker.querySelector(".dropdown-options");
    menu.replaceChildren();
    for (const p of state.profiles) {
      const b = button(p.name, () => {
        if (state.busy) {
          notice(t("files.wait_operation"));
          return;
        }
        if (p.name === state.profile) return;
        pendingProfile = p.name;
        $("environment-target").textContent = state.profile + " → " + p.name;
        $("environment-confirm").showModal();
        picker.open = false;
      });
      b.replaceChildren(
        node("strong", p.name),
        node("small", p.username + "@" + new URL(p.url).host),
      );
      b.setAttribute("aria-pressed", String(p.name === state.profile));
      menu.append(b);
    }
  }
  async function profiles() {
    const data = await json("/v1/profiles");
    state.instance = data.instance;
    state.profiles = data.profiles || [];
    state.profile = data.current || data.active || "";
    state.connection = "connected";
    try {
      const saved = JSON.parse(
        sessionStorage.getItem("govmc-web-profile") || "null",
      );
      if (
        saved?.instance === data.instance &&
        state.profiles.some((p) => p.name === saved.name)
      ) {
        state.profile = saved.name;
        state.connection = saved.connection || "connected";
      }
    } catch {}
    renderProfiles();
  }
  function storeSummary() {
    const ds = state.stores.find((s) => s.name === state.ds),
      summary = $("store-summary");
    summary.replaceChildren();
    if (!ds) return;
    const health = {
      green: "files.healthy",
      yellow: "files.warning",
      red: "files.critical",
      gray: "files.unknown",
    };
    const maintenance = {
      normal: "files.normal",
      enteringMaintenance: "files.entering_maintenance",
      inMaintenance: "files.maintenance",
    };
    for (const [label, value] of [
      [
        "files.status",
        ds.accessible === false
          ? t("files.inaccessible")
          : t(health[ds.status] || "files.accessible"),
      ],
      [
        "files.maintenance",
        t(maintenance[ds.maintenanceMode] || "files.unknown"),
      ],
      ["files.vmfs", ds.vmfsVersion || "—"],
      ["files.potential", bytes(ds.uncommitted)],
      ["files.max_file", bytes(ds.maxFileSize)],
      ["files.max_disk", bytes(ds.maxVirtualDiskCapacity)],
      ["files.location", ds.url || "—"],
    ]) {
      const cell = node("div");
      cell.append(node("small", t(label)), node("span", value));
      summary.append(cell);
    }
  }
  function renderStores() {
    const list = $("store-list");
    list.replaceChildren();
    $("store-count").textContent = String(state.stores.length);
    $("store-count").title = t("files.store_count", {
      count: state.stores.length,
    });
    for (const ds of state.stores) {
      const b = button(ds.name, () => go(ds.name, ""), null, false, true);
      b.disabled = ds.accessible === false;
      b.className = "store-button";
      b.setAttribute("aria-pressed", String(ds.name === state.ds));
      const name = node("strong", null, "store-name");
      name.append(icon("storage"), node("span", ds.name));
      b.replaceChildren(name);
      const valid =
        ds.capacity > 0 &&
        ds.freeSpace >= 0 &&
        ds.freeSpace <= ds.capacity &&
        ds.accessible !== false;
      const used = valid ? ds.capacity - ds.freeSpace : null,
        pct = valid ? Math.round((used / ds.capacity) * 100) : null;
      b.append(
        node(
          "small",
          (ds.type || "—") +
            " · " +
            (ds.accessible === false
              ? t("files.inaccessible")
              : t("files.machines_count", { count: ds.vmCount ?? 0 })),
        ),
      );
      b.append(
        node(
          "small",
          t("files.space_used", {
            used: bytes(used),
            total: bytes(ds.capacity),
            percent: pct == null ? "—" : pct + "%",
          }),
        ),
        node(
          "small",
          t("files.free_space", {
            size: bytes(ds.accessible === false ? null : ds.freeSpace),
          }),
        ),
      );
      if (ds.capacity > 0 && ds.accessible !== false) {
        const bar = node("progress");
        bar.max = ds.capacity;
        bar.value = ds.capacity - ds.freeSpace;
        bar.setAttribute("aria-label", t("files.used"));
        b.append(bar);
      }
      list.append(b);
    }
    if (!state.stores.length)
      list.append(node("p", t("files.no_stores"), "files-empty"));
    storeSummary();
  }
  function saveLocation() {
    const url = new URL(location.href);
    url.search = new URLSearchParams({
      ds: state.ds,
      path: state.path,
      q: state.search,
      kind: state.kind,
      page: state.page,
    });
    history.replaceState(null, "", url);
  }
  function restoreLocation() {
    const q = new URLSearchParams(location.search);
    state.ds = q.get("ds") || "";
    state.path = q.get("path") || "";
    state.search = q.get("q") || "";
    state.kind = ["all", "folder", "file"].includes(q.get("kind"))
      ? q.get("kind")
      : "all";
    state.page = Math.max(1, Number(q.get("page")) || 1);
    $("file-search").value = state.search;
    renderKind();
  }
  function fileButton(file) {
    const b = button(
      file.path,
      () =>
        file.folder ? go(state.ds, join(state.path, file.path)) : preview(file),
      null,
      false,
      true,
    );
    b.className = "file-name";
    b.dataset.kind = kind(file);
    b.replaceChildren(
      icon(file.folder ? "folder" : kind(file)),
      node("span", file.path),
    );
    return b;
  }
  function moreMenu(file) {
    const wrap = node("span");
    const b = button(
      "files.more",
      () => {
        for (const old of document.querySelectorAll(
          ".file-menu-items:popover-open",
        ))
          old.hidePopover();
        const r = b.getBoundingClientRect();
        menu.style.left =
          Math.max(8, Math.min(r.right - 170, innerWidth - 178)) + "px";
        menu.showPopover();
        menu.style.top =
          Math.min(r.bottom + 4, innerHeight - menu.offsetHeight - 8) + "px";
      },
      "more",
      true,
    );
    const menu = node("div", null, "file-menu-items");
    menu.setAttribute("popover", "auto");
    for (const action of ["copy", "move", "rename", "delete"]) {
      const item = button(
        "files." + action,
        () => {
          menu.hidePopover();
          openOperation(action, file);
        },
        action,
      );
      item.disabled = !combinedEntries || state.loading || !!state.fileError;
      if (action === "delete") item.classList.add("danger");
      menu.append(item);
    }
    wrap.append(b, menu);
    return wrap;
  }
  function renderKind() {
    for (const button of $("file-kind").querySelectorAll("button")) {
      const selected = button.dataset.kind === state.kind;
      button.setAttribute("aria-pressed", String(selected));
      button.tabIndex = selected ? 0 : -1;
    }
  }
  function renderFiles() {
    renderKind();
    $("refresh").setAttribute(
      "aria-busy",
      String(state.loading || state.storeLoading),
    );
    $("refresh").disabled = state.loading || state.storeLoading || state.busy;
    $("reload-files").setAttribute("aria-busy", String(state.loading));
    $("reload-stores").setAttribute("aria-busy", String(state.storeLoading));
    $("reload-stores").disabled = state.storeLoading || state.busy;
    storeSummary();
    const crumbs = $("breadcrumbs");
    crumbs.replaceChildren(
      button(
        state.ds || t("files.choose_store"),
        () => go(state.ds, ""),
        null,
        false,
        true,
      ),
    );
    let current = "";
    for (const segment of state.path.split("/").filter(Boolean)) {
      current = join(current, segment);
      const path = current;
      crumbs.append(
        node("span", "/"),
        button(segment, () => go(state.ds, path), null, false, true),
      );
    }
    const selected = state.stores.find((s) => s.name === state.ds);
    for (const id of ["mkdir", "upload", "reload-files"])
      $(id).disabled =
        state.loading ||
        state.busy ||
        !selected ||
        selected.accessible === false;
    for (const id of ["mkdir", "upload"])
      if (state.fileError || !combinedEntries) $(id).disabled = true;
    $("up").disabled = state.loading || !state.path;
    $("back").disabled = state.loading || !state.history.length;
    const visible = state.files
      .filter(
        (file) =>
          file.path
            .toLocaleLowerCase()
            .includes(state.search.toLocaleLowerCase()) &&
          (state.kind === "all" || file.folder === (state.kind === "folder")),
      )
      .sort(
        (a, b) =>
          Number(b.folder) - Number(a.folder) ||
          state.direction *
            (state.sort === "size"
              ? (a.size || 0) - (b.size || 0)
              : state.sort === "modified"
                ? (a.modified || "").localeCompare(b.modified || "")
                : a.path.localeCompare(b.path)),
      );
    const pages = Math.max(1, Math.ceil(visible.length / 50));
    if (!state.loading) state.page = Math.min(state.page, pages);
    const body = $("file-body");
    body.replaceChildren();
    if (!state.loading || state.files.length)
      for (const file of visible.slice(
        (state.page - 1) * 50,
        state.page * 50,
      )) {
        const tr = node("tr");
        const name = node("td");
        name.append(fileButton(file));
        tr.append(
          name,
          node("td", t("files.kind_" + kind(file))),
          node("td", file.folder ? "—" : bytes(file.size)),
          node(
            "td",
            file.modified
              ? new Date(file.modified).toLocaleString(
                  document.documentElement.lang,
                  { hour12: false },
                )
              : "—",
          ),
        );
        const td = node("td", null, "file-operations"),
          actions = node("div", null, "file-row-actions");
        if (!file.folder) {
          actions.append(
            button("files.preview", () => preview(file), "preview", true),
            button("files.download", () => download(file), "download", true),
          );
        }
        actions.append(moreMenu(file));
        tr.append(td);
        td.append(actions);
        body.append(tr);
      }
    $("file-empty").hidden = visible.length > 0;
    $("file-table-wrap").setAttribute("aria-busy", String(state.loading));
    $("file-empty").textContent = state.loading
      ? t("files.loading")
      : state.fileError
        ? t("files.load_failed")
        : state.ds
          ? t("files.empty")
          : t("files.choose_store");
    $("file-info").textContent = state.fileError
      ? "—"
      : t("files.entry_count", { count: visible.length });
    $("page-number").textContent = state.page + " / " + pages;
    $("previous").disabled = state.page <= 1 || state.loading;
    $("next").disabled = state.page >= pages || state.loading;
    for (const b of document.querySelectorAll("[data-sort]"))
      b.setAttribute("aria-pressed", String(b.dataset.sort === state.sort));
    saveLocation();
  }
  async function loadFiles() {
    const epoch = ++state.epoch;
    state.loading = true;
    state.fileError = "";
    notice(combinedEntries ? "" : t("files.server_update"));
    const cached = await window.govmcCache.read(
      state.instance,
      state.profile,
      "directory:" + state.ds + ":" + state.path,
    );
    if (epoch !== state.epoch) return;
    state.files = cached?.value || [];
    renderFiles();
    if (!state.ds) {
      state.loading = false;
      renderFiles();
      return;
    }
    try {
      const files = await entries(state.ds, state.path);
      if (epoch !== state.epoch) return;
      state.files = (files || []).map((file) => ({
        ...file,
        path: file.folder ? file.path.replace(/\/+$/, "") : file.path,
      }));
      window.govmcCache.write(
        state.instance,
        state.profile,
        "directory:" + state.ds + ":" + state.path,
        state.files,
      );
      state.connection = "connected";
      remember();
      renderProfiles();
    } catch (error) {
      if (epoch !== state.epoch || error.name === "AbortError") return;
      state.connection = "error";
      state.fileError = error.message;
      remember();
      renderProfiles();
      notice(error.message);
    } finally {
      if (epoch === state.epoch) {
        state.loading = false;
        renderFiles();
      }
    }
  }
  async function go(ds, path, restore = false) {
    if (state.busy) return;
    if (!ds) return;
    if (!restore)
      state.history.push({
        ds: state.ds,
        path: state.path,
        search: state.search,
        kind: state.kind,
        page: state.page,
      });
    state.ds = ds;
    state.path = path.replace(/^\/+|\/+$/g, "");
    state.search = "";
    state.kind = "all";
    state.page = 1;
    $("file-search").value = "";
    renderKind();
    renderStores();
    notice("");
    await loadFiles();
  }
  async function loadStores() {
    const epoch = state.epoch;
    state.loading = true;
    state.storeLoading = true;
    renderFiles();
    try {
      const cached = (
        await window.govmcCache.read(
          state.instance,
          state.profile,
          "datastores",
        )
      )?.value;
      if (epoch !== state.epoch) return;
      if (cached) {
        state.stores = cached;
        if (!state.ds)
          state.ds =
            cached.find((store) => store.accessible !== false)?.name || "";
        renderStores();
        state.files =
          (
            await window.govmcCache.read(
              state.instance,
              state.profile,
              "directory:" + state.ds + ":" + state.path,
            )
          )?.value || [];
        renderFiles();
      }
      const stores = await json("/v1/datastores");
      if (epoch !== state.epoch) return;
      state.stores = stores || [];
      window.govmcCache.write(
        state.instance,
        state.profile,
        "datastores",
        state.stores,
      );
      if (!state.stores.some((s) => s.name === state.ds)) {
        state.ds = state.stores.find((s) => s.accessible !== false)?.name || "";
        state.path = "";
      }
      renderStores();
      await loadFiles();
    } catch (error) {
      if (epoch === state.epoch) {
        state.loading = false;
        state.fileError = error.message;
        renderFiles();
      }
      throw error;
    } finally {
      state.storeLoading = false;
      renderFiles();
    }
  }
  function resetPreview() {
    previewController?.abort();
    previewController = null;
    if (previewURL) URL.revokeObjectURL(previewURL);
    previewURL = null;
    $("preview-content").replaceChildren();
  }
  async function preview(file) {
    if (!combinedEntries) {
      notice(t("files.server_update"));
      return;
    }
    resetPreview();
    $("preview-title").textContent = file.path;
    $("preview-content").append(node("p", t("files.loading"), "muted"));
    $("preview-dialog").showModal();
    previewController = new AbortController();
    const controller = previewController;
    try {
      const response = await request(
        query("/v1/datastores/file/preview", {
          path: join(state.path, file.path),
        }),
        { signal: controller.signal },
      );
      const type = response.headers.get("Content-Type") || "";
      const blob = await response.blob();
      if (controller.signal.aborted) return;
      $("preview-content").replaceChildren();
      if (type.startsWith("text/"))
        $("preview-content").append(node("pre", await blob.text()));
      else {
        previewURL = URL.createObjectURL(blob);
        const view = node(type === "application/pdf" ? "iframe" : "img");
        view.src = previewURL;
        view.title = file.path;
        if (view.tagName === "IMG") view.alt = file.path;
        $("preview-content").append(view);
      }
    } catch (error) {
      if (error.name !== "AbortError")
        $("preview-content").replaceChildren(node("p", error.message, "muted"));
    }
  }
  async function download(file) {
    let writer;
    try {
      if (window.showSaveFilePicker) {
        const handle = await showSaveFilePicker({ suggestedName: file.path });
        writer = await handle.createWritable();
      }
      const response = await request(
        query("/v1/datastores/file/content", {
          path: join(state.path, file.path),
        }),
      );
      if (writer) {
        await response.body.pipeTo(writer);
        notice(t("files.downloaded"));
      } else {
        const size = Number(response.headers.get("Content-Length"));
        if (
          !response.headers.has("Content-Length") ||
          size > 64 * 1024 * 1024
        ) {
          await response.body.cancel();
          throw new Error(t("files.streaming_required"));
        }
        const blob = await response.blob(),
          url = URL.createObjectURL(blob),
          a = node("a");
        a.href = url;
        a.download = file.path;
        a.click();
        setTimeout(() => URL.revokeObjectURL(url), 30000);
      }
    } catch (error) {
      try {
        await writer?.abort();
      } catch {}
      if (error.name !== "AbortError") notice(error.message);
    }
  }
  function openOperation(action, file = null) {
    if (state.busy || state.loading || state.fileError) return;
    if (!combinedEntries) {
      notice(t("files.server_update"));
      return;
    }
    operation = {
      action,
      file,
      ds: state.ds,
      path: state.path,
      profile: state.profile,
      requestKey:
        window.crypto?.randomUUID?.() || String(Date.now()) + Math.random(),
      stage: 0,
      body: null,
    };
    $("file-dialog-title").textContent = t("files." + action);
    $("operation-source").textContent =
      "[" +
      state.ds +
      "] /" +
      (file ? join(state.path, file.path) : state.path);
    $("operation-fields").replaceChildren();
    $("operation-error").textContent = "";
    $("operation-plan").hidden = true;
    $("operation-submit").textContent = t("files.preview_changes");
    if (["copy", "move"].includes(action)) {
      const label=node("label",t("Destination datastore"));
      const select=node("select");select.id="operation-datastore";select.setAttribute("aria-label",t("Destination datastore"));
      for(const store of state.stores){const option=node("option",store.name);option.value=store.name;option.disabled=store.accessible===false;select.append(option);}
      select.value=operation.ds;
      label.append(select);$("operation-fields").append(label);
    }
    if (["copy", "move", "rename", "mkdir"].includes(action)) {
      const label = node(
        "label",
        t(
          action === "copy" || action === "move"
            ? "files.destination"
            : "files.new_name",
        ),
      );
      const input = node("input");
      input.id = "operation-value";
      input.required = true;
      input.value =
        action === "rename"
          ? file.path
          : action === "copy"
            ? join(state.path, file.path + ".copy")
            : action === "move"
              ? join(state.path, file.path)
              : "";
      label.append(input);
      $("operation-fields").append(label);
      if (action === "copy" || action === "move")
        $("operation-fields").append(
          node("small", t("files.destination_help")),
        );
    }
    if (action === "upload")
      $("operation-fields").append(
        node("p", file.name + " · " + bytes(file.size)),
      );
    if (action === "delete")
      $("operation-fields").append(node("p", t("files.delete_notice")));
    $("file-dialog").showModal();
    $("operation-value")?.focus();
  }
  async function submitOperation(event) {
    event.preventDefault();
    if (!operation || state.busy) return;
    const op = operation;
    state.busy = true;
    $("operation-submit").disabled = true;
    try {
      if (op.profile !== state.profile || op.ds !== state.ds)
        throw new Error(t("files.target_changed"));
      if (!op.stage) {
        let value = $("operation-value")?.value.trim() || "",
          source = op.file ? join(op.path, op.file.path) : "";
        if (["mkdir", "rename"].includes(op.action) && !validName(value))
          throw new Error(t("files.invalid_name"));
        if (["copy", "move"].includes(op.action)) {
          value = value.replace(/^\/+|\/+$/g, "");
          const dstDS=$("operation-datastore").value;
          if(!dstDS)throw new Error(t("files.invalid_destination"));
          if (
            !value ||
            value.split("/").some((part) => !validName(part)) ||
            (dstDS === op.ds && (value === source ||
            value.startsWith(source + "/")))
          )
            throw new Error(t("files.invalid_destination"));
          const destinationEntries = await entries(dstDS, parent(value));
          if (destinationEntries.some((f) => f.path === value.split("/").pop()))
            throw new Error(t("files.exists"));
          op.body = { ds: op.ds, dstDatastore:dstDS, src: source, dst: value, dryRun: true };
        } else if (op.action === "rename") {
          if (state.files.some((f) => f.path === value))
            throw new Error(t("files.exists"));
          op.body = { ds: op.ds, path: source, name: value, dryRun: true };
        } else if (op.action === "mkdir") {
          if (state.files.some((f) => f.path === value))
            throw new Error(t("files.exists"));
          op.body = { ds: op.ds, path: join(op.path, value) };
        } else if (op.action === "delete")
          op.body = { ds: op.ds, path: source };
        else if (op.action === "upload") {
          if (state.files.some((f) => f.path === op.file.name))
            throw new Error(t("files.exists"));
          op.body = { ds: op.ds, path: join(op.path, op.file.name) };
        }
        const type = op.file?.folder ? "folder" : "file";
        op.url = "/v1/datastores/" + type + "/" + op.action;
        if (["copy", "move", "rename"].includes(op.action)) {
          const preview=await json(op.url, { method: "POST", body: JSON.stringify(op.body) });
          if (["copy","move"].includes(op.action) && op.body.dstDatastore!==op.ds && preview?.detail?.dstDatastore!==op.body.dstDatastore)
            throw new Error(t("files.cross_store_update"));
        }
        op.stage = 1;
        $("operation-fields")
          .querySelectorAll("input,select")
          .forEach((input) => (input.disabled = true));
        $("operation-plan").textContent = t("files.plan", {
          action: t("files." + op.action),
          profile: op.profile,
          source: "[" + op.ds + "] /" + (source || op.path),
          destination:
            op.action === "rename"
              ? join(op.path, value)
              : op.action === "delete"
                ? t("files.removed")
                : op.body.dst ? "["+op.body.dstDatastore+"] /"+op.body.dst : op.body.path,
        });
        $("operation-plan").hidden = false;
        $("operation-submit").textContent = t("files.confirm_action");
        return;
      }
      state.busy = true;
      $("operation-submit").disabled = true;
      $("operation-cancel").disabled = true;
      window.addEventListener("beforeunload", warnUnload);
      if (op.action === "mkdir")
        await json("/v1/datastores/folder", {
          method: "POST",
          body: JSON.stringify({ ...op.body, confirm: true }),
        });
      else if (op.action === "upload")
        await json(
          "/v1/datastores/file?" +
            new URLSearchParams({ ...op.body, confirm: true }),
          {
            method: "PUT",
            body: op.file,
            headers: { "Content-Type": "application/octet-stream" },
          },
        );
      else if (op.action === "delete")
        await json("/v1/datastores/" + (op.file.folder ? "folder" : "file"), {
          method: "DELETE",
          body: JSON.stringify({ ...op.body, confirm: true }),
        });
      else {
        const submitted = await json(op.url, {
          method: "POST",
          headers: { "Idempotency-Key": op.requestKey },
          body: JSON.stringify({ ...op.body, confirm: true, dryRun: false }),
        });
        if (submitted?.id) {
          $("file-dialog").close();
          notice(t("Task submitted"));
          window.govmcTasks?.open(submitted.id);
          return;
        }
      }
      await window.govmcCache.invalidate(state.instance, state.profile);
      $("file-dialog").close();
      notice(t("files.completed"));
      await loadFiles();
    } catch (error) {
      $("operation-error").textContent = error.message;
    } finally {
      state.busy = false;
      $("operation-submit").disabled = false;
      $("operation-cancel").disabled = false;
      window.removeEventListener("beforeunload", warnUnload);
      renderFiles();
    }
  }
  function warnUnload(event) {
    event.preventDefault();
    event.returnValue = "";
  }
  $("file-form").addEventListener("submit", submitOperation);
  $("operation-cancel").addEventListener("click", () =>
    $("file-dialog").close(),
  );
  $("file-dialog").addEventListener("cancel", (event) => {
    if (state.busy) event.preventDefault();
  });
  $("preview-close").addEventListener("click", () =>
    $("preview-dialog").close(),
  );
  $("preview-dialog").addEventListener("close", resetPreview);
  $("mkdir").addEventListener("click", () => openOperation("mkdir"));
  $("upload").addEventListener("click", () => $("upload-input").click());
  $("upload-input").addEventListener("change", (event) => {
    const file = event.target.files[0];
    if (file) openOperation("upload", file);
    event.target.value = "";
  });
  $("file-search").addEventListener("input", (event) => {
    state.search = event.target.value;
    state.page = 1;
    renderFiles();
  });
  $("file-kind").addEventListener("click", (event) => {
    const button = event.target.closest("button[data-kind]");
    if (!button) return;
    state.kind = button.dataset.kind;
    state.page = 1;
    renderFiles();
  });
  $("file-kind").addEventListener("keydown", (event) => {
    if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
    event.preventDefault();
    const buttons = [...$("file-kind").querySelectorAll("button")],
      index = buttons.indexOf(document.activeElement);
    const next =
      event.key === "Home"
        ? 0
        : event.key === "End"
          ? 2
          : (index + (event.key === "ArrowLeft" ? 2 : 1)) % 3;
    buttons[next].click();
    buttons[next].focus();
  });
  for (const b of document.querySelectorAll("[data-sort]"))
    b.addEventListener("click", () => {
      state.direction = state.sort === b.dataset.sort ? -state.direction : 1;
      state.sort = b.dataset.sort;
      renderFiles();
    });
  $("previous").addEventListener("click", () => {
    state.page--;
    renderFiles();
  });
  $("next").addEventListener("click", () => {
    state.page++;
    renderFiles();
  });
  $("up").addEventListener("click", () => go(state.ds, parent(state.path)));
  $("back").addEventListener("click", async () => {
    const saved = state.history.pop();
    if (!saved) return;
    Object.assign(state, saved);
    $("file-search").value = state.search;
    renderKind();
    renderStores();
    await loadFiles();
  });
  const safely = (fn) => async () => {
    if (state.busy) {
      notice(t("files.wait_operation"));
      return;
    }
    try {
      await fn();
    } catch (error) {
      if (error.name !== "AbortError") notice(error.message);
    }
  };
  $("reload-files").addEventListener("click", safely(loadFiles));
  $("reload-stores").addEventListener("click", safely(loadStores));
  $("refresh").addEventListener("click", safely(loadStores));
  $("environment-confirm").addEventListener("close", async () => {
    const name = pendingProfile;
    pendingProfile = null;
    if ($("environment-confirm").returnValue !== "switch" || !name) return;
    try {
      await json("/v1/profiles/connect", {
        method: "POST",
        body: JSON.stringify({ name }),
      });
      state.controller.abort();
      state.controller = new AbortController();
      state.epoch++;
      state.profile = name;
      state.connection = "connected";
      state.ds = state.path = state.search = "";
      state.files = [];
      state.history = [];
      state.page = 1;
      state.kind = "all";
      $("file-search").value = "";
      renderKind();
      $("file-dialog").close();
      $("preview-dialog").close();
      remember();
      renderProfiles();
      await loadStores();
    } catch (error) {
      notice(error.message);
    }
  });
  $("language").addEventListener("click", (event) => {
    const b = event.target.closest("[data-language]");
    if (!b) return;
    setLanguage(b.dataset.language);
    try {
      localStorage.setItem("govmc-language", getLanguage());
    } catch {}
    $("language").open = false;
    renderIcons();
    renderStores();
    renderFiles();
    renderProfiles();
  });
  $("theme").addEventListener("click", (event) => {
    const b = event.target.closest("[data-theme-option]");
    if (!b) return;
    window.govmcTheme.apply(b.dataset.themeOption);
    try {
      localStorage.setItem("govmc-theme", b.dataset.themeOption);
    } catch {}
  });
  window.addEventListener("storage", (event) => {
    if (event.key === "govmc-theme") window.govmcTheme.apply(event.newValue);
  });
  $("menu").addEventListener("click", () => {
    $("menu").setAttribute(
      "aria-expanded",
      String($("nav").classList.toggle("open")),
    );
  });
  document.addEventListener("click", (event) => {
    for (const d of document.querySelectorAll(".dropdown[open]"))
      if (!d.contains(event.target)) d.open = false;
  });
  $("token-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    token = $("api-token").value;
    try {
      sessionStorage.setItem("govmc-api-token", token);
    } catch {}
    $("api-token").value = "";
    $("token-dialog").close();
    try {
      await profiles();
      await loadStores();
    } catch (error) {
      notice(error.message);
    }
  });
  $("token-cancel").addEventListener("click", () => $("token-dialog").close());
  apply();
  window.govmcTheme.apply(window.govmcTheme.read());
  renderIcons();
  const detailsMedia = matchMedia("(min-width:701px)");
  $("store-details").open = detailsMedia.matches;
  detailsMedia.addEventListener(
    "change",
    (event) => ($("store-details").open = event.matches),
  );
  restoreLocation();
  renderFiles();
  try {
    await profiles();
    await loadStores();
  } catch (error) {
    notice(error.message);
  }
})();
