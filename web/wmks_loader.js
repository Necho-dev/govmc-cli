// The browser SDK is optional. Native CLI/MCP input never loads these resources.
(() => {
 let pending,loading;
 async function check(force=false){
  if(force)pending=null;
  if(!pending)pending=(async()=>{
   let token='';try{token=sessionStorage.getItem('govmc-api-token')||'';}catch{}
   const response=await fetch('/v1/console/capabilities',{cache:'no-store',headers:token?{Authorization:'Bearer '+token}:{},signal:AbortSignal.timeout(10000)});
   if(!response.ok)return {available:false,state:response.status===401?'auth':response.status===404?'unsupported':'unavailable'};
   const value=await response.json();
   if(!['ready','missing','invalid'].includes(value.state))return {available:false,state:'unsupported'};
   if(value.available&&!/^\/wmks\/[a-f0-9]{64}\/$/.test(value.baseURL))return {available:false,state:'invalid'};
   return value;
  })().catch(()=>({available:false,state:'unavailable'}));
  return pending;
 }
 function resource(tag,url){return new Promise((resolve,reject)=>{const el=document.createElement(tag);if(tag==='script')el.src=url;else{el.rel='stylesheet';el.href=url;}const timer=setTimeout(()=>{el.remove();reject(new Error('wmks_load_timeout'));},8000);el.onload=()=>{clearTimeout(timer);resolve();};el.onerror=()=>{clearTimeout(timer);el.remove();reject(new Error('wmks_load_failed'));};document.head.append(el);});}
 async function load(){if(!loading)loading=(async()=>{const state=await check();if(!state.available)throw new Error('wmks_unavailable');await Promise.all([resource('link','/vendor/jquery-ui.min.css'),resource('link',state.baseURL+'css/wmks-all.css')]);if(!window.jQuery)await resource('script','/vendor/jquery-3.6.0.min.js');if(!window.jQuery.ui)await resource('script','/vendor/jquery-ui.min.js');await resource('script',state.baseURL+'wmks.min.js');if(typeof window.jQuery.fn.wmks!=='function')throw new Error('wmks_invalid_sdk');})().catch(e=>{loading=null;throw e;});return loading;}
 function icon(name) {
  const paths={
   monitor:'M3 4h18v13H3zM8 21h8m-4-4v4',
   terminal:'m5 7 4 5-4 5m7 0h7',
   copy:'M9 9h11v12H9zM15 6V3H4v12h2',
   check:'m5 12 4 4L19 6',
   refresh:'M20 7v5h-5M4 17v-5h5M6 7a7 7 0 0 1 12-2l2 3M4 16l2 3a7 7 0 0 0 12-2',
   info:'M12 10v7m0-10v.1M22 12a10 10 0 1 1-20 0 10 10 0 0 1 20 0'
  };
  const svg=document.createElementNS('http://www.w3.org/2000/svg','svg');
  for(const [key,value] of Object.entries({viewBox:'0 0 24 24',fill:'none',stroke:'currentColor','stroke-width':'1.7','stroke-linecap':'round','stroke-linejoin':'round','aria-hidden':'true',focusable:'false'}))svg.setAttribute(key,value);
  const path=document.createElementNS(svg.namespaceURI,'path');path.setAttribute('d',paths[name]);svg.append(path);return svg;
 }
 function guide(container,state={state:'missing'},onRefresh) {
  const t=window.govmcI18n.t,code=state.state||'unavailable';
  const node=(tag,text,className)=>{const el=document.createElement(tag);if(text)el.textContent=text;if(className)el.className=className;return el;};
  const card=node('section',null,'wmks-guide');card.setAttribute('role','status');
  const title=node('h3');title.append(icon('monitor'),node('span',t('wmks.ui.'+({ready:'load_failed',invalid:'invalid',auth:'auth',unsupported:'unsupported',unavailable:'unavailable',loading:'checking'}[code]||'missing'))));card.append(title);
  const detail=code==='auth'?'auth_detail':code==='unsupported'?'unsupported_detail':code==='ready'?'load_detail':code==='invalid'?'repair_detail':code==='unavailable'?'unavailable_detail':'install_detail';
  card.append(node('p',t('wmks.ui.'+detail),'wmks-intro'));
  if(['missing','invalid','ready'].includes(code)) {
   card.append(node('p',t('wmks.ui.run_on_host'),'wmks-command-label'));
   const row=node('div',null,'wmks-command'),command=node('code',code==='missing'?'govmc wmks install --file /path/to/SDK.zip':'govmc wmks verify');
   const copy=node('button');copy.type='button';copy.append(icon('copy'),node('span',t('wmks.ui.copy')));
   copy.onclick=async()=>{try{await navigator.clipboard.writeText(command.textContent);copy.replaceChildren(icon('check'),node('span',t('wmks.ui.copied')));}catch{const range=document.createRange();range.selectNodeContents(command);const sel=getSelection();sel.removeAllRanges();sel.addRange(range);}};
   row.append(icon('terminal'),command,copy);card.append(row);
   card.append(node('p',t('wmks.ui.same_config'),'wmks-config-note'));
   card.append(node('p',t('wmks.ui.restart'),'wmks-next-step'));
   card.append(node('p',t('wmks.ui.sessions'),'wmks-session-note'));
   const unaffected=node('p',null,'wmks-scope-note');unaffected.append(icon('info'),node('span',t('wmks.ui.unaffected')));card.append(unaffected);
  }
  const refresh=node('button',null,'wmks-recheck');refresh.type='button';refresh.append(icon('refresh'),node('span',t('wmks.ui.recheck')));
  refresh.onclick=async()=>{refresh.disabled=true;const fresh=await check(true);if(onRefresh)onRefresh(fresh);else location.reload();};card.append(refresh);
  container.replaceChildren(card);return card;
 }
 window.govmcWMKS={check,load,guide};
})();
