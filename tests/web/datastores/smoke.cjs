async (page) => {
  await page.unrouteAll({ behavior: "wait" });
  const origin = "http://127.0.0.1:18765",
    calls = [],
    errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  const entry = (path, folder = false) => ({
    path,
    folder,
    size: folder ? 0 : 123,
    modified: "2026-11-25T12:34:56Z",
  });
  const root = [
    entry("configs", true),
    entry("backups", true),
    entry("readme.txt"),
    entry("disk.vmdk"),
    ...Array.from({ length: 53 }, (_, i) =>
      entry("log-" + String(i).padStart(2, "0") + ".log"),
    ),
  ];
  let legacy=false,legacyCross=false;
  const files = { "": root, configs: [entry("server.vmx")], backups: [] };
  const files2={"":[],backups:[]};
  await page.route(origin + "/**", async (route) => {
    const request = route.request(),
      url = new URL(request.url()),
      path = url.pathname,
      profile = request.headers()["x-govmc-profile"] || "";
    if (path.startsWith("/v1/"))
      calls.push({
        path,
        method: request.method(),
        profile,
        body: request.postData(),
        query: Object.fromEntries(url.searchParams),
      });
    if(path==="/v1/console/capabilities")return route.fulfill({json:{available:false,state:"missing"}});
 if(path==="/v1/tasks")return route.fulfill({json:{items:[],hasMore:false,active:0}});
    const json = (data) =>
      route.fulfill({
        contentType: "application/json",
        body: JSON.stringify(data),
      });
    if (path === "/ui-preferences") return json({ language: "zh" });
    if (path === "/v1/profiles")
      return json({
        instance: "files-test",
        current: "lab",
        profiles: [
          { name: "lab", username: "root", url: "https://lab.example" },
          { name: "prod", username: "root", url: "https://prod.example" },
        ],
      });
    if (path === "/v1/profiles/connect") return json({ name: "prod" });
    if (path === "/v1/datastores")
      return json([
        {
          name: "datastore1",
          type: "VMFS",
          capacity: 1024 ** 4,
          freeSpace: 500 * 1024 ** 3,
          accessible: true,
          vmCount: 33,
        },
        {
          name: "datastore2",
          type: "VMFS",
          capacity: 2 * 1024 ** 4,
          freeSpace: 800 * 1024 ** 3,
          accessible: true,
          vmCount: 12,
        },
      ]);
    if(path==="/v1/datastores/entries"&&legacy)return route.fulfill({status:404,body:"not found"});
    if(request.method()==="GET"&&(path==="/v1/datastores/folder"||path==="/v1/datastores/file"))return json((files[url.searchParams.get("path")]||[]).filter(file=>file.folder===(path.endsWith("/folder"))));
    if (path === "/v1/datastores/entries")
      return json(
        profile === "prod"
          ? [entry("prod-only.txt")]
          : (url.searchParams.get("ds")==="datastore2"?files2:files)[url.searchParams.get("path")] || [],
      );
    if (path === "/v1/datastores/file/preview") {
      if (url.searchParams.get("path").endsWith(".vmdk"))
        return route.fulfill({ status: 415, body: "preview_unsupported" });
      return route.fulfill({
        contentType: "text/plain; charset=utf-8",
        body: '<script>window.previewAttack=true</script>\nguestOS = "ubuntu64Guest"',
      });
    }
    if (
      path === "/v1/datastores/file/copy" ||
      path === "/v1/datastores/file/move"
    ) {
      const body = request.postDataJSON();
      if (!body.dryRun) {
        const name = body.dst.split("/").pop(),
          dir = body.dst.split("/").slice(0, -1).join("/");
        (body.dstDatastore==="datastore2"?files2:files)[dir].push(entry(name));
        if (path.endsWith("/move")) {
          const srcDir = body.src.split("/").slice(0, -1).join("/"),
            srcName = body.src.split("/").pop();
          files[srcDir] = files[srcDir].filter((f) => f.path !== srcName);
        }
      }
      return json({
        dryRun: body.dryRun,
        detail: legacyCross?{}:{dstDatastore:body.dstDatastore||body.ds},
        op: path.endsWith("/move") ? "move" : "copy",
      });
    }
    if(path==="/v1/datastores/file/rename"){
      const body=request.postDataJSON();if(!body.dryRun){const dir=body.path.split('/').slice(0,-1).join('/'),name=body.path.split('/').pop();files[dir].find(file=>file.path===name).path=body.name;}return json({dryRun:body.dryRun});
    }
    if(path==="/v1/datastores/folder"&&request.method()==="POST"){
      const body=request.postDataJSON(),dir=body.path.split('/').slice(0,-1).join('/'),name=body.path.split('/').pop();files[dir].push(entry(name,true));files[body.path]=[];return json({status:"ok"});
    }
    if(path==="/v1/datastores/file"&&request.method()==="PUT"){
      const target=url.searchParams.get('path'),dir=target.split('/').slice(0,-1).join('/');files[dir].push(entry(target.split('/').pop()));return json({status:"ok"});
    }
    if(request.method()==="DELETE"&&(path==="/v1/datastores/folder"||path==="/v1/datastores/file")){
      const body=request.postDataJSON(),dir=body.path.split('/').slice(0,-1).join('/'),name=body.path.split('/').pop();files[dir]=files[dir].filter(file=>file.path!==name);return json({status:"ok"});
    }
    if (path.startsWith("/v1/")) return json({ status: "ok" });
    return route.fulfill({
      path: "web" + (path === "/" ? "/index.html" : path),
    });
  });
  const check = (value, message) => {
    if (!value) throw new Error(message);
  };
  await page.setViewportSize({ width: 1440, height: 980 });
  await page.goto(origin + "/datastores.html");
  await page.evaluate(async () => {
    localStorage.setItem("govmc-language", "zh");
    localStorage.setItem("govmc-theme", "system");
    sessionStorage.clear();
    await window.govmcCache.invalidate('files-test','lab');await window.govmcCache.invalidate('files-test','prod');
  });
  await page.reload();
  await page.locator("#file-body tr").first().waitFor();
  await page.waitForFunction(()=>document.querySelector('#file-table-wrap').getAttribute('aria-busy')==='false');
  await page.screenshot({path:'/tmp/govmc-web-datastores.png',fullPage:true});
  await page.emulateMedia({colorScheme:'dark'});
  await page.screenshot({path:'/tmp/govmc-web-datastores-dark.png',fullPage:true});
  await page.emulateMedia({colorScheme:'light'});
  await page.locator('#file-kind [data-kind="folder"]').click();check(await page.locator('#file-body tr').count()===2,'folder segment filter failed');
  await page.locator('#file-kind [data-kind="folder"]').press('ArrowRight');check(await page.locator('#file-body .file-name[data-kind="folder"]').count()===0,'file segment filter failed');
  await page.locator('#file-kind [data-kind="file"]').press('Home');check(await page.locator('#file-kind svg').count()===3,'segment icons missing');
  check(
    (await page.locator("#file-body tr").count()) === 50,
    "page size incorrect",
  );
  check(
    (await page
      .locator("#file-body tr")
      .first()
      .locator(".file-name")
      .getAttribute("data-kind")) === "folder",
    "folders not first",
  );
  check((await page.locator(".store-button").count()) === 2, "missing stores");
  await page.getByRole("button", { name: "下一页", exact: true }).click();
  check(
    (await page.locator("#file-body tr").count()) === 7,
    "file pagination not applied",
  );
  await page.getByRole("button", { name: "上一页", exact: true }).click();
  await page.locator("#file-search").fill("readme");
  const row = page.locator("#file-body tr").filter({ hasText: "readme.txt" });
  await row.getByRole("button", { name: "在线预览", exact: true }).click();
  await page.locator("#preview-content pre").waitFor();
  check(
    (await page.locator("#preview-content pre").innerText()).includes(
      "<script>",
    ),
    "text preview escaped or missing",
  );
  check(
    await page.evaluate(() => window.previewAttack === undefined),
    "preview executed remote text",
  );
  await page.locator("#preview-close").click();
  async function fileAction(action, destination,targetStore="datastore1") {
    await page
      .locator("#file-body tr")
      .filter({ hasText: "readme.txt" })
      .getByRole("button", { name: "更多操作", exact: true })
      .click();
    await page
      .locator(".file-menu-items:popover-open")
      .getByRole("button", { name: action, exact: true })
      .click();
    await page.getByRole("combobox",{name:"目标数据存储",exact:true}).click();
    await page.getByRole("option",{name:targetStore,exact:true}).click();
    await page.locator("#operation-value").fill(destination);
    if(targetStore==="datastore2"){await page.setViewportSize({width:375,height:844});check(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),"cross-store form overflow");await page.screenshot({path:"/tmp/govmc-cross-datastore-form.png"});await page.setViewportSize({width:1440,height:980});}
    const before = calls.filter(
      (call) => call.method === "POST" && call.path.includes("/file/"),
    ).length;
    await page.getByRole("button", { name: "预览变更", exact: true }).click();
    await page
      .getByRole("button", { name: "确认并执行", exact: true })
      .waitFor();
    check(await page.getByRole("combobox",{name:"目标数据存储",exact:true}).isDisabled(),"target datastore was not frozen after preview");
    check((await page.locator("#operation-plan").innerText()).includes("["+targetStore+"]"),"preview lost destination datastore");
    const preview = calls
      .filter((call) => call.method === "POST" && call.path.includes("/file/"))
      .at(-1);
    check(
      JSON.parse(preview.body).dryRun === true &&
        !JSON.parse(preview.body).confirm && JSON.parse(preview.body).dstDatastore===targetStore,
      "preview submitted write",
    );
    check(
      calls.filter(
        (call) => call.method === "POST" && call.path.includes("/file/"),
      ).length ===
        before + 1,
      "duplicate preview",
    );
    await page.getByRole("button", { name: "确认并执行", exact: true }).click();
    await page.waitForFunction(
      () => !document.querySelector("#file-dialog").open,
    );
    await page.waitForFunction(()=>document.querySelector('#file-table-wrap').getAttribute('aria-busy')==='false');
    const commit = calls
      .filter((call) => call.method === "POST" && call.path.includes("/file/"))
      .at(-1);
    check(
      JSON.parse(commit.body).dryRun === false &&
        JSON.parse(commit.body).confirm === true &&
        commit.profile === "lab" && JSON.parse(commit.body).dstDatastore===targetStore,
      "confirmed action target mismatch",
    );
  }
  await fileAction("复制", "backups/readme-copy.txt");
  legacyCross=true;
  await page.locator("#file-body tr").filter({hasText:"readme.txt"}).getByRole("button",{name:"更多操作",exact:true}).click();
  await page.locator(".file-menu-items:popover-open").getByRole("button",{name:"复制",exact:true}).click();
  await page.getByRole("combobox",{name:"目标数据存储",exact:true}).click();await page.getByRole("option",{name:"datastore2",exact:true}).click();
  await page.locator("#operation-value").fill("legacy-cross.txt");
  await page.getByRole("button",{name:"预览变更",exact:true}).click();
  await page.locator("#operation-error").filter({hasText:"跨数据存储"}).waitFor();
  check(await page.getByRole("button",{name:"预览变更",exact:true}).isVisible(),"legacy backend allowed cross-store commit");
  await page.locator("#operation-cancel").click();legacyCross=false;
  await fileAction("复制","readme.txt","datastore2");
  check(files2[""].some(file=>file.path==="readme.txt"),"cross-datastore copy not executed");
  await fileAction("移动", "configs/readme.txt");
  await page.locator("#file-search").fill("");
  await page.locator(".file-name").filter({ hasText: "configs" }).click();
  await page.locator(".file-name").filter({ hasText: "server.vmx" }).waitFor();
  async function confirmLocalOperation(){
    const writes=calls.filter(call=>["POST","PUT","DELETE"].includes(call.method)).length;
    await page.getByRole('button',{name:'预览变更',exact:true}).click();await page.getByRole('button',{name:'确认并执行',exact:true}).waitFor();
    check(calls.filter(call=>["POST","PUT","DELETE"].includes(call.method)).length===writes,'local preview performed mutation');
    await page.getByRole('button',{name:'确认并执行',exact:true}).click();await page.waitForFunction(()=>!document.querySelector('#file-dialog').open);
  }
  await page.locator('#file-body tr').filter({hasText:'server.vmx'}).getByRole('button',{name:'更多操作',exact:true}).click();
  await page.locator('.file-menu-items:popover-open').getByRole('button',{name:'重命名',exact:true}).click();await page.locator('#operation-value').fill('guest.vmx');
  await page.getByRole('button',{name:'预览变更',exact:true}).click();await page.getByRole('button',{name:'确认并执行',exact:true}).click();await page.locator('.file-name').filter({hasText:'guest.vmx'}).waitFor();
  await page.locator('#mkdir').click();await page.locator('#operation-value').fill('new-folder');await confirmLocalOperation();await page.locator('.file-name').filter({hasText:'new-folder'}).waitFor();
  await page.locator('#upload-input').setInputFiles({name:'uploaded.txt',mimeType:'text/plain',buffer:Buffer.from('fixture')});await confirmLocalOperation();await page.locator('.file-name').filter({hasText:'uploaded.txt'}).waitFor();
  await page.locator('#file-body tr').filter({hasText:'new-folder'}).getByRole('button',{name:'更多操作',exact:true}).click();await page.locator('.file-menu-items:popover-open').getByRole('button',{name:'删除',exact:true}).click();await confirmLocalOperation();
  await page.waitForFunction(()=>![...document.querySelectorAll('.file-name')].some(el=>el.textContent==='new-folder'));
  await page.locator("#file-search").fill("guest");
  await page.locator("#back").click();
  await page.locator(".file-name").filter({ hasText: "configs" }).waitFor();
  await page.locator(".file-name").filter({ hasText: "disk.vmdk" }).click();
  await page
    .getByText("该文件类型暂不支持在线预览，请下载后查看。", { exact: true })
    .waitFor();
  await page.locator("#preview-close").click();
  legacy=true;await page.reload();await page.locator('#file-body tr').first().waitFor();await page.getByText(/当前服务版本较旧/).waitFor();
  check(calls.some(call=>call.path==='/v1/datastores/folder'&&call.method==='GET'),'old server directory fallback missing');
  check((await page.locator('#notice').innerText()).includes('重启'),'compatibility update notice missing');check(await page.locator('#mkdir').isDisabled(),'legacy server write actions enabled');legacy=false;await page.reload();await page.locator('#file-body tr').first().waitFor();
  await page.waitForFunction(()=>document.querySelector('#file-table-wrap').getAttribute('aria-busy')==='false');
  await page.screenshot({
    path: "/tmp/govmc-datastores-desktop.png",
    fullPage: true,
  });
  for (const width of [320, 375, 760, 1440]) {
    await page.setViewportSize({ width, height: 980 });
    check(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
      "page overflow at " + width,
    );
  }
  await page.setViewportSize({ width: 375, height: 844 });
  await page.screenshot({
    path: "/tmp/govmc-datastores-mobile.png",
    fullPage: true,
  });
  await page.locator("#environment summary").click();
  await page
    .locator("#environment .dropdown-options button")
    .filter({ hasText: "prod" })
    .click();
  await page.getByRole("button", { name: "确认切换", exact: true }).click();
  await page
    .locator(".file-name")
    .filter({ hasText: "prod-only.txt" })
    .waitFor();
  check(
    await page
      .locator("#file-body")
      .innerText()
      .then((text) => !text.includes("server.vmx")),
    "old environment files retained",
  );
  check(
    calls.filter((call) => call.path === "/v1/datastores/entries").at(-1)
      .profile === "prod",
    "browse did not pin profile",
  );
  check(!errors.length, errors.join("\n"));
  return {
    ok: true,
    pagination: true,
    preview: true,
    copyMove: true,
    crossDatastore: true,
    profileIsolation: true,
  };
}
