/* Shared single-select controls. Keep native values/events for existing forms. */
(() => {
  "use strict";
  const controls = new Map();
  let active, sequence = 0;
  function close() {
    if (!active) return;
    const c = active; active = null;
    if (c.list.matches(":popover-open")) c.list.hidePopover();
    c.list.hidden = true;
    c.button.setAttribute("aria-expanded", "false");
    c.button.removeAttribute("aria-activedescendant");
  }
  function position(c) {
    const rect = c.button.getBoundingClientRect(), gap = 5, edge = 8;
    const width = Math.min(Math.max(rect.width, 150), innerWidth - edge * 2);
    const below = innerHeight - rect.bottom - gap - edge, above = rect.top - gap - edge;
    const up = below < 160 && above > below;
    c.list.style.width = width + "px";
    c.list.style.maxHeight = Math.max(60, Math.min(280, up ? above : below)) + "px";
    c.list.style.left = Math.max(edge, Math.min(rect.left, innerWidth - width - edge)) + "px";
    c.list.style.top = (up ? Math.max(edge, rect.top - gap - c.list.offsetHeight) : rect.bottom + gap) + "px";
  }
  function highlight(c, index) {
    const enabled = c.items.filter(item => !item.disabled);
    if (!enabled.length) return;
    c.index = enabled[Math.max(0, Math.min(index, enabled.length - 1))].index;
    c.items.forEach(item => item.node.classList.toggle("is-active", item.index === c.index));
    const item = c.items[c.index];
    c.button.setAttribute("aria-activedescendant", item.node.id);
    item.node.scrollIntoView({block:"nearest"});
  }
  function sync(c) {
    c.text.textContent = c.select.selectedOptions[0]?.textContent || "—";
    c.button.disabled = c.select.matches(":disabled");
    c.button.title = c.text.textContent;
    const labelled = c.select.getAttribute("aria-label");
    if (labelled) c.button.setAttribute("aria-label", labelled);
    else {
      const label = c.select.labels?.[0], span = label?.querySelector("span");
      if (span && !c.wrapper.contains(span)) {
        span.id ||= "select-label-" + ++sequence;
        c.button.setAttribute("aria-labelledby", span.id);
      }
    }
    if (active === c) render(c);
  }
  function render(c) {
    c.list.replaceChildren();
    c.items = Array.from(c.select.options, (option, index) => {
      const node = document.createElement("div");
      node.id = c.list.id + "-" + index;
      node.role = "option";
      node.className = "select-option";
      node.textContent = option.textContent;
      node.setAttribute("aria-label", option.textContent);
      node.setAttribute("aria-selected", String(option.selected));
      const disabled = option.disabled || option.parentElement.disabled;
      node.setAttribute("aria-disabled", String(!!disabled));
      node.hidden = option.hidden;
      node.addEventListener("pointerdown", event => event.preventDefault());
      node.addEventListener("click", event => { event.preventDefault(); if (!disabled) choose(c,index); });
      c.list.append(node);
      return {node, index, disabled: disabled || option.hidden};
    });
  }
  function open(c) {
    close(); sync(c); if (c.button.disabled) return;
    active = c; render(c); c.list.hidden = false;
    c.list.showPopover();
    c.button.setAttribute("aria-expanded", "true");
    position(c);
    const enabled = c.items.filter(item => !item.disabled);
    highlight(c, Math.max(0, enabled.findIndex(item => item.index === c.select.selectedIndex)));
  }
  function choose(c, index) {
    const changed = c.select.selectedIndex !== index;
    c.select.selectedIndex = index;
    close(); sync(c); c.button.focus({preventScroll:true});
    if (changed) {
      c.select.dispatchEvent(new Event("input", {bubbles:true}));
      c.select.dispatchEvent(new Event("change", {bubbles:true}));
    }
  }
  function enhance(select) {
    if (controls.has(select) || select.multiple || select.size > 1) return;
    const wrapper = document.createElement("span"), button = document.createElement("button"), text = document.createElement("span"), list = document.createElement("div");
    wrapper.className = "custom-select";
    button.type = "button"; button.className = "select-trigger"; button.role = "combobox";
    button.setAttribute("aria-haspopup", "listbox"); button.setAttribute("aria-expanded", "false");
    text.className = "select-value"; button.append(text);
    list.id = "select-options-" + ++sequence; list.className = "select-options"; list.role = "listbox";
    list.setAttribute("popover", "manual"); list.hidden = true;
    button.setAttribute("aria-controls", list.id);
    const label = select.labels?.[0];
    if (label && !label.querySelector("span")) {
      const nodes = [...label.childNodes].filter(node=>node.nodeType===Node.TEXT_NODE);
      const labelText = nodes.map(node=>node.textContent).join("").trim();
      if (labelText) { const span = document.createElement("span"); span.textContent=labelText; label.prepend(span); nodes.forEach(node=>node.remove()); }
    }
    select.before(wrapper); wrapper.append(select, button, list);
    select.classList.add("select-native"); select.tabIndex = -1; select.setAttribute("aria-hidden", "true");
    const c = {select, wrapper, button, text, list, items:[], index:0, search:"", searchAt:0};
    controls.set(select,c); sync(c);
    button.addEventListener("click", event => { event.preventDefault(); active === c ? close() : open(c); });
    button.addEventListener("focus", () => sync(c));
    button.addEventListener("keydown", event => {
      const key = event.key;
      if (key === "Tab") { close(); return; }
      if (key === "Escape" && active === c) { event.preventDefault(); event.stopPropagation(); close(); return; }
      if (["ArrowDown","ArrowUp","Home","End","Enter"," "].includes(key)) {
        event.preventDefault(); event.stopPropagation();
        if ((key === "Enter" || key === " ") && active === c) { choose(c,c.index); return; }
        if (active !== c) { open(c); if (key !== "Home" && key !== "End") return; }
        const enabled = c.items.filter(item => !item.disabled), index = enabled.findIndex(item => item.index === c.index);
        highlight(c,key === "Home" ? 0 : key === "End" ? enabled.length-1 : index + (key === "ArrowUp" ? -1 : 1));
      } else if (key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey) {
        event.preventDefault(); if (active !== c) open(c);
        c.search = Date.now()-c.searchAt > 700 ? key : c.search+key; c.searchAt=Date.now();
        const enabled = c.items.filter(item=>!item.disabled);
        const index = enabled.findIndex(item=>item.node.textContent.toLocaleLowerCase().startsWith(c.search.toLocaleLowerCase()));
        if (index >= 0) highlight(c,index);
      }
    });
    select.addEventListener("change",()=>sync(c));
    select.addEventListener("invalid", event=>{event.preventDefault();button.focus();open(c);});
    new MutationObserver(()=>sync(c)).observe(select,{subtree:true,childList:true,characterData:true,attributes:true,attributeFilter:["disabled","selected","label","aria-label","hidden"]});
  }
  function scan(root) {
    if (root.nodeType !== 1) return;
    if (root.matches("select")) enhance(root);
    root.querySelectorAll("select").forEach(enhance);
  }
  function init() {
    scan(document.documentElement);
    new MutationObserver(records=>{
      for (const record of records) for (const node of record.addedNodes) scan(node);
      for (const [select,c] of controls) if (!select.isConnected) { if (active === c) close(); controls.delete(select); }
    }).observe(document.body,{childList:true,subtree:true});
    document.addEventListener("pointerdown",event=>{if(active&&!active.wrapper.contains(event.target))close();},true);
    document.addEventListener("scroll",event=>{if(active&&!active.list.contains(event.target))position(active);},true);
    document.addEventListener("close",close,true);
    window.addEventListener("resize",close);
    document.addEventListener("reset",()=>requestAnimationFrame(()=>controls.forEach(sync)));
  }
  window.govmcSelect={sync(select){const control=controls.get(select);if(control)sync(control);}};
  if(document.readyState === "loading") document.addEventListener("DOMContentLoaded",init,{once:true}); else init();
})();
