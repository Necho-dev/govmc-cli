async(page)=>{
 await page.unrouteAll({behavior:'wait'});const origin='http://127.0.0.1:18765',errors=[],requests=[];let state='missing',tickets=0;
 page.on('pageerror',e=>errors.push(e.message));
 await page.route(origin+'/**',async route=>{const path=new URL(route.request().url()).pathname;requests.push(path);const json=value=>route.fulfill({json:value});
 if(path==='/v1/console/capabilities')return json({available:false,state});
 if(path==='/ui-preferences')return json({language:'en'});
 if(path==='/v1/profiles')return json({instance:'wmks-fixture',current:'lab',profiles:[{name:'lab',url:'https://lab.example',username:'root'}]});
 if(path==='/v1/host')return json({name:'SDK-free host',connected:true});
 if(path==='/v1/vms')return json([{id:'1',name:'SDK-free VM',powerState:'poweredOn',numCPU:2,memoryMB:4096}]);
 if(path==='/v1/datastores')return json([{name:'Fixture datastore',type:'VMFS',capacity:1024**4,freeSpace:500*1024**3,accessible:true}]);
 if(path==='/v1/datastores/entries')return json([]);
 if(path==='/v1/tasks'||path.startsWith('/v1/settings/'))return json({items:[],hasMore:false,active:0});
 if(path==='/v1/vms/1/console'){tickets++;return json({ws:'/ws/console?id=abcdef'});}
 if(path.startsWith('/v1/'))return json({series:[]});
 return route.fulfill({path:'web'+(path==='/'?'/index.html':path)});
 });
 const check=(v,m)=>{if(!v)throw new Error(m);};
 await page.setViewportSize({width:1440,height:900});await page.goto(origin+'/#overview');await page.evaluate(()=>{localStorage.setItem('govmc-language','en');sessionStorage.removeItem('govmc-web-profile');});await page.reload();
 await page.getByText('SDK-free host',{exact:true}).first().waitFor();check(await page.locator('[data-page="console"]').isVisible(),'console navigation hidden');
 await page.locator('.vm-console-action').first().click();await page.getByRole('heading',{name:'Enable remote console'}).waitFor();
 await page.locator('#theme [data-theme-option="light"]').click();await page.screenshot({path:'/tmp/govmc-wmks-guide-light.png',fullPage:false});
 check(tickets===0,'missing SDK requested ticket');check(await page.locator('#session-stage iframe').count()===0,'missing SDK created session');
 await page.getByText(/restart serve and refresh/).waitFor();check(await page.getByText('govmc wmks install --file /path/to/SDK.zip',{exact:true}).isVisible(),'install instruction missing');
 state='invalid';await page.getByRole('button',{name:'Check again',exact:true}).click();await page.getByRole('heading',{name:'Repair the console component'}).waitFor();check(await page.getByText('govmc wmks verify',{exact:true}).isVisible(),'repair instruction missing');
 await page.locator('#language summary').click();await page.locator('#language [data-language="zh"]').click();await page.getByRole('heading',{name:'修复远程控制台组件'}).waitFor();
 await page.locator('#theme [data-theme-option="dark"]').click();await page.setViewportSize({width:390,height:844});await page.screenshot({path:'/tmp/govmc-wmks-guide-mobile.png',fullPage:false});check(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'guide overflows mobile');
 await page.goto(origin+'/datastores.html');await page.getByText('Fixture datastore',{exact:true}).first().waitFor();await page.waitForFunction(()=>document.title==='数据存储 - GoVMC WebUI');
 await page.goto(origin+'/settings.html');await page.getByRole('heading',{name:'主题',exact:true}).waitFor();await page.waitForFunction(()=>document.title==='设置 - GoVMC WebUI');
 check(!requests.some(p=>p.startsWith('/wmks/')),'ordinary pages fetched SDK');check(errors.length===0,errors.join(';'));
 return {ok:true,sdkFreeCore:true,consoleEntryRetained:true,noTicketWhenMissing:true,repairGuide:true,i18n:true,mobile:true};
}
