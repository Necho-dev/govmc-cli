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
 await page.locator('#console-machines [data-vm="2"]').click();await page.locator('#connected-sessions [data-vm="2"]').waitFor();
 await page.locator('#connected-sessions [data-vm="1"]').click();
 check(tickets.length===2,'initial console tickets mismatch');
 await page.locator('[data-page="overview"]').click();
 await page.locator('#leave-console-confirm[open]').waitFor();
 check(new URL(page.url()).hash==='#console','overview changed before confirmation');
 await page.keyboard.press('Escape');
 check(new URL(page.url()).hash==='#console','cancelled overview left console');
 await page.locator('[data-page="overview"]').click();
 await page.locator('#leave-console-confirm button[value="leave"]').click();
 await page.waitForURL('**/#overview');
 await page.locator('[data-page="console"]').click();
 check(tickets.length===2,'in-page navigation recreated sessions');
 await page.waitForFunction(()=>document.title==='Console - GoVMC WebUI');
 const saved=await page.evaluate(()=>JSON.parse(sessionStorage.getItem('govmc-console:v1:'+JSON.stringify(['recovery-server','lab']))));
 check(saved.sessions.length===2&&saved.active==='1','session selection was not saved');
 check(Object.keys(saved.sessions[0]).sort().join() === 'id,name,uuid','saved session includes ticket or unexpected data');
 // Test the registered guard without interrupting the CLI runner's native-dialog transport.
 const guarded=await page.evaluate(()=>{const event=new Event('beforeunload',{cancelable:true});window.dispatchEvent(event);return event.defaultPrevented;});
 check(guarded,'refresh with live sessions did not install a warning');
 check(tickets.length===2,'guard invocation recreated sessions');
 await page.evaluate(()=>window.removeEventListener("beforeunload",window.fixtureUnloadGuard));
 await page.reload();await page.waitForFunction(()=>document.querySelectorAll('#connected-sessions .connected-row').length===2);
 check(tickets.length===4,'refresh did not get new tickets for both sessions');
 check(await page.locator('#connected-sessions [data-vm="1"]').getAttribute('aria-pressed')==='true','refresh lost active machine');
 await page.getByRole('link',{name:'Settings',exact:true}).click();await page.locator('#leave-console-confirm').waitFor();
 check(await page.locator('#leave-console-confirm button[value="cancel"]').evaluate(n=>n===document.activeElement),'leaving page does not default to Cancel');
 await page.keyboard.press('Escape');check(new URL(page.url()).pathname==='/','cancelled navigation left workspace');
 await page.getByRole('link',{name:'Settings',exact:true}).click();await page.locator('#leave-console-confirm button[value="leave"]').click();
 await page.waitForURL('**/settings.html');await page.waitForFunction(()=>document.title==='Settings - GoVMC WebUI');
 await page.getByRole('link',{name:'Overview',exact:true}).click();
 await page.waitForFunction(()=>document.querySelectorAll('#connected-sessions .connected-row').length===2);
 check(new URL(page.url()).hash==='#overview','session restore unexpectedly navigated away from Overview');
 check(tickets.length===6,'returning to workspace did not recover connections');
 await page.locator('#environment summary').click();await page.locator('#environment [data-profile="prod"]').click();
 await page.locator('#environment-confirm button[value="switch"]').click();await page.getByText('prod VM 1',{exact:true}).first().waitFor();
 check(await page.locator('#session-stage iframe').count()===0,'profile switch retained old frames');
 check(await page.evaluate(()=>sessionStorage.getItem('govmc-console:v1:'+JSON.stringify(['recovery-server','lab'])))===null,'profile switch retained restorable old sessions');
 // Recovery must not connect to a reused ID or a powered-off machine.
 offSecond=true;
 await page.evaluate(()=>sessionStorage.setItem('govmc-console:v1:'+JSON.stringify(['recovery-server','prod']),JSON.stringify({active:'1',savedAt:Date.now(),sessions:[{id:'1',name:'prod VM 1',uuid:'previous-machine-uuid'},{id:'2',name:'prod VM 2',uuid:'prod-uuid-2'}]})));
 const verifiedInventory=page.waitForResponse(response=>new URL(response.url()).pathname==='/v1/vms');
 await page.reload();await verifiedInventory;await page.waitForFunction(()=>document.querySelector('#refresh').getAttribute('aria-busy')==='false');
 check(tickets.length===6&&await page.locator('#session-stage iframe').count()===0,'unsafe identity or powered-off recovery');
 // Leave a connected session for separate native dialog accept/dismiss CLI checks.
 await page.locator('[data-page="console"]').click();
 await page.locator('#console-machines [data-vm="1"]').click();
 await page.locator('#connected-sessions [data-vm="1"]').waitFor();
 await page.evaluate(()=>{window.consoleNativeCheck='original'});
 check(!errors.length,errors.join(';'));
 return {ok:true,refreshGuard:true,restoreTwoSessions:true,selectedMachine:true,freshTickets:true,hashNavigationPreserves:true,leaveConfirmation:true,noDoublePrompt:true,returnToWorkspace:true,profileIsolation:true,identityValidation:true,poweredOffNotRestored:true};
}
