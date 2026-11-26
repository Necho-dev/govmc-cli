async page=>{
 if(await page.evaluate(()=>window.consoleNativeCheck)==='original')throw new Error('accepted refresh did not reload');
 await page.locator('#connected-sessions [data-vm="1"]').waitFor();
 if(await page.title()!=='Console - GoVMC WebUI')throw new Error('restored page title mismatch');
 await page.locator('#language summary').click();await page.locator('#language [data-language="zh"]').click();
 await page.waitForFunction(()=>document.title==='控制台 - GoVMC WebUI');
 await page.screenshot({path:'/tmp/govmc-console-restored.png',fullPage:false});
 return {ok:true,nativeRefreshAccepted:true,sessionReconnected:true};
}
