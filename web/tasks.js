(async () => {
  "use strict";
  await window.govmcI18n.init();
  const { t } = window.govmcI18n;
  const el = (tag, text, cls) => {
    const n = document.createElement(tag);
    if (text != null) n.textContent = text;
    if (cls) n.className = cls;
    return n;
  };
  const btn = (label, fn) => {
    const b = el("button", t(label));
    b.type = "button";
    b.onclick = fn;
    return b;
  };
  const icon =
    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true"><path d="M8 4h13M8 12h13M8 20h13M3 4h.01M3 12h.01M3 20h.01"/></svg>';
  const trigger = el("button", null, "top-icon tasks-trigger");
  trigger.id = "tasks-trigger";
  trigger.type = "button";
  trigger.innerHTML = icon;
  trigger.title = t("Tasks");
  trigger.setAttribute("aria-label", t("Tasks"));
  trigger.setAttribute("aria-haspopup", "dialog");
  trigger.setAttribute("aria-controls", "tasks-dialog");
  trigger.setAttribute("aria-expanded", "false");
  const badge = el("span", null, "tasks-badge");
  badge.hidden = true;
  trigger.append(badge);
  document.getElementById("language")?.after(trigger);
  const dialog = el("dialog", null, "tasks-dialog");
  dialog.id = "tasks-dialog";
  dialog.setAttribute("aria-label", t("Tasks"));
  document.body.append(dialog);
  let all = false,
    group = "all",
    offset = 0,
    selected = "",
    listScroll = 0,
    listFocus = "",
    busy = false,
    epoch = 0,
    controller = null,
    profile = "",
    savedFocus = null;
  let autoRefresh = true,
    refreshing = false;
  try {
    autoRefresh = localStorage.getItem("govmc-tasks-auto-refresh") !== "false";
  } catch {}
  const icons = {
    refresh:
      '<path d="M20 7v5h-5M4 17v-5h5"/><path d="M6.1 7a7 7 0 0 1 11.6-1L20 9M4 15l2.3 3A7 7 0 0 0 18 17"/>',
    auto: '<path d="M12 3a9 9 0 1 1-6.4 2.6M3 3v5h5"/><path d="M12 7v5l3 2"/>',
    close: '<path d="m6 6 12 12M18 6 6 18"/>',
    back: '<path d="m10 5-7 7 7 7M3 12h18"/>',
  };
  function iconButton(label, name, fn) {
    const b = btn(label, fn);
    b.className = "task-toolbar-button";
    b.setAttribute("aria-label", t(label));
    b.title = t(label);
    b.innerHTML =
      '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">' +
      icons[name] +
      "</svg>";
    return b;
  }
  const context = () => {
    try {
      return (
        JSON.parse(sessionStorage.getItem("govmc-web-profile") || "{}").name ||
        ""
      );
    } catch {
      return "";
    }
  };
  const observed = new Map(),
    watched = new Set();
  function observe(task) {
    if (!task?.id) return;
    const prior = observed.get(task.id);
    observed.set(task.id, task.status);
    if (observed.size > 150) observed.delete(observed.keys().next().value);
    if (terminal(task.status)) {
      watched.delete(task.id);
      if (prior !== task.status)
        window.dispatchEvent(
          new CustomEvent("govmc-tasks-updated", { detail: { task } }),
        );
    }
  }
  const terminal = (s) => !["queued", "running", "cancelling"].includes(s);
  const duration = (task) => {
    const start = new Date(task.startedAt || task.createdAt),
      end = task.endedAt
        ? new Date(task.endedAt)
        : terminal(task.status) && task.updatedAt
          ? new Date(task.updatedAt)
          : new Date();
    const seconds = Math.max(0, Math.round((end - start) / 1000));
    return seconds < 60
      ? seconds + "s"
      : Math.floor(seconds / 60) + "m " + (seconds % 60) + "s";
  };
  const stamp = (v) =>
    v
      ? new Date(v).toLocaleString(document.documentElement.lang, {
          hour12: false,
        })
      : "—";
  function headers(p) {
    const h = { "Content-Type": "application/json", "X-Govmc-Profile": p };
    try {
      const token = sessionStorage.getItem("govmc-api-token");
      if (token) h["X-API-Token"] = token;
    } catch {}
    return h;
  }
  async function api(path, options = {}, p = context()) {
    const r = await fetch(path, {
      ...options,
      headers: headers(p),
      cache: "no-store",
    });
    if (!r.ok) {
      const problem = await r.json().catch(() => null);
      throw Error(
        problem?.message
          ? t(problem.message)
          : t("Task request failed") + " (" + r.status + ")",
      );
    }
    return r.json();
  }
  function title(label, onBack) {
    const h = el("div", null, "tasks-heading");
    const left = el("div", null, "tasks-heading-title");
    const navigation = onBack
      ? iconButton("Back", "back", onBack)
      : iconButton("Close", "close", () => dialog.close());
    navigation.classList.add("task-navigation");
    left.append(navigation);
    left.append(el("h2", t(label)));
    const tools = el("div", null, "tasks-heading-tools");
    const auto = iconButton("Auto refresh", "auto", () => {
      autoRefresh = !autoRefresh;
      try {
        localStorage.setItem("govmc-tasks-auto-refresh", String(autoRefresh));
      } catch {}
      syncToolbar();
      if (autoRefresh) refreshTasks();
    });
    auto.classList.add("task-auto-refresh");
    auto.setAttribute("role", "switch");
    auto.append(el("span", t("Auto refresh")));
    const refresh = iconButton("Refresh", "refresh", () => refreshTasks());
    refresh.classList.add("task-refresh");
    tools.append(auto, refresh);
    h.append(left, tools);
    queueMicrotask(syncToolbar);
    return h;
  }
  function syncToolbar() {
    const auto = dialog.querySelector(".task-auto-refresh");
    auto?.setAttribute("aria-checked", String(autoRefresh));
    if (auto)
      auto.title = t(autoRefresh ? "Auto refresh on" : "Auto refresh off");
    const refresh = dialog.querySelector(".task-refresh");
    refresh?.setAttribute("aria-busy", String(refreshing));
    if (refresh) refresh.disabled = refreshing;
  }
  async function refreshTasks() {
    await poll(true);
  }
  function summary(task) {
    const box = el("dl", null, "task-facts");
    for (const [label, value] of [
      ["Task ID", task.id],
      ["Type", t(task.kind)],
      ["Profile", task.profile],
      ["Original profile", task.originalProfile || "—"],
      ["Source", task.source || "—"],
      ["Process", task.pid || "—"],
      ["Endpoint", task.endpoint],
      ["Status", t(task.status)],
      ["Created", stamp(task.createdAt)],
      ["Updated", stamp(task.updatedAt)],
      ["Started", stamp(task.startedAt)],
      ["Finished", stamp(task.endedAt)],
      ["Duration", duration(task)],
      ["Current step", t(task.currentStep || "—")],
    ]) {
      box.append(el("dt", t(label)), el("dd", value));
    }
    return box;
  }
  function fields(value) {
    const dl = el("dl", null, "task-facts");
    for (const [k, v] of Object.entries(value || {})) {
      if (["confirm", "dryRun", "async"].includes(k)) continue;
      dl.append(
        el("dt", t(k)),
        el(
          "dd",
          typeof v === "boolean"
            ? t(v ? "Yes" : "No")
            : typeof v === "object"
              ? JSON.stringify(v)
              : String(v),
        ),
      );
    }
    return dl;
  }
  async function details(id) {
    if (dialog.dataset.view === "list") {
      listScroll = dialog.querySelector(".tasks-body")?.scrollTop || 0;
      listFocus = id;
    }
    selected = id;
    const request = ++epoch;
    try {
      const task = await api(
        "/v1/tasks/" + encodeURIComponent(id) + "?allProfiles=true",
      );
      observe(task);
      if (request !== epoch || !dialog.open) return;
      const scroll = dialog.querySelector(".tasks-body")?.scrollTop || 0;
      const focused =
        document.activeElement?.tagName === "BUTTON"
          ? document.activeElement.textContent
          : null;
      dialog.dataset.view = "detail";
      dialog.replaceChildren(
        title("Task details", () => {
          selected = "";
          load();
        }),
      );
      const body = el("div", null, "tasks-body");
      body.append(
        summary(task),
        el("h3", t("Parameters")),
        fields(task.parameters),
      );
      const timeline = el("ol", null, "task-timeline");
      for (const step of task.steps || []) {
        const item = el("li");
        item.append(
          el("strong", t(step.name)),
          el(
            "span",
            t(step.status) +
              " · " +
              stamp(step.startedAt) +
              (step.endedAt ? " → " + stamp(step.endedAt) : ""),
          ),
        );
        timeline.append(item);
      }
      body.append(el("h3", t("Steps")), timeline);
      if (!timeline.childElementCount)
        timeline.append(
          el(
            "li",
            t(
              terminal(task.status)
                ? "No recorded steps"
                : "Waiting to execute",
            ),
          ),
        );
      if (task.error) {
        const error = el(
          "p",
          t(task.error) + (task.errorMessage ? " · " + task.errorMessage : ""),
          "task-error",
        );
        error.setAttribute("role", "alert");
        body.append(error);
        if (task.errorMessage)
          body.append(el("p", task.errorMessage, "task-error"));
      }
      if (task.result) {
        body.append(el("h3", t("Result")), fields(task.result));
      }
      if (task.remoteTask)
        body.append(el("p", t("Remote task") + ": " + task.remoteTask));
      dialog.append(body);
      body.scrollTop = scroll;
      const actions = el("div", null, "dialog-actions");
      if (!terminal(task.status))
        actions.append(btn("Cancel task", () => confirmCancel(task)));
      if (actions.childElementCount) dialog.append(actions);
      if (focused)
        [...dialog.querySelectorAll("button")]
          .find((b) => b.textContent === focused)
          ?.focus({ preventScroll: true });
    } catch (e) {
      showError(e);
    }
  }
  function confirmCancel(task) {
    epoch++;
    dialog.dataset.view = "confirm";
    dialog.replaceChildren(
      title("Cancel task", () => details(task.id)),
      el("p", task.profile + " / " + task.resource),
      el("p", t("Cancellation does not roll back completed steps")),
    );
    const actions = el("div", null, "dialog-actions");
    actions.append(
      btn("Confirm", async () => {
        try {
          await api(
            "/v1/tasks/" + encodeURIComponent(task.id) + "/cancel",
            { method: "POST", body: JSON.stringify({ confirm: true }) },
            task.profile,
          );
          await details(task.id);
        } catch (e) {
          showError(e);
        }
      }),
    );
    dialog.append(actions);
  }
  function showError(error) {
    let p = dialog.querySelector(".task-error");
    if (!p) {
      p = el("p", null, "task-error");
      p.setAttribute("role", "alert");
      dialog.append(p);
    }
    p.textContent = error.message;
  }
  async function load() {
    if (busy) return;
    busy = true;
    const request = ++epoch;
    try {
      profile = context();
      const query = new URLSearchParams({
        limit: "50",
        offset: String(offset),
        allProfiles: String(all),
      });
      if (group !== "all") query.set("status", group);
      const page = await api("/v1/tasks?" + query);
      if (request !== epoch || !dialog.open) return;
      const focused = document.activeElement?.dataset.taskId || listFocus;
      const scroll =
        dialog.dataset.view === "list"
          ? dialog.querySelector(".tasks-body")?.scrollTop || 0
          : listScroll;
      dialog.dataset.view = "list";
      dialog.replaceChildren(title("Tasks"));
      const filters = el("div", null, "tasks-filters");
      for (const [key, label] of [
        ["active", "In progress"],
        ["succeeded", "Succeeded"],
        ["failed", "Failed"],
        ["all", "All"],
      ]) {
        const b = btn(label, () => {
          group = key;
          offset = 0;
          load();
        });
        b.setAttribute("aria-pressed", String(group === key));
        filters.append(b);
      }
      const scope = btn(
        all ? "Current environment" : "All environments",
        () => {
          all = !all;
          offset = 0;
          load();
        },
      );
      filters.append(scope);
      dialog.append(filters);
      const body = el("div", null, "tasks-body"),
        list = el("div", null, "task-list");
      for (const task of page.items || []) {
        const row = btn("", () => details(task.id));
        row.className = "task-row";
        row.dataset.taskId = task.id;
        row.append(
          el("strong", task.resource),
          el("span", t(task.kind) + " · " + task.profile),
          el("span", t(task.status), "task-state " + task.status),
        );
        const detail = el("span", null, "task-progress");
        detail.textContent =
          task.percent != null
            ? task.percent + "%"
            : t(task.currentStep || task.status);
        row.append(
          detail,
          el("time", stamp(task.createdAt) + " · " + duration(task)),
        );
        list.append(row);
      }
      if (!list.childElementCount) {
        const empty = el("div", null, "tasks-empty");
        empty.append(
          el(
            "p",
            t(
              group === "active"
                ? "No active tasks"
                : group === "all"
                  ? "No tasks"
                  : "No tasks match this filter",
            ),
          ),
        );
        if (group !== "all")
          empty.append(
            btn("View all tasks", () => {
              group = "all";
              offset = 0;
              load();
            }),
          );
        list.append(empty);
      }
      body.append(list);
      dialog.append(body);
      body.scrollTop = scroll;
      const pager = el("div", null, "dialog-actions");
      const prev = btn("Previous", () => {
        offset = Math.max(0, offset - 50);
        load();
      });
      prev.disabled = offset === 0;
      const next = btn("Next", () => {
        offset += 50;
        load();
      });
      next.disabled = !page.hasMore;
      pager.append(prev, el("span", String(offset / 50 + 1)), next);
      dialog.append(pager);
      if (focused)
        dialog
          .querySelector('[data-task-id="' + CSS.escape(focused) + '"]')
          ?.focus({ preventScroll: true });
    } catch (e) {
      if (dialog.open) showError(e);
    } finally {
      busy = false;
    }
  }
  function open(id) {
    savedFocus = document.activeElement;
    if (!dialog.open) dialog.showModal();
    trigger.setAttribute("aria-expanded", "true");
    if (id) {
      watched.add(id);
      details(id);
    } else {
      selected = "";
      load();
    }
  }
  trigger.onclick = () => open();
  dialog.addEventListener("close", () => {
    trigger.setAttribute("aria-expanded", "false");
    epoch++;
    selected = "";
    savedFocus?.focus();
  });
  dialog.addEventListener("click", (event) => {
    const rect = dialog.getBoundingClientRect();
    if (event.target === dialog && event.clientX < rect.left) dialog.close();
  });
  let lastActive = 0;
  async function poll(force = false) {
    if ((!force && (!autoRefresh || document.hidden)) || refreshing) return;
    refreshing = true;
    syncToolbar();
    try {
      const page = await api(
        "/v1/tasks?allProfiles=true&status=active&limit=1",
      );
      await Promise.allSettled(
        [...watched]
          .slice(0, 40)
          .map(async (id) =>
            observe(
              await api(
                "/v1/tasks/" + encodeURIComponent(id) + "?allProfiles=true",
              ),
            ),
          ),
      );
      const active = page.active ?? page.items?.length ?? 0;
      badge.hidden = !active;
      badge.textContent = active > 99 ? "99+" : String(active);
      badge.setAttribute("aria-hidden", "true");
      trigger.classList.toggle("has-active-tasks", active > 0);
      trigger.title =
        t("Tasks") + (active ? " · " + t("In progress") + ": " + active : "");
      if (lastActive > active)
        window.dispatchEvent(new CustomEvent("govmc-tasks-updated"));
      lastActive = active;
      if (dialog.open && dialog.dataset.view !== "confirm") {
        if (selected) await details(selected);
        else await load();
      }
    } catch (error) {
      if (force && dialog.open) showError(error);
    } finally {
      refreshing = false;
      syncToolbar();
    }
  }
  setInterval(() => poll(), 3000);
  poll();
  window.addEventListener("govmc-language", () => {
    trigger.setAttribute("aria-label", t("Tasks"));
    trigger.title = t("Tasks");
    if (dialog.open) {
      if (selected) details(selected);
      else load();
    }
  });
  // Shared clone form: the selected VM ID and profile stay bound through preview and confirmation.
  async function clone(vm, p = context()) {
    const d = el("dialog", null, "clone-dialog");
    d.id = "clone-dialog";
    d.setAttribute("aria-label", t("Clone machine"));
    const h = el("h2", t("Clone machine"));
    const caption = el("p", p + " / " + vm.name + " (" + vm.id + ")");
    const form = el("form");
    const inputs = {};
    const field = (key, label, value, type = "text") => {
      const wrap = el("label", null, "clone-field");
      wrap.append(el("span", t(label)));
      const input = el("input");
      input.name = key;
      input.type = type;
      if (type === "checkbox") input.checked = !!value;
      else input.value = value ?? "";
      if (key === "dst") input.required = true;
      if (["numCPU", "memoryMB"].includes(key)) input.min = "1";
      inputs[key] = input;
      wrap.append(input);
      form.append(wrap);
    };
    const disks = vm.disks || [],
      file = vm.config?.vmx || disks[0]?.fileName || "",
      ds = /^\[([^\]]+)\]/.exec(file)?.[1] || "";
    field("dst", "Target name", vm.name + "-clone");
    field("datastore", "Source datastore", ds);
    inputs.datastore.readOnly = true;
    const targetWrap = el("label", null, "clone-field");
    targetWrap.append(el("span", t("Destination datastore")));
    const target = el("select");
    target.name = "dstDatastore";
    target.required = true;
    target.setAttribute("aria-label", t("Destination datastore"));
    inputs.dstDatastore = target;
    if (ds) {
      const option = el("option", ds);
      option.value = ds;
      target.append(option);
    }
    targetWrap.append(target);
    form.append(targetWrap);
    api("/v1/datastores", {}, p)
      .then((stores) => {
        if (!d.isConnected || plan) return;
        const choice = target.value;
        for (const store of stores || []) {
          if (
            store.accessible === false ||
            [...target.options].some((o) => o.value === store.name)
          )
            continue;
          const option = el("option", store.name);
          option.value = store.name;
          target.append(option);
        }
        if (choice) target.value = choice;
        window.govmcSelect?.sync(target);
      })
      .catch(() => {});
    field("numCPU", "vCPU", vm.numCPU, "number");
    field("memoryMB", "Memory MiB", vm.memoryMB, "number");
    field("portgroup", "Portgroup (inherit if empty)", "");
    field("powerOn", "Power on after verification", false, "checkbox");
    for (const [key, label] of [
      ["cpuHotAddEnabled", "CPU hot add"],
      ["memoryHotAddEnabled", "Memory hot add"],
      ["lockMemory", "Memory reservation lock"],
    ]) {
      const wrap = el("label", null, "clone-field");
      wrap.append(el("span", t(label)));
      const select = el("select");
      select.name = key;
      for (const [value, text] of [
        ["", "Inherit"],
        ["true", "Enabled"],
        ["false", "Disabled"],
      ]) {
        const option = el("option", t(text));
        option.value = value;
        select.append(option);
      }
      inputs[key] = select;
      wrap.append(select);
      form.append(wrap);
    }
    for (const disk of disks) {
      const wrap = el("label", null, "clone-field");
      wrap.append(el("span", t("Disk MiB") + " · " + disk.label));
      const input = el("input");
      input.type = "number";
      input.value = Math.ceil((disk.capacityKB || 0) / 1024);
      input.min = input.value;
      input.dataset.diskKey = String(disk.key);
      wrap.append(input);
      form.append(wrap);
    }
    const preview = el("section", null, "clone-preview");
    preview.hidden = true;
    const error = el("p", null, "task-error");
    error.setAttribute("role", "alert");
    const actions = el("div", null, "dialog-actions"),
      cancel = btn("Cancel", () => d.close()),
      back = btn("Back", () => {
        plan = null;
        preview.hidden = true;
        form.querySelectorAll("input,select").forEach((i) => {
          i.disabled = false;
          if (i.tagName === "SELECT") window.govmcSelect?.sync(i);
        });
        back.hidden = true;
        submit.textContent = t("Preview changes");
      }),
      submit = el("button", t("Preview changes"));
    submit.type = "submit";
    back.hidden = true;
    actions.append(cancel, back, submit);
    form.append(preview, error, actions);
    d.append(h, caption, form);
    document.body.append(d);
    window.govmcSelect?.upgrade?.(d);
    d.showModal();
    let plan = null,
      requestKey = crypto.randomUUID?.() || String(Date.now()) + Math.random();
    let cloneBusy = false;
    d.addEventListener("close", () => d.remove());
    form.onsubmit = async (event) => {
      event.preventDefault();
      if (cloneBusy) return;
      cloneBusy = true;
      submit.disabled = true;
      error.textContent = "";
      try {
        let spec;
        if (!plan) {
          spec = {
            src: vm.id,
            dst: inputs.dst.value,
            datastore: inputs.datastore.value,
            dstDatastore: inputs.dstDatastore.value,
            numCPU: Number(inputs.numCPU.value),
            memoryMB: Number(inputs.memoryMB.value),
            portgroup: inputs.portgroup.value,
            powerOn: inputs.powerOn.checked,
            dryRun: true,
          };
          for (const key of [
            "cpuHotAddEnabled",
            "memoryHotAddEnabled",
            "lockMemory",
          ])
            if (inputs[key].value !== "")
              spec[key] = inputs[key].value === "true";
          spec.expandDisks = [...form.querySelectorAll("[data-disk-key]")]
            .filter((i) => Number(i.value) > Number(i.min))
            .map((i) => ({
              key: Number(i.dataset.diskKey),
              capacityMiB: Number(i.value),
            }));
          const proposed = await api(
            "/v1/custom/clone",
            { method: "POST", body: JSON.stringify(spec) },
            p,
          );
          if (
            spec.dstDatastore &&
            spec.dstDatastore !== spec.datastore &&
            proposed?.configuration?.dstDatastore !== spec.dstDatastore
          )
            throw Error(t("files.cross_store_update"));
          plan = proposed;
          preview.replaceChildren(
            el("h3", t("Preview changes")),
            fields(plan.configuration || spec),
            el("p", t("New UUID and MAC; guest OS identity remains unchanged")),
          );
          const list = el("ol");
          for (const step of plan.steps || [])
            list.append(el("li", t(step.op)));
          preview.append(list);
          preview.hidden = false;
          form.querySelectorAll("input,select").forEach((i) => {
            i.disabled = true;
            if (i.tagName === "SELECT") window.govmcSelect?.sync(i);
          });
          back.hidden = false;
          submit.textContent = t("Confirm clone");
        } else {
          spec = { ...plan.configuration, dryRun: false, confirm: true };
          const response = await fetch("/v1/custom/clone", {
            method: "POST",
            headers: { ...headers(p), "Idempotency-Key": requestKey },
            body: JSON.stringify(spec),
          });
          if (!response.ok) {
            const body = await response.json().catch(() => ({}));
            throw Error(body.message || t("Task request failed"));
          }
          const task = await response.json();
          d.close();
          open(task.id);
          await poll();
        }
      } catch (e) {
        error.textContent = e.message;
      } finally {
        cloneBusy = false;
        submit.disabled = false;
      }
    };
  }
  window.govmcTasks = { open, clone };
})();
