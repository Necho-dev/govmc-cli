(() => {
  const themes = ["system", "light", "dark", "magic-blue", "classic-dark"];
  const normalizeTheme = value => themes.includes(value) ? value : "system";
  const themeFamily = value => ["magic-blue", "classic-dark"].includes(value) ? "dark" : normalizeTheme(value);
  const readTheme = () => { try { return normalizeTheme(localStorage.getItem("govmc-theme")); } catch { return "system"; } };
  const applyTheme = value => {
    value = normalizeTheme(value);
    document.documentElement.dataset.theme = value;
    for (const button of document.querySelectorAll("[data-theme-option]")) {
      button.setAttribute("aria-pressed", String(button.dataset.themeOption === themeFamily(value)));
      const label = value === "magic-blue" ? "theme.magic_blue" : value === "classic-dark" ? "theme.classic_dark" : "深色";
      if (button.dataset.themeOption === "dark") {
        button.title = t(label);
        button.setAttribute("aria-label", t(label));
      }
    }
    return value;
  };
  window.govmcTheme = {normalize:normalizeTheme, family:themeFamily, read:readTheme, apply:applyTheme};
  let catalogs = {en:{}, zh:{}}, aliases = {};
  const ready = Promise.all(["en", "zh", "_aliases"].map(async name => {
    const response = await fetch("/locales/"+name+".json", {cache:"no-cache"});
    if (!response.ok) throw new Error("Translation catalog unavailable: "+name);
    return response.json();
  })).then(([en, zh, keys]) => { catalogs = {en, zh}; aliases = keys; });
  ready.catch(() => {});

  let language = "zh";
  const normalize = (value) => /^(zh|en)(?:[-_.]|$)/i.exec(value || "")?.[1].toLowerCase();
  const t = (label, values) => {
    const key = aliases[label] || label;
    const text = catalogs[language]?.[key] ?? catalogs.en[key] ?? label;
    return values ? text.replace(/\{(\w+)\}/g, (match, key) => values[key] ?? match) : text;
  };
  function setPageTitle(label) {
    const title=document.querySelector("title");
    if (title) {title.dataset.pageTitle=label;title.removeAttribute("data-i18n");}
    document.title=t(label)+" - GoVMC WebUI";
  }
  function apply() {
    document.documentElement.lang = language === "zh" ? "zh-CN" : "en";
    const pageTitle=document.querySelector("title")?.dataset.pageTitle;
    if(pageTitle) setPageTitle(pageTitle);
    document.querySelectorAll("[data-i18n]").forEach((node) => { node.textContent = t(node.dataset.i18n); });
    for (const attr of ["aria-label", "placeholder", "title"]) {
      document.querySelectorAll("[data-i18n-" + attr + "]").forEach((node) => {
        node.setAttribute(attr, t(node.getAttribute("data-i18n-" + attr)));
      });
    }
  }
  function setLanguage(value) {
    language = normalize(value) || "zh";
    apply();
    applyTheme(document.documentElement.dataset.theme);
 window.dispatchEvent(new Event("govmc-language"));
  }
  let initialization;
  function init(){return initialization ||= initialize();}
  async function initialize() {
    await ready;
    const frameLanguage = normalize(new URLSearchParams(location.search).get("lang"));
    let saved;
    try {
      saved = normalize(localStorage.getItem("govmc-language"));
      applyTheme(readTheme());
    } catch {}
    if (frameLanguage || saved) { setLanguage(frameLanguage || saved); return; }
    let configured;
    try {
      const response = await fetch("/ui-preferences", { signal: AbortSignal.timeout(2000) });
      if (response.ok) configured = normalize((await response.json()).language);
    } catch {}
    setLanguage(configured || navigator.languages.map(normalize).find(Boolean) || "en");
  }
  window.govmcI18n = { t, apply, init, setLanguage, getLanguage: () => language, setPageTitle };
})();
