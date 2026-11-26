async(page)=>{
 await page.evaluate(()=>{if(window.fixtureUnloadGuard)window.removeEventListener('beforeunload',window.fixtureUnloadGuard)});
 await page.unrouteAll({behavior:'wait'});
 // The CLI owns native dialogs. Keep this recovery fixture's inline reload separate
 // from the real refresh/close accept and dismiss checks run by make web-smoke.
 await page.addInitScript(()=>{
  const add=window.addEventListener;
  window.addEventListener=function(type,listener,...args){
   if(type==='beforeunload')window.fixtureUnloadGuard=listener;
   return add.call(this,type,listener,...args);
  };
 });
 const origin='http://127.0.0.1:18765',tickets=[],errors=[];let current='lab',offSecond=false;
 const check=(v,m)=>{if(!v)throw new Error(m)};
 page.on('pageerror',e=>errors.push(e.message));
 await page.route(origin+'/**',async route=>{
  const path=new URL(route.request().url()).pathname,profile=route.request().headers()['x-govmc-profile']||current;
  const reply=json=>route.fulfill({json});
  if(path==='/v1/profiles')return reply({instance:'recovery-server',current,profiles:[{name:'lab',url:'https://lab.example',username:'root'},{name:'prod',url:'https://prod.example',username:'root'}]});
  if(path==='/v1/profiles/connect'){current=route.request().postDataJSON().name;return reply({name:current});}
  if(path==='/v1/console/capabilities')return reply({available:true,state:'ready',baseURL:'/wmks/'+'a'.repeat(64)+'/'});
  if(path==='/ui-preferences')return reply({language:'en'});
  if(path==='/v1/host')return reply({name:'Recovery host',connectionState:'connected',numCpuCores:4,numCpuThreads:8,memoryMB:8192});
  if(path==='/v1/vms')return reply([1,2].map(id=>({id:String(id),uuid:profile+'-uuid-'+id,name:profile+' VM '+id,powerState:offSecond&&id===2?'poweredOff':'poweredOn',numCPU:2,memoryMB:2048})));
  if(/^\/v1\/vms\/\d+\/console$/.test(path)){tickets.push({id:path.split('/')[3],profile});return reply({ws:'/ws/console?id='+tickets.length});}
  if(path==='/v1/datastores')return reply([]);
  if(path==='/v1/tasks'||path.startsWith('/v1/settings/'))return reply({items:[],hasMore:false,active:0});
  if(path.startsWith('/v1/'))return reply({series:[]});
  if(path==='/console.html')return route.fulfill({contentType:'text/html',body:`<!doctype html><p id="name">Connecting</p><script>
   addEventListener('message',event=>{if(event.origin!==location.origin||event.source!==parent)return;const d=event.data;
    if(d.type==='connect'){document.querySelector('#name').textContent=d.name;parent.postMessage({source:'govmc-console',state:'connected'},location.origin)}
    if(d.type==='disconnect')parent.postMessage({source:'govmc-console',state:'disconnected'},location.origin);
   });parent.postMessage({source:'govmc-console',state:'ready'},location.origin);
  </script>`});
  return route.fulfill({path:'web'+(path==='/'?'/index.html':path)});
 });
 await page.setViewportSize({width:1440,height:900});await page.goto(origin+'/settings.html');
 await page.evaluate(()=>{sessionStorage.clear();localStorage.setItem('govmc-language','en')});await page.goto(origin+'/#console');
 await page.locator('#console-machines [data-vm="1"]').click();await page.locator('#connected-sessions [data-vm="1"]').waitFor();

 const dialog=page.locator('#leave-console-confirm');
 const cancel=async()=>{await dialog.locator('button[value="cancel"]').click();await page.waitForFunction(()=>!document.querySelector('#leave-console-confirm').open);};
 const accept=async()=>{await dialog.locator('button[value="leave"]').click();await page.waitForFunction(()=>!document.querySelector('#leave-console-confirm').open);};
 const consoleVisible=async()=>check(await page.locator('#console').isVisible(),'console was hidden before confirmation');
 // All in-document entry points, including logo and mobile navigation.
 for(const selector of ['[data-page="overview"]','.brand']) {
  await page.locator(selector).click();await dialog.waitFor();await consoleVisible();
  check(new URL(page.url()).hash==='#console','navigation changed URL before confirmation');
  check(await dialog.locator('button[value="cancel"]').evaluate(n=>n===document.activeElement),'cancel not focused');
  check((await page.locator('#leave-console-detail').textContent()).includes('background'),'same-document copy claims disconnect');
  await cancel();check(tickets.length===1,'cancel recreated console connection');
 }
 await page.setViewportSize({width:390,height:844});
 await page.locator('#menu').click();await page.locator('[data-page="overview"]').click();await dialog.waitFor();await cancel();
 await page.setViewportSize({width:1440,height:900});
 // Create an earlier overview entry for actual browser Back/Forward traversal.
 await page.locator('[data-page="overview"]').click();await accept();await page.waitForURL('**/#overview');
 await page.locator('[data-page="console"]').click();await page.waitForURL('**/#console');
 await page.goBack();await dialog.waitFor();await page.waitForURL('**/#console');await consoleVisible();
 await cancel();check(tickets.length===1,'cancelled Back destroyed connection');
 await page.goBack();await dialog.waitFor();await page.waitForURL('**/#console');await accept();await page.waitForURL('**/#overview');
 await page.goForward();await page.waitForURL('**/#console');await consoleVisible();
 // Direct hash changes still pass through the same guard.
 await page.evaluate(()=>{location.hash='overview'});await dialog.waitFor();await page.waitForURL('**/#console');
 await page.keyboard.press('Escape');await consoleVisible();
 // Full-document links must prompt, cancel safely, then navigate without a second native prompt.
 for(const selector of ['.settings-link','.datastores-link']) {
  await page.locator(selector).click();await dialog.waitFor();await consoleVisible();
  check((await page.locator('#leave-console-detail').textContent()).includes('disconnect'),'full-document copy omitted disconnect');
  await cancel();
 }
 // A same-page link leaves the active console in place without a warning.
 await page.locator('[data-page="console"]').click();check(!await dialog.isVisible(),'same page prompted');
 check(tickets.length===1,'background navigation recreated sessions');
 // Connecting sessions are protected too; disconnected sessions do not prompt.
 const frame=page.frameLocator('#session-stage iframe');
 await frame.locator('#name').evaluate(()=>parent.postMessage({source:'govmc-console',state:'connecting'},location.origin));
 await page.waitForFunction(()=>document.querySelectorAll('#connected-sessions .connected-row').length===0);
 await page.locator('[data-page="overview"]').click();await dialog.waitFor();await cancel();
 await frame.locator('#name').evaluate(()=>parent.postMessage({source:'govmc-console',state:'connected'},location.origin));
 await page.locator('#connected-sessions [data-vm="1"]').waitFor();
 await page.getByRole('button',{name:'Disconnect from lab VM 1',exact:true}).click();
 await page.locator('#disconnect-confirm button[value="disconnect"]').click();
 await page.waitForFunction(()=>document.querySelectorAll('#connected-sessions .connected-row').length===0);
 check(!await page.evaluate(()=>{const e=new Event('beforeunload',{cancelable:true});dispatchEvent(e);return e.defaultPrevented}),'disconnected session still guards unload');
 await page.locator('[data-page="overview"]').click();await page.waitForURL('**/#overview');
 check(!await dialog.isVisible(),'disconnected session prompted on overview');
 await page.locator('[data-page="console"]').click();
 await page.locator('#console-machines [data-vm="1"]').click();await page.locator('#connected-sessions [data-vm="1"]').waitFor();
 // Set focus inside the console, as during actual remote control, for native exit tests.
 await page.frameLocator('#session-stage iframe').locator('#name').click();
 check(await page.evaluate(()=>navigator.userActivation.hasBeenActive),'iframe interaction did not activate page');
 const guarded=await page.evaluate(()=>{const e=new Event('beforeunload',{cancelable:true});window.dispatchEvent(e);return e.defaultPrevented});
 check(guarded,'registered beforeunload guard did not cancel');
 await page.evaluate(()=>window.consoleNativeCheck='original');
 check(!errors.length,errors.join(';'));
 return {ok:true,overviewCancel:true,logoCancel:true,mobileNavigation:true,browserBackCancel:true,browserBackAccept:true,browserForward:true,directHashCancel:true,settingsCancel:true,datastoresCancel:true,backgroundSessionsPreserved:true,connectingGuard:true,disconnectedNoWarning:true,iframeActivation:true};
}
