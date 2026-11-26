(async () => {
  "use strict";
  const { t, init, apply, setLanguage, getLanguage } = window.govmcI18n;
  await init();
  const $ = (id) => document.getElementById(id);
  let kind = ["logs","audits"].includes(location.hash.slice(1)) ? location.hash.slice(1) : "appearance";
  let offset = 0, generation = 0, controller, snapshotUntil;
  let token = "";
  try { token = sessionStorage.getItem("govmc-api-token") || ""; } catch {}
  function node(tag, text, className) { const el = document.createElement(tag); if (text != null) el.textContent = text; if (className) el.className = className; return el; }
  const statuses = { success: "成功", failed: "失败", unknown: "结果未知", pending: "执行中", denied: "已拒绝", INFO: "信息", WARNING: "警告", ERROR: "错误" };
  function describe(e) {
    const list = node("dl", null, "record-details");
    for (const [label, value] of [["记录 ID", e.id], ["时间", e.time], ["来源", e.source], ["环境", e.profile], ["Original profile", e.originalProfile], ["操作", e.operation || e.event], ["操作者", e.actor], ["结果", t(statuses[e.outcome] || statuses[e.level] || e.outcome || e.level || "—")], ["错误代码", e.code], ["资源", e.resource], ["请求 ID", e.requestId], ["耗时", e.durationMs == null ? "—" : e.durationMs + " ms"]]) {
      list.append(node("dt", t(label)), node("dd", value || "—"));
    }
    $("record-content").replaceChildren(list); $("record-dialog").showModal();
  }
  let environments=[], environmentName="", environmentInstance="", pendingProfile, environmentState="unknown", switching=false;
 function address(profile){try{return profile.username+"@"+new URL(profile.url).host;}catch{return profile.username+"@"+profile.url;}}
 function renderProfiles(){
  const picker=$("environment"), profile=environments.find(item=>item.name===environmentName);picker.hidden=!environments.length;
  picker.querySelector(".environment-initials").textContent=profile?Array.from(profile.name).slice(0,2).join("").toUpperCase():"··";
  const status=t(({connected:"已连接",connecting:"正在连接",error:"连接异常",disconnected:"已断开"})[environmentState]||"未知");
  picker.querySelector("summary").title=profile?profile.name+" · "+address(profile)+" · "+status:t("切换环境");
  picker.querySelector("summary").setAttribute("aria-label",t("切换环境")+" · "+(profile?.name||t("未知"))+" · "+status);
  picker.querySelector(".environment-dot").dataset.state=environmentState;
  const options=picker.querySelector(".dropdown-options");options.replaceChildren();
  for(const item of environments){const button=node("button");button.type="button";button.disabled=switching;button.setAttribute("aria-pressed",String(item.name===environmentName));button.append(node("strong",item.name),node("small",address(item)));button.addEventListener("click",()=>{
   if(switching||item.name===environmentName&&environmentState==="connected"){picker.open=false;return;}
   const current=environments.find(p=>p.name===environmentName);pendingProfile=item.name;$("environment-from-name").textContent=current?.name||t("未知");$("environment-from-address").textContent=current?address(current):"";$("environment-to-name").textContent=item.name;$("environment-to-address").textContent=address(item);picker.open=false;$("environment-confirm").returnValue="";$("environment-confirm").showModal();
  });options.append(button);}
 }
 async function loadProfiles(){
  try{
   const response=await fetch("/v1/profiles",{headers:token?{Authorization:"Bearer "+token}:{},cache:"no-store",signal:AbortSignal.timeout(4000)});
   if(!response.ok)return;
   const data=await response.json();environments=data.profiles||[];environmentInstance=data.instance;environmentName=data.current||data.active||"";
   environmentState=environmentName===data.current?"connected":"unknown";
   try{
    const saved=JSON.parse(sessionStorage.getItem("govmc-web-profile")||"null");
    if(saved?.instance===data.instance&&environments.some(p=>p.name===saved.name)){
     environmentName=saved.name;
     environmentState=["connected","error","disconnected"].includes(saved.connection)?saved.connection:"connected";
    }
   }catch{}
   if(!$("profile").value)$("profile").value=environmentName;
   renderProfiles();query();
  }catch{}
 }
 $("environment-confirm").addEventListener("close",async()=>{
  const name=pendingProfile;pendingProfile=null;if($("environment-confirm").returnValue!=="switch"||!name||switching)return;
  switching=true;const oldState=environmentState;environmentState="connecting";renderProfiles();
  try{const response=await fetch("/v1/profiles/connect",{method:"POST",headers:{"Content-Type":"application/json",...(token?{Authorization:"Bearer "+token}:{})},body:JSON.stringify({name}),signal:AbortSignal.timeout(35000)});if(!response.ok)throw new Error(t("切换环境失败"));if(!$("profile").value||$("profile").value===environmentName)$("profile").value=name;environmentName=name;environmentState="connected";snapshotUntil=null;offset=0;query();try{sessionStorage.setItem("govmc-web-profile",JSON.stringify({instance:environmentInstance,name,connection:environmentState}));}catch{}}
  catch(error){environmentState=oldState;$("notice").textContent=error.message;$("notice").hidden=false;}
  finally{switching=false;renderProfiles();}
 });
  function render(data) {
    const table = node("table");
    const header = node("tr");
    for (const label of ["时间", "级别", "来源", "环境", kind === "audits" ? "操作" : "事件", "结果", "耗时"]) header.append(node("th", t(label)));
    table.append(node("thead")); table.tHead.append(header);
    const body = node("tbody"); table.append(body);
    for (const entry of data.items || []) {
      const row = node("tr"); const values = [new Date(entry.time).toLocaleString(document.documentElement.lang, { hour12: false }), t(statuses[entry.level] || entry.level), entry.source, entry.profile || "—", entry.operation || entry.event, t(statuses[entry.outcome] || entry.outcome || "—"), entry.durationMs == null ? "—" : entry.durationMs + " ms"];
      values.forEach((value, index) => { const cell = node("td"); if (index === 4) { const button = node("button", value, "record-link"); button.type = "button"; button.addEventListener("click", () => describe(entry)); cell.append(button); } else cell.textContent = value; if (index === 5) cell.dataset.outcome = entry.outcome; row.append(cell); });
      body.append(row);
    }
    $("records").replaceChildren((data.items || []).length ? table : node("p", t("暂无匹配记录"), "empty"));
    $("previous").disabled = offset === 0; $("next").disabled = !data.hasMore;
    $("page-number").textContent = t("第 {page} 页", { page: Math.floor(offset / 50) + 1 });
  }
  const recordCache=new Map();
  async function query() {
 if(kind==="appearance"){controller?.abort();return;}
    const id = ++generation;
    controller?.abort(); controller = new AbortController();
    if (!snapshotUntil) snapshotUntil = new Date().toISOString();
 const params = new URLSearchParams({ offset, limit: 50, until: snapshotUntil });
    for (const name of ["search", "source", "profile", "level", "outcome"]) if ($(name).value) params.set(name, $(name).value);
    for (const name of ["since", "until"]) if ($(name).value) params.set(name, new Date($(name).value).toISOString());
    const cacheParams=new URLSearchParams(params);if(!$("until").value)cacheParams.delete("until");const cacheKey=kind+"?"+cacheParams;
    const cached=recordCache.get(cacheKey);
    if(cached&&Date.now()-cached.at<30000)render(cached.data);else $("records").replaceChildren(node("p",t("files.loading"),"empty"));
    $("notice").hidden = true; $("records").setAttribute("aria-busy", "true");$("refresh").setAttribute("aria-busy","true");$("refresh").disabled=true;
    try {
      const headers = token ? { Authorization: "Bearer " + token } : {};
      const response = await fetch("/v1/settings/" + kind + "?" + params, { headers, cache: "no-store", signal: AbortSignal.any([controller.signal, AbortSignal.timeout(8000)]) });
      if (response.status === 401) { $("token-dialog").showModal(); throw new Error(t("请输入 API 令牌后重试")); }
      if (!response.ok) throw new Error(t(response.status === 400 ? "筛选条件无效，请检查时间范围和分页" : "日志存储暂不可用，请稍后重试"));
      const data = await response.json(); if (id !== generation) return;
      recordCache.set(cacheKey,{at:Date.now(),data});
      for(const [key,value] of recordCache)if(Date.now()-value.at>30000)recordCache.delete(key);
      while(recordCache.size>16)recordCache.delete(recordCache.keys().next().value);
      render(data);
    } catch (error) {
      if (id !== generation || error.name === "AbortError") return;
      $("notice").textContent = error.message; $("notice").hidden = false;
    } finally { if (id === generation){$("records").setAttribute("aria-busy", "false");$("refresh").setAttribute("aria-busy","false");$("refresh").disabled=false;} }
  }
  function tabs() {
    for (const button of document.querySelectorAll("[data-record-kind]")) { const active = button.dataset.recordKind === kind; button.setAttribute("aria-selected", String(active)); button.tabIndex = active ? 0 : -1; }
    $("query-form").dataset.kind=kind;
 $("appearance-panel").hidden = kind!=="appearance";
 $("record-panel").hidden = kind==="appearance";
 $("record-panel").setAttribute("aria-labelledby", "tab-"+kind);
 $("records-title").textContent = t(kind === "logs" ? "运行日志" : "操作审计");
    $("outcome-label").hidden = kind !== "audits";
  }
  function select(next) { if (next === kind) return; kind = next; offset = 0; snapshotUntil = null; $("level").value = ""; $("outcome").value = ""; history.replaceState(null, "", "#" + kind); tabs(); query(); }
  $("record-tabs").addEventListener("click", (event) => { const button = event.target.closest("[data-record-kind]"); if (button) select(button.dataset.recordKind); });
  $("record-tabs").addEventListener("keydown", (event) => { if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return; event.preventDefault(); const names=["appearance","logs","audits"];const step=event.key==="ArrowLeft"?-1:1;select(event.key==="Home"?names[0]:event.key==="End"?names[2]:names[(names.indexOf(kind)+step+3)%3]); document.querySelector('[data-record-kind="' + kind + '"]').focus(); });
  $("query-form").addEventListener("submit", (event) => { event.preventDefault(); offset = 0; snapshotUntil = null; query(); });
  $("previous").addEventListener("click", () => { offset = Math.max(0, offset - 50); query(); });
  $("next").addEventListener("click", () => { offset += 50; query(); });
  $("refresh").addEventListener("click", () => { offset=0; snapshotUntil=null; query(); });
  function language(value){setLanguage(value);try{localStorage.setItem("govmc-language",getLanguage());}catch{}$("preference-language").value=getLanguage();for(const button of document.querySelectorAll("[data-language]"))button.setAttribute("aria-pressed",String(button.dataset.language===getLanguage()));tabs();query();}
 $("language").addEventListener("click", (event) => { const button = event.target.closest("[data-language]"); if (!button) return; language(button.dataset.language); $("language").open = false; });
 $("preference-language").value=getLanguage();for(const button of document.querySelectorAll("[data-language]"))button.setAttribute("aria-pressed",String(button.dataset.language===getLanguage()));$("preference-language").addEventListener("change",event=>language(event.target.value));
  $("theme").addEventListener("click", (event) => { const button = event.target.closest("[data-theme-option]"); if (!button) return; const value = button.dataset.themeOption; try { localStorage.setItem("govmc-theme", value); } catch {} appearance(value); });
  function appearance(value) { value=window.govmcTheme.apply(value); for(const button of document.querySelectorAll("[data-preference-theme]"))button.setAttribute("aria-pressed",String(button.dataset.preferenceTheme===value)); }
  try { appearance(localStorage.getItem("govmc-theme") || "system"); } catch {appearance("system");}
 document.querySelectorAll("[data-preference-theme]").forEach(button=>button.addEventListener("click",()=>{const value=button.dataset.preferenceTheme;try{localStorage.setItem("govmc-theme",value);}catch{}appearance(value);}));
 $("reset-appearance").addEventListener("click",()=>{try{localStorage.setItem("govmc-theme","system");}catch{}appearance("system");});
 window.addEventListener("storage",event=>{if(event.key==="govmc-theme")appearance(event.newValue||"system");if(event.key==="govmc-language"){setLanguage(event.newValue);$("preference-language").value=getLanguage();tabs();query();}});
  $("token-form").addEventListener("submit", (event) => { event.preventDefault(); token = $("api-token").value; try { sessionStorage.setItem("govmc-api-token", token); } catch {} $("api-token").value = ""; $("token-dialog").close(); loadProfiles(); query(); });
  document.querySelectorAll("[data-close-dialog]").forEach((button) => button.addEventListener("click", () => button.closest("dialog").close()));
  $("menu").addEventListener("click",()=>{$("menu").setAttribute("aria-expanded",String($("nav").classList.toggle("open")));});
 document.addEventListener("click",(event)=>{for(const picker of document.querySelectorAll(".dropdown[open]")){if(!picker.contains(event.target))picker.open=false;}});
 apply(); appearance(document.documentElement.dataset.theme); tabs(); loadProfiles(); query();
})();
