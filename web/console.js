/* global $ */
(async function () {
  const { t, init, setLanguage } = window.govmcI18n;
  await init();
  const capability = await window.govmcWMKS.check();
  if (!capability.available) { window.govmcWMKS.guide(document.querySelector("main"),capability); return; }
  try { await window.govmcWMKS.load(); } catch {
    window.govmcWMKS.guide(document.querySelector("main"),{state:"ready"});
    window.parent.postMessage({source:"govmc-console",state:"sdk-error"},location.origin); return;
  }
  const $box = $("#wmksContainer");
  const $status = $("#status");
  const $name = $("#machine-name");
  const $cad = $("#cad");
  const $disc = $("#disconnect");
  const $hud = $("#hud");
  let sessionActive = false;
  let phase = "disconnected";
  let currentId = "";
  let currentName = "";
  function vmLabel() {
    if (currentName && currentId && currentName !== currentId) {
      return currentName + " (" + currentId + ")";
    }
    return currentName || currentId;
  }
  let ready = false;
  let grabbed = false;
  let ungrabbing = false;
  let keysLocked = false;
  let relAccX = 0;
  let relAccY = 0;
  let relRaf = 0;

  const stats = {
    frames: 0,
    fps: 0,
    sampledAt: performance.now(),
    markFrame() {
      this.frames++;
    },
  };

  $box.wmks({
    fitToParent: true,
    fitGuest: false,
    useNativePixels: false,
    useVNCHandshake: true,
    retryConnectionInterval: 1500,
    // 直通：Command=Win，Control=Ctrl。不要把 Cmd+C/V 改成 Ctrl+C/V。
    mapMetaToCtrlForKeys: [],
    mapMetaToCtrlForVScans: [],
    enableWindowsKey: false,
    disableVscanKeyboard: false,
  });
  patchKeyboardPassthrough();

  $box.find("canvas").attr("tabindex", "-1");
  patchRescale();

  $box.on("wmksconnecting", () => report("connecting"));
  $box.on("wmksconnected", () => {
    if (phase !== "connecting") return;
    ready = true;
    $cad.prop("disabled", false);
    $disc.prop("disabled", false);
    hookFps();
    patchKeyboardPassthrough();
    wireMouse();
    wireHostKeys();
    setRelativeMouse(false);
    setIdleCursor();
    scheduleLayout();
    report("connected");
  });
  $box.on("wmksresolutionchanged wmksframeBufferInited", () => {
    if (!ready) return;
    hookFps();
    wireMouse();
    scheduleLayout();
  });
  function markDisconnected() {
    ready = false;
    if (disconnectDialog.open) disconnectDialog.close();
    leaveControl(true);
    $cad.prop("disabled", true);
    $disc.prop("disabled", true);
    report("disconnected");
  }
  function disconnectConsole() {
    try {
      leaveControl(true);
      $box.wmks("disconnect");
    } finally {
      // WebMKS detaches the socket close handler on a client-initiated disconnect.
      markDisconnected();
    }
  }
  $box.on("wmksdisconnected", markDisconnected);
  $box.on("wmkserror wmksauthenticationfailed wmksprotocolerror", () => {
    ready = false;
    leaveControl(true);
    $cad.prop("disabled", true);
    $disc.prop("disabled", true);
    report("error");
  });

  $("#cad").on("click", () => {
    if (!sessionActive || !ready) return;
    const { VSCAN_LCONTROL, VSCAN_LALT, VSCAN_DELETE } = WMKS.VSCAN_CODES;
    $box.wmks("sendKeyCodes", [17, 18, 46], [VSCAN_LCONTROL, VSCAN_LALT, VSCAN_DELETE], true);
  });
  const disconnectDialog = document.getElementById("disconnect-confirm");
  $("#disconnect").on("click", () => {
    if (!ready || disconnectDialog.open) return;
    leaveControl(true);
    if (window.parent !== window) {
      window.parent.postMessage({source:"govmc-console",state:"disconnect-request"},location.origin);
      return;
    }
    document.getElementById("disconnect-target").textContent = vmLabel();
    disconnectDialog.returnValue = "";
    disconnectDialog.showModal();
  });
  disconnectDialog.addEventListener("close", () => {
    if (ready && disconnectDialog.returnValue === "disconnect") disconnectConsole();
  });
  $(window).on("resize", () => {
    if (ready) layoutConsole();
  });
  setInterval(renderHud, 1000);

  function widget() {
    return $box.data("wmks-wmks") || $box.data("wmks");
  }

  function patchRescale() {
    const inst = widget();
    if (!inst || inst._govmomiPatched) return;
    const orig = inst.rescaleOrResize.bind(inst);
    inst.rescaleOrResize = function (tryFitGuest) {
      orig(tryFitGuest);
      centerScaledCanvas();
    };
    inst._govmomiPatched = true;
  }

  function centerScaledCanvas() {
    const host = $box[0];
    const canvas = $box.find("canvas#mainCanvas")[0];
    if (!host || !canvas) return;
    const inst = widget();
    const hostW = host.clientWidth;
    const hostH = host.clientHeight;
    if (hostW < 8 || hostH < 8) return;
    canvas.style.transformOrigin = "0 0";
    const gw = (inst && inst._guestWidth) || canvas.offsetWidth || canvas.width;
    const gh = (inst && inst._guestHeight) || canvas.offsetHeight || canvas.height;
    const scaleX = hostW / gw;
    const scaleY = hostH / gh;
    const scale = Math.max(0.1, Math.min(scaleX, scaleY));
    if (inst) inst._scale = scale;
    canvas.style.transform = "scale(" + scale + ")";
    const left = Math.max(0, (hostW - gw * scale) / 2);
    canvas.style.left = left + "px";
    canvas.style.top = "0px";
    if (inst) {
      inst._x = left;
      inst._y = 0;
    }
    $box.find("video").each(function () {
      this.style.transformOrigin = "0 0";
      this.style.transform = "scale(" + scale + ")";
      this.style.left = left + "px";
      this.style.top = "0px";
    });
  }

  function layoutConsole() {
    patchRescale();
    try {
      $box.wmks("rescale");
    } catch (_) {
      /* widget 尚未就绪 */
    }
    centerScaledCanvas();
  }

  function scheduleLayout() {
    layoutConsole();
    requestAnimationFrame(() => {
      layoutConsole();
      requestAnimationFrame(layoutConsole);
    });
    setTimeout(layoutConsole, 50);
    setTimeout(layoutConsole, 200);
  }

  function watchContainerSize() {
    const host = $box[0];
    if (!host || host._govmomiRo) return;
    if (typeof ResizeObserver === "undefined") return;
    host._govmomiRo = true;
    new ResizeObserver(() => {
      if (ready) layoutConsole();
    }).observe(host);
  }
  watchContainerSize();

  function setRelativeMouse(on) {
    const inst = widget();
    if (!inst) return;
    inst.options.sendRelativeMouseEvent = on;
    if (inst._vncDecoder) inst._vncDecoder.options.sendRelativeMouseEvent = on;
    if (!on) {
      relAccX = 0;
      relAccY = 0;
      if (relRaf) {
        cancelAnimationFrame(relRaf);
        relRaf = 0;
      }
    }
  }

  function guestMouseScale() {
    const inst = widget();
    const scale = inst && inst._scale ? inst._scale : 1;
    const pixel = inst && inst._pixelRatio ? inst._pixelRatio : 1;
    const dpi = inst && inst._displayScaleRatio ? inst._displayScaleRatio : 1;
    return (pixel * dpi) / scale;
  }

  function queueRelativeMove(dx, dy) {
    if (!dx && !dy) return;
    const k = guestMouseScale();
    relAccX += dx * k;
    relAccY += dy * k;
    if (!relRaf) relRaf = requestAnimationFrame(flushRelativeMove);
  }

  function flushRelativeMove() {
    if (relRaf) {
      cancelAnimationFrame(relRaf);
      relRaf = 0;
    }
    const inst = widget();
    const dec = inst && inst._vncDecoder;
    if (!dec || !dec.options.sendRelativeMouseEvent) {
      relAccX = 0;
      relAccY = 0;
      return;
    }
    const dx = Math.trunc(relAccX);
    const dy = Math.trunc(relAccY);
    if (!dx && !dy) return;
    relAccX -= dx;
    relAccY -= dy;
    // 不要走 onMouseMove：它按绝对坐标节流，相对位移会被后一包覆盖丢掉。
    dec._mouseX = dx;
    dec._mouseY = dy;
    dec._sendMouseEvent();
    dec._mouseX = 0;
    dec._mouseY = 0;
  }

  function setIdleCursor() {
    const canvas = $box.find("canvas#mainCanvas")[0];
    if (!canvas) return;
    if (document.pointerLockElement === canvas) return;
    $box.removeClass("controlling");
    canvas.style.cursor = "default";
    $box.css("cursor", "default");
  }

  function releaseGuestModifiers() {
    const inst = widget();
    const km = inst && inst._keyboardManager;
    const dec = inst && inst._vncDecoder;
    const codes = (typeof WMKS !== "undefined" && WMKS.VSCAN_CODES) || {};
    const scans = [
      codes.VSCAN_LCONTROL,
      codes.VSCAN_RCONTROL,
      codes.VSCAN_LALT,
      codes.VSCAN_RALT,
      codes.VSCAN_LSHIFT,
      codes.VSCAN_RSHIFT,
      codes.VSCAN_LGUI,
      codes.VSCAN_RGUI,
    ].filter(Boolean);
    scans.forEach((scan) => {
      if (km && km.sendVScanKey) km.sendVScanKey(scan, false);
      else if (dec && dec.onKeyVScan) dec.onKeyVScan(scan, false);
    });
  }

  function enterControl() {
    if (!sessionActive || !ready || grabbed) return;
    grabbed = true;
    const canvas = $box.find("canvas#mainCanvas")[0];
    setRelativeMouse(true);
    $box.addClass("controlling");
    $box.trigger("focus");
    if (canvas) canvas.style.cursor = "none";
    renderStatus();
  }

  function leaveControl(silent) {
    if (ungrabbing) return;
    ungrabbing = true;
    grabbed = false;
    setRelativeMouse(false);
    unlockSystemKeys();
    releaseGuestModifiers();
    $box.removeClass("controlling");
    if (document.pointerLockElement) {
      try {
        document.exitPointerLock();
      } catch (_) {}
    }
    setIdleCursor();
    if (!silent || ready) renderStatus();
    ungrabbing = false;
    if (ready) layoutConsole();
  }

  function hookFps() {
    const canvas = $box.find("canvas#mainCanvas")[0];
    if (!canvas || canvas._fpsHooked) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    canvas._fpsHooked = true;
    ["putImageData", "drawImage"].forEach((name) => {
      const orig = ctx[name].bind(ctx);
      ctx[name] = function () {
        stats.markFrame();
        return orig.apply(this, arguments);
      };
    });
  }

  function wireMouse() {
    const inst = widget();
    const canvas = $box.find("canvas#mainCanvas")[0];
    if (!inst || !canvas || canvas._govmomiMouse) {
      setIdleCursor();
      return;
    }
    canvas._govmomiMouse = true;
    setRelativeMouse(false);
    setIdleCursor();

    canvas.addEventListener("click", () => {
      if (!ready) return;
      $box.trigger("focus");
      if (document.pointerLockElement === canvas) return;
      beginGrab(canvas);
    });

    document.addEventListener("pointerlockchange", () => {
      const locked = document.pointerLockElement === canvas;
      if (locked) enterControl();
      else if (grabbed) leaveControl();
    });

    document.addEventListener("mousemove", (e) => {
      if (!ready || !inst._vncDecoder) return;
      if (document.pointerLockElement !== canvas) return;
      queueRelativeMove(e.movementX || 0, e.movementY || 0);
      e.stopPropagation();
    });

    document.addEventListener("mousedown", (e) => {
      if (!ready || !inst._vncDecoder) return;
      if (document.pointerLockElement !== canvas) return;
      flushRelativeMove();
      inst._vncDecoder.onMouseButton(0, 0, 1, 1 << (e.button || 0));
      e.preventDefault();
    });
    document.addEventListener("mouseup", (e) => {
      if (!ready || !inst._vncDecoder) return;
      if (document.pointerLockElement !== canvas) return;
      flushRelativeMove();
      inst._vncDecoder.onMouseButton(0, 0, 0, 1 << (e.button || 0));
      e.preventDefault();
    });
    document.addEventListener(
      "wheel",
      (e) => {
        if (!ready || document.pointerLockElement !== canvas || !inst._vncDecoder) return;
        const dx = Math.max(-1, Math.min(1, e.deltaX || e.wheelDeltaX || 0));
        const dy = Math.max(-1, Math.min(1, -(e.deltaY || 0) || e.wheelDeltaY || 0));
        inst._vncDecoder.onMouseWheel(0, 0, dx, dy);
        e.preventDefault();
      },
      { passive: false }
    );
    canvas.addEventListener("contextmenu", (e) => e.preventDefault());
  }

  async function beginGrab(canvas) {
    keysLocked = await lockSystemKeys();
    try {
      const req = canvas.requestPointerLock({ unadjustedMovement: true });
      if (req && typeof req.then === "function") await req;
    } catch (_) {
      try {
        canvas.requestPointerLock();
      } catch (__) {}
    }
  }

  async function lockSystemKeys() {
    if (!navigator.keyboard || !navigator.keyboard.lock) return false;
    try {
      await navigator.keyboard.lock();
      return true;
    } catch (_) {
      return false;
    }
  }

  function unlockSystemKeys() {
    keysLocked = false;
    if (navigator.keyboard && navigator.keyboard.unlock) {
      navigator.keyboard.unlock();
    }
  }

  function isReleaseCombo(e) {
    return e.code === "Escape" || e.key === "Escape" || e.key === "Esc";
  }

  function controlling() {
    return ready && grabbed;
  }

  function patchKeyboardPassthrough() {
    const inst = widget();
    if (!inst || inst._govmomiKbPatch) return;
    const km = inst._keyboardManager;
    if (!km) return;
    inst._govmomiKbPatch = true;
    if (km.mapMetaToCtrlForKeys) km.mapMetaToCtrlForKeys = [];
    if (Array.isArray(km._guiToCtrlVScanMap)) km._guiToCtrlVScanMap = [];
    if (typeof km.enableWindowsKey === "function") km.enableWindowsKey(false);
    else km._enableWindowsKey = false;
    if (!km.sendVScanKey || typeof WMKS === "undefined") return;

    const LGUI = WMKS.VSCAN_CODES.VSCAN_LGUI;
    const RGUI = WMKS.VSCAN_CODES.VSCAN_RGUI;
    const isGui = (code) =>
      code === "MetaLeft" ||
      code === "MetaRight" ||
      code === "OSLeft" ||
      code === "OSRight";
    const scanOf = (code) => (code.indexOf("Right") >= 0 ? RGUI : LGUI);
    const down = km.onKeyDown.bind(km);
    const up = km.onKeyUp.bind(km);
    km.onKeyDown = function (e) {
      const code = e.originalEvent && e.originalEvent.code;
      if (isGui(code)) {
        const scan = scanOf(code);
        km._clientModifierStatus[scan] = true;
        km.sendVScanKey(scan, true);
        e.preventDefault();
        return false;
      }
      return down(e);
    };
    km.onKeyUp = function (e) {
      const code = e.originalEvent && e.originalEvent.code;
      if (isGui(code)) {
        const scan = scanOf(code);
        km._clientModifierStatus[scan] = false;
        km.sendVScanKey(scan, false);
        e.preventDefault();
        return false;
      }
      return up(e);
    };
  }

  function wireHostKeys() {
    if (document._govmomiHostKeys) return;
    document._govmomiHostKeys = true;
    const steal = (e) => {
      if (!controlling()) return;
      e.preventDefault();
      e.stopImmediatePropagation();
      if (isReleaseCombo(e)) {
        if (e.type === "keydown") leaveControl();
        return;
      }
      const inst = widget();
      const km = inst && inst._keyboardManager;
      if (!km) return;
      const je = $.event.fix(e);
      if (e.type === "keydown") km.onKeyDown(je);
      else if (e.type === "keypress") km.onKeyPress(je);
      else if (e.type === "keyup") km.onKeyUp(je);
    };
    document.addEventListener("keydown", steal, true);
    document.addEventListener("keypress", steal, true);
    document.addEventListener("keyup", steal, true);
  }

  function renderHud(sample = true) {
    const now = performance.now();
    if (sample) {
      const elapsed = now - stats.sampledAt;
      stats.fps = elapsed > 0 ? Math.round(1000 * stats.frames / elapsed) : 0;
      stats.sampledAt = now;
      stats.frames = 0;
    }
    const inst = ready ? widget() : null;
    const size = inst?._guestWidth > 0 && inst?._guestHeight > 0 ?
      inst._guestWidth + "×" + inst._guestHeight : "—";
    const scale = inst?._scale > 0 ? Math.round(inst._scale * 100) + "%" : "—";
    $hud.text((ready ? stats.fps : "—") + " fps").attr("title", t("浏览器每秒画面更新次数"));
    $("#hud-resolution").text(size).attr("title", t("远程画面分辨率"));
    $("#hud-scale").text(scale).attr("title", t("画面显示缩放"));
  }

  function renderPlaceholder() {
    const placeholder = document.getElementById("console-placeholder");
    placeholder.hidden = phase === "connected";
    const key = phase === "error" ? "error" : phase === "connecting" ? "connecting" : currentId ? "disconnected" : "idle";
    document.getElementById("console-placeholder-title").textContent = t("console." + key + "_title");
    document.getElementById("console-placeholder-detail").textContent = t("console." + key + "_detail");
    const reconnect = document.getElementById("console-reconnect");
    document.getElementById("console-reconnect-label").textContent = t("console.reconnect");
    reconnect.hidden = !currentId || phase === "connecting";
    reconnect.disabled = !sessionActive;
  }
  document.getElementById("console-reconnect").addEventListener("click", () => {
    if (!sessionActive || !currentId || !["disconnected", "error"].includes(phase)) return;
    phase = "connecting";
    renderStatus();
    window.parent.postMessage({source:"govmc-console",state:"reconnect"},location.origin);
  });

  function renderStatus(message) {
    const labels = { connecting: "正在连接", connected: "已连接", disconnected: currentId ? "已断开" : "未连接", error: "连接错误" };
    $name.text(currentName || currentId || "—").attr("title", vmLabel());
    $status.text(t(grabbed ? "正在控制" : labels[phase])).attr("data-state", phase);
    $box.attr("data-state", phase);
    renderPlaceholder();
    $status.attr("title", message || (ready ? t("点击画面接管，按 Esc 释放") : ""));
  }

  function report(state) {
    if (state !== "ready") phase = state;
    renderStatus();
    renderHud(false);
    window.parent.postMessage({ source: "govmc-console", state }, location.origin);
  }

  window.addEventListener("message", (event) => {
    if (event.origin !== location.origin || event.source !== window.parent || event.data?.source !== "govmc-workbench") return;
    const data = event.data;
    if (data.type === "appearance") {
      setLanguage(data.language);
      window.govmcTheme.apply(data.theme);
      renderStatus();
      renderHud(false);
      return;
    }
    if (data.type === "machine") {
      currentId = data.id;
      currentName = data.name;
      phase = "connecting";
      renderStatus();
      return;
    }
    if (data.type === "ticket-error") {
      phase = "error";
      renderStatus(data.message);
      return;
    }
    if (data.type === "active") {
      sessionActive = data.active === true;
      renderPlaceholder();
      if (sessionActive) scheduleLayout();
      else leaveControl(true);
      return;
    }
    if (data.type === "disconnect") {
      disconnectConsole();
      return;
    }
    if (data.type !== "connect" || !/^\/ws\/console\?id=[a-f0-9]+(?:&profile=[a-z0-9][a-z0-9._-]{0,63})?$/i.test(data.ws)) return;
    currentId = data.id;
    currentName = data.name;
    phase = "connecting";
    renderStatus();
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    try {
      $box.wmks("connect", proto + "//" + location.host + data.ws);
      scheduleLayout();
    } catch {
      report("error");
    }
  });
  window.addEventListener("blur", () => leaveControl(true));
  window.addEventListener("pagehide", () => {
    leaveControl(true);
    $box.wmks("disconnect");
  });
  document.addEventListener("visibilitychange", () => {
    if (document.hidden) leaveControl(true);
  });
  report("ready");
})();
