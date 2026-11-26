"""Native/wheel parity, including local WMKS management, outside the checkout."""
import base64
import csv
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
import venv
import zipfile

ROOT=Path(__file__).resolve().parents[2]
class DistributionTest(unittest.TestCase):
 def test_native_wheel_and_sdk_parity(self):
  value=os.environ.get('GOVMC_PACKAGED_BINARY')
  if not value:self.skipTest('set GOVMC_PACKAGED_BINARY to a native release executable')
  native=Path(value).resolve();
  archives=list(native.parent.glob('govmc-*.zip'))
  if archives:
   package=archives[0];expected=package.with_suffix('.zip.sha256').read_text().split()[0];self.assertEqual(hashlib.sha256(package.read_bytes()).hexdigest(),expected)
   with zipfile.ZipFile(package) as bundle:
    self.assertEqual(bundle.read(native.name),native.read_bytes())
    self.assertEqual(bundle.read("LICENSE"),(ROOT/"LICENSE").read_bytes())
  sdk=ROOT/'web/wmks/wmks.min.js'
  if sdk.is_file() and sdk.stat().st_size>256:self.assertNotIn(sdk.read_bytes()[:256],native.read_bytes())
  version=subprocess.check_output([str(native),'--version'],text=True,encoding="utf-8").strip().split()[-1]
  with tempfile.TemporaryDirectory(prefix='govmc-cli-distribution-') as d:
   work=Path(d);wheels=work/'wheels'
   published=os.environ.get('GOVMC_PACKAGED_WHEEL')
   if published:
    wheel=Path(published).resolve()
   else:
    subprocess.run([sys.executable,str(ROOT/'build/wheel.py'),'--version',version,'--binary',str(native),'--out',str(wheels)],check=True,cwd=work)
    wheel=next(wheels.glob('*.whl'))
   with zipfile.ZipFile(wheel) as z:
    self.assertEqual(z.read('govmc_cli/'+native.name),native.read_bytes())
    metadata=z.read(next(n for n in z.namelist() if n.endswith('/METADATA'))).decode();self.assertIn('Name: govmc-cli\n',metadata);self.assertNotIn('Requires-Dist: govmc-cli-web',metadata);self.assertIn('govmc = govmc_cli.launcher:main',z.read(next(n for n in z.namelist() if n.endswith('/entry_points.txt'))).decode())
    self.assertIn('License: MIT\n',metadata)
    self.assertEqual(z.read(next(n for n in z.namelist() if n.endswith('.dist-info/LICENSE'))),(ROOT/'LICENSE').read_bytes())
    self.assertFalse(any(n.startswith('govmc_web/') for n in z.namelist()))
    for name,digest,size in csv.reader(io.StringIO(z.read(next(n for n in z.namelist() if n.endswith('/RECORD'))).decode())):
     if not digest:continue
     body=z.read(name);self.assertEqual(len(body),int(size));self.assertEqual(digest,'sha256='+base64.urlsafe_b64encode(hashlib.sha256(body).digest()).decode().rstrip('='))
   environment=work/'venv';venv.EnvBuilder(with_pip=True).create(environment);bindir=environment/('Scripts' if os.name=='nt' else 'bin')
   subprocess.run([str(bindir/('python.exe' if os.name=='nt' else 'python')),'-m','pip','install','--no-index','--no-deps','--no-cache-dir',str(wheel)],check=True,cwd=work,stdout=subprocess.DEVNULL)
   launcher=bindir/('govmc.exe' if os.name=='nt' else 'govmc');home=work/'home';home.mkdir();env={**os.environ,'GOVMC_CONFIG':'','GOVMC_WEB_DIR':'','HOME':str(home),'USERPROFILE':str(home)}
   def run(binary,*args,input=None):return subprocess.run([str(binary),'--config',str(work/'state/config.yaml'),*args],input=input,text=True,encoding="utf-8",capture_output=True,check=True,cwd=work,env=env).stdout
   self.assertEqual(run(native,'--version'),run(launcher,'--version'))
   self.assertEqual(json.loads(run(native,'wmks','status','--json'))['state'],'missing')
   package=work/'SDK.zip'
   official=os.environ.get('GOVMC_OFFICIAL_SDK_ZIP')
   if official:
    package.write_bytes(Path(official).read_bytes())
   else:
    with zipfile.ZipFile(package,'w') as z:
     for name,body in {'wmks.min.js':b'/* WMKS */ window.wmks={};','css/wmks-all.css':b'body{}','img/touch_sprite.png':b'png','img/touch_sprite_feedback.png':b'png','LICENSE.txt':b'fixture notice'}.items():z.writestr(name,body)
   run(native,'wmks','install','--file',str(package),'--yes','--json')
   self.assertEqual(json.loads(run(native,'wmks','status','--json')),json.loads(run(launcher,'wmks','status','--json')))
   run(launcher,'wmks','verify')
   run(native,'profiles','add','fixture','--url','https://127.0.0.1:1/sdk','--username','fixture','--password-stdin',input='temporary-fixture\n')
   for binary in [native,launcher]:
    run(binary,'skill','install','--client','codex','--force')
    skill=home/'.agents/skills/govmc'
    for resource in ('agents/openai.yaml','references/setup.md','references/tasks.md','references/datastores.md','references/clone_workflow.md','references/remote_control.md','references/web_console.md'):
     self.assertTrue((skill/resource).is_file(),resource)
    entry=json.loads(run(binary,'skill','show','--json'));self.assertEqual(entry['name'],'govmc');self.assertEqual((skill/'SKILL.md').read_text(encoding='utf-8'),entry['content'])
    self.assertIn('protocolVersion',run(binary,'mcp','serve',input='{"jsonrpc":"2.0","id":1,"method":"initialize"}\n'))
   if os.environ.get('GOVMC_PACKAGE_SERVE_TEST')=='1':
    for binary in [native,launcher]:
     print('Full serve lifecycle:',binary,flush=True)
     subprocess.run(['go','test','-race','-count=1','-timeout','120s','-run','^TestBuiltServeWMKSFullLifecycle$','-v','./internal/command'],cwd=ROOT,env={**env,'GOVMC_SERVE_BINARY':str(binary),'GOMODCACHE':subprocess.check_output(['go','env','GOMODCACHE'],cwd=ROOT,text=True,encoding="utf-8").strip()},check=True)
   run(launcher,'wmks','uninstall','--yes');self.assertEqual(json.loads(run(native,'wmks','status','--json'))['state'],'missing')
if __name__=='__main__':unittest.main()
