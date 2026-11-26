async(page)=>{
 await page.emulateMedia({reducedMotion:'no-preference'});
 await page.unrouteAll({behavior:'wait'});
 const origin='http://127.0.0.1:18765',errors=[];page.on('pageerror',error=>errors.push(error.message));
 let holdMain=false,holdDirectory=false,metricsError=false,releaseMain,releaseDirectory;
 const mainGate=new Promise(resolve=>releaseMain=resolve),directoryGate=new Promise(resolve=>releaseDirectory=resolve);
 const machine=name=>({id:'1',name,powerState:'poweredOn',connectionState:'connected',numCPU:2,memoryMB:4096,cpuPct:5,memActivePct:10,netRxMbps:1,netTxMbps:2,disks:[],networks:[],guestOS:'Fixture OS'});
 const stores=[{name:'datastore1',type:'VMFS',capacity:1024**4,freeSpace:500*1024**3,accessible:true,vmCount:1}];
 await page.route(origin+'/**',async route=>{
  const path=new URL(route.request().url()).pathname;
 if(path==="/v1/console/capabilities")return route.fulfill({json:{available:false,state:"missing"}});
 if(path==="/v1/tasks")return route.fulfill({json:{items:[],hasMore:false,active:0}});
  const json=value=>route.fulfill({contentType:'application/json',body:JSON.stringify(value)});
  if(path==='/ui-preferences')return json({language:'zh'});
  if(path==='/v1/profiles')return json({instance:'cache-test',current:'lab',profiles:[{name:'lab',url:'https://lab.example',username:'root'}]});
  if(path.startsWith('/v1/settings/'))return json({items:[],hasMore:false});
  if(path==='/v1/vms'){if(holdMain)await mainGate;return json([machine('Fresh machine')]);}
  if(path==='/v1/host'){if(holdMain)await mainGate;return json({name:'Fresh host',connected:true});}
  if(path==='/v1/datastores'){if(holdDirectory)await directoryGate;return json(stores);}
  if(path==='/v1/datastores/entries'){if(holdDirectory)await directoryGate;return json([{path:'Fresh.txt',folder:false,size:123}]);}
  if(path.includes('metrics'))return metricsError?route.fulfill({status:503,contentType:'application/json',body:'{"message":"fixture metrics unavailable"}'}):json({series:[]});
  if(path.startsWith('/v1/'))return json([]);
  return route.fulfill({path:'web'+(path==='/'?'/index.html':path)});
 });
 const check=(value,message)=>{if(!value)throw new Error(message);};
 await page.goto(origin+'/settings.html');await page.getByRole('heading',{name:'主题',exact:true}).waitFor();
 await page.evaluate(async({stores,machine})=>{
  const cache=window.govmcCache;
  sessionStorage.setItem('govmc-cache:v1:legacy','temporary old cache');localStorage.setItem('govmc-cache:v1:legacy','temporary old cache');
  await cache.invalidate('cache-test','lab');await cache.invalidate('cache-test','prod');
  await cache.write('cache-test','lab','workbench:'+cache.tab,{host:{name:'Cached host',connected:true},vms:[machine],stores,metrics:{series:[]},updated:new Date().toISOString(),ui:{search:'',range:'60'}});
  await cache.write('cache-test','prod','test',{marker:'other-profile'});
 },{stores,machine:machine('Cached machine')});
 holdMain=true;await page.goto(origin+'/#overview');
 await page.getByRole('button',{name:/Cached machine/}).first().waitFor({timeout:3000});
 check((await page.locator('#host-range').evaluate(el=>el.nextElementSibling.textContent)).includes('60'),'cached range label not synchronized');
 check(await page.locator('.vm-edit-action').first().isDisabled(),'stale snapshot allowed writes');
 check(await page.getByText('Cached host',{exact:true}).isVisible(),'cached host missing while API is pending');
 check(await page.locator('#refresh').getAttribute('aria-busy')==='true','refresh status missing');
 check(await page.locator('#refresh svg').evaluate(el=>getComputedStyle(el).animationName)==='refresh-spin','refresh icon not spinning');
 await page.emulateMedia({reducedMotion:'reduce'});
 check(await page.locator('#refresh svg').evaluate(el=>getComputedStyle(el).animationName)==='none','reduced-motion preference ignored');
 await page.emulateMedia({reducedMotion:'no-preference'});
 releaseMain();holdMain=false;await page.getByRole('button',{name:/Fresh machine/}).first().waitFor();
 await page.waitForFunction(()=>document.querySelector('#refresh').getAttribute('aria-busy')==='false');
 check(await page.getByRole('button',{name:/Cached machine/}).count()===0,'background refresh did not replace cached rows');
 metricsError=true;await page.reload();await page.getByText(/刷新失败/).waitFor();
 await page.waitForFunction(()=>document.querySelector('#refresh').getAttribute('aria-busy')==='false');
 check(await page.locator('.vm-edit-action').first().isEnabled(),'unavailable metrics locked a freshly verified VM inventory');metricsError=false;
 await page.evaluate(async stores=>{
  await window.govmcCache.write('cache-test','lab','datastores',stores);
  await window.govmcCache.write('cache-test','lab','directory:datastore1:',[{path:'Cached.txt',folder:false,size:12}]);
 },stores);
 holdDirectory=true;await page.goto(origin+'/datastores.html');
 await page.getByRole('button',{name:'Cached.txt',exact:true}).waitFor({timeout:3000});
 check(await page.locator('#mkdir').isDisabled(),'write controls enabled on stale directory data');
 releaseDirectory();holdDirectory=false;await page.getByRole('button',{name:'Fresh.txt',exact:true}).waitFor();
 check(await page.getByRole('button',{name:'Cached.txt',exact:true}).count()===0,'fresh directory did not replace cache');
 const result=await page.evaluate(async()=>{
  const cache=window.govmcCache;
  await cache.invalidate('cache-test','lab');
  const cleared=await cache.read('cache-test','lab','datastores');
  const isolated=await cache.read('cache-test','prod','test');
  await cache.write('cache-test','lab','expired',{marker:'expired-test'});
  const db=await new Promise((resolve,reject)=>{const request=indexedDB.open('govmc-read-cache',1);request.onsuccess=()=>resolve(request.result);request.onerror=()=>reject(request.error);});
  await new Promise(resolve=>{const tx=db.transaction('snapshots','readwrite'),store=tx.objectStore('snapshots'),request=store.openCursor();request.onsuccess=()=>{const cursor=request.result;if(cursor){if(cursor.value.value.marker==='expired-test')cursor.update({...cursor.value,expires:Date.now()-1});cursor.continue();}};tx.oncomplete=resolve;});db.close();
  const expired=await cache.read('cache-test','lab','expired');
  return {cleared:cleared===null,isolated:isolated?.value.marker==='other-profile',expired:expired===null,noWebStorageCache:![...Object.keys(localStorage),...Object.keys(sessionStorage)].some(key=>key.startsWith('govmc-cache'))};
 });
 check(Object.values(result).every(Boolean),JSON.stringify(result));
 await page.addInitScript(()=>{Object.defineProperty(window,'indexedDB',{configurable:true,get(){throw new Error('unavailable fixture');}});Object.defineProperty(window.crypto,'randomUUID',{configurable:true,value:undefined});});
 await page.goto(origin+'/settings.html');await page.getByRole('heading',{name:'主题',exact:true}).waitFor();
 const fallback=await page.evaluate(async()=>{await window.govmcCache.write('fallback','lab','data',{ok:true});return (await window.govmcCache.read('fallback','lab','data'))?.value.ok;});
 check(fallback,'IndexedDB failure did not degrade gracefully');
 check(!errors.length,errors.join('\n'));
 return {ok:true,indexedDB:true,cachedThenFresh:true,writeGuard:true,...result};
}
