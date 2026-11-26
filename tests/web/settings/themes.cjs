async (page) => {
  await page.unrouteAll({behavior:"wait"});
  let sdkAvailable=false; const sdkPrefix="/wmks/"+"a".repeat(64)+"/";
  const origin="http://127.0.0.1:18765", errors=[];
  page.on("pageerror",error=>errors.push(error.message));
  await page.route(origin+"/**",async route=>{
    let path=new URL(route.request().url()).pathname;
    if(path==="/v1/console/capabilities")return route.fulfill({json:{available:sdkAvailable,state:sdkAvailable?"ready":"missing",baseURL:sdkPrefix}});
    if(path.startsWith(sdkPrefix))path="/wmks/"+path.slice(sdkPrefix.length);
    if(path.startsWith("/wmks/"))return route.fulfill({path:"web"+path}).catch(()=>route.fulfill({status:404,body:"SDK fixture absent"}));
 if(path==="/v1/tasks")return route.fulfill({json:{items:[],hasMore:false,active:0}});
    let data;
    if(path==="/ui-preferences")data={language:"zh"};
    else if(path==="/v1/profiles")data={instance:"theme-fixture",current:"lab",profiles:[{name:"lab",username:"root",url:"https://lab.example"}]};
    else if(path.startsWith("/v1/settings/"))data={items:[],hasMore:false};
    else if(path==="/v1/host")data={name:"Fixture host",connected:true};
    else if(path.includes("metrics"))data={series:[]};
    else if(path.startsWith("/v1/"))data=[];
    if(data!==undefined)return route.fulfill({contentType:"application/json",body:JSON.stringify(data)});
    return route.fulfill({path:"web"+(path==="/"?"/index.html":path)});
  });
  const check=(condition,message)=>{if(!condition)throw new Error(message);};
  await page.setViewportSize({width:1440,height:980});
  await page.goto(origin+"/settings.html");
  sdkAvailable=await page.evaluate(()=>fetch("/wmks/wmks.min.js").then(r=>r.ok));
  await page.evaluate(()=>{localStorage.setItem("govmc-language","zh");localStorage.setItem("govmc-theme","system");sessionStorage.clear();});
  await page.reload();
  await page.locator('[data-preference-theme="classic-dark"]').waitFor();
  check(await page.locator("[data-preference-theme]").count()===5,"missing theme cards");
  check(await page.locator("#theme button").count()===3,"toolbar theme controls changed");
  const background=()=>page.evaluate(()=>getComputedStyle(document.body).backgroundColor);
  const colors={};
  for(const [theme,label] of [["magic-blue","魔法蓝"],["classic-dark","经典深色"]]){
    await page.locator('[data-preference-theme="'+theme+'"]').click();
    check(await page.locator("html").getAttribute("data-theme")===theme,"theme did not apply");
    check(await page.locator('#theme [data-theme-option="dark"]').getAttribute("aria-pressed")==="true","dark family indicator missing");
    check((await page.locator('#theme [data-theme-option="dark"]').getAttribute("title")).includes(label),"theme tooltip missing");
    colors[theme]=await background();
    await page.reload();await page.locator('[data-preference-theme="'+theme+'"][aria-pressed="true"]').waitFor();
    check((await page.locator('#theme [data-theme-option="dark"]').getAttribute("title")).includes(label),"tooltip lost after reload");
    await page.screenshot({path:"/tmp/govmc-theme-"+theme+".png",fullPage:true});
    await page.locator('.brand').click();
    await page.waitForFunction(theme=>document.documentElement.dataset.theme===theme,theme);
    await page.waitForFunction(()=>document.querySelector(".environment-dot").dataset.state==="connected");
    await page.locator('.settings-link').click();
    await page.locator('[data-preference-theme="'+theme+'"][aria-pressed="true"]').waitFor();
    await page.waitForFunction(()=>document.querySelector(".environment-dot").dataset.state==="connected");
    await page.locator('#theme [data-theme-option="dark"]').click();
    check(await page.locator("html").getAttribute("data-theme")==="dark","moon shortcut did not select standard dark");
  }
  check(colors["magic-blue"]!==colors["classic-dark"],"additional themes have same palette");
  await page.evaluate(()=>sessionStorage.setItem("govmc-web-profile",JSON.stringify({instance:"theme-fixture",name:"lab",connection:"error"})));
  await page.reload();await page.waitForFunction(()=>document.querySelector(".environment-dot").dataset.state==="error");
  check((await page.locator('#environment summary').getAttribute("title")).includes("连接异常"),"error state tooltip missing");
  await page.evaluate(()=>sessionStorage.setItem("govmc-web-profile",JSON.stringify({instance:"theme-fixture",name:"lab",connection:"connected"})));
  await page.reload();await page.waitForFunction(()=>document.querySelector(".environment-dot").dataset.state==="connected");
  for(const width of [2200,1440,800,390]){
    await page.setViewportSize({width,height:980});
    const geometry=await page.evaluate(()=>{
      const rect=selector=>{const r=document.querySelector(selector).getBoundingClientRect();return {x:r.x,width:r.width};};
      return {tabs:rect('.settings-tabs'),themes:rect('.theme-previews'),language:rect('.language-preference'),overflow:document.documentElement.scrollWidth>innerWidth};
    });
    check(Math.abs(geometry.tabs.x-geometry.themes.x)<1&&Math.abs(geometry.tabs.width-geometry.themes.width)<1,"theme card not aligned at "+width);
    check(Math.abs(geometry.language.width-geometry.themes.width)<1,"language card width mismatch");
    check(!geometry.overflow,"page overflow at "+width);
    if(width===390)await page.screenshot({path:"/tmp/govmc-theme-mobile.png",fullPage:true});
  }
  if(!sdkAvailable){check(!errors.length,errors.join("\n"));return {ok:true,themes:5,consoleThemes:"SDK fixture not installed"};}
  await page.goto(origin+"/console.html");
  await page.locator("#wmksContainer canvas").waitFor({state:"attached"});
  for(const theme of ["magic-blue","classic-dark"]){
    await page.evaluate(theme=>window.postMessage({source:"govmc-workbench",type:"appearance",language:"zh",theme},location.origin),theme);
    await page.waitForFunction(theme=>document.documentElement.dataset.theme===theme,theme);
  }
  check(!errors.length,errors.join("\n"));
  return {ok:true,themes:5,toolbarButtons:3,widths:[2200,1440,800,390],profileStates:["connected","error"],consoleThemes:"passed"};
}
