"""Exercise real tab-close dialogs through the CLI's dialog transport."""
import argparse
import shlex
import subprocess

parser = argparse.ArgumentParser()
parser.add_argument('--pwcli', required=True)
parser.add_argument('--session', required=True)
args = parser.parse_args()
command = shlex.split(args.pwcli) + ['-s=' + args.session]


def run(*arguments):
    result = subprocess.run(command + list(arguments), text=True, capture_output=True, timeout=60)
    output = result.stdout + result.stderr
    if result.returncode or '### Error' in output:
        raise RuntimeError(output)
    return output


close = 'async page=>{await page.close({runBeforeUnload:true});return {closeRequested:true};}'
for decision in ['dismiss', 'accept']:
    output = run('run-code', close)
    if '["beforeunload" dialog' not in output:
        raise AssertionError('Closing an active console did not display a native warning: ' + output)
    run('dialog-' + decision)
    if decision == 'dismiss':
        output = run('run-code', '''async page=>{
          if(await page.evaluate(()=>window.consoleNativeCheck)!=='original')throw Error('Close cancellation replaced page');
          await page.locator('#connected-sessions [data-vm="1"]').waitFor();
          await page.frameLocator('#session-stage iframe').locator('#name').click();
          return {ok:true,closeCancelled:true,sessionUsable:true};
        }''')
        if '"closeCancelled":true' not in output:
            raise AssertionError(output)
    else:
        output = run('tab-list')
        if '127.0.0.1:18765' in output:
            raise AssertionError('Accepted tab-close left the workspace open: ' + output)
print('{"ok":true,"nativeTabClosePrompt":true,"cancelPreservesSession":true,"acceptClosesTab":true}')
