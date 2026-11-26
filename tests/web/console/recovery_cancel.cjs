async page=>{
 if(await page.evaluate(()=>window.consoleNativeCheck)!=='original')throw new Error('cancelled refresh replaced the page');
 await page.locator('#connected-sessions [data-vm="1"]').waitFor();
 return {ok:true,nativeRefreshCancelled:true,sessionPreserved:true};
}
