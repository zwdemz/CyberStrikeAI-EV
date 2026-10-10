"""Loopback-only integration test for the HTTP recipe; requires httpx[socks] and PyYAML."""
import socketserver,socket,threading,subprocess,os,yaml,sys
from pathlib import Path
root=Path(__file__).resolve().parents[2]
recipe=yaml.safe_load((root/'tools/http-framework-test.yaml').read_text());seen=[]
class Proxy(socketserver.BaseRequestHandler):
 def handle(self):
  c=self.request;c.settimeout(5);prefix=c.recv(2)
  if prefix[0]==5:
   c.recv(prefix[1]);c.sendall(b'\x05\x00');h=c.recv(4);assert h[:3]==b'\x05\x01\x00'
   if h[3]==3:host=c.recv(c.recv(1)[0]).decode()
   elif h[3]==1:host=socket.inet_ntoa(c.recv(4))
   else:raise AssertionError('unexpected address type')
   c.recv(2);seen.append(('socks',host));c.sendall(b'\x05\x00\x00\x01\x7f\x00\x00\x01\x00\x50');data=b''
  else:data=prefix
  while b'\r\n\r\n' not in data:data+=c.recv(8192)
  seen.append(('request',data.split(b'\r\n')[0].decode()))
  c.sendall(b'HTTP/1.1 200 OK\r\nContent-Length: 20\r\nConnection: close\r\n\r\nPROXY_FIXTURE_OK_123')
server=socketserver.ThreadingTCPServer(('127.0.0.1',0),Proxy);threading.Thread(target=server.serve_forever,daemon=True).start()
python=sys.executable
for scheme in ['http','socks5']:
 env={**os.environ,'CYBERSTRIKE_TEST_PROXY':f'{scheme}://127.0.0.1:{server.server_address[1]}','NO_PROXY':'*'}
 r=subprocess.run([python]+recipe['args']+['--url','http://proxy-fixture.invalid/check','--proxy','http://127.0.0.1:1','--timeout','3'],env=env,capture_output=True,text=True,timeout=10)
 assert r.returncode==0 and 'PROXY_FIXTURE_OK_123' in r.stdout,(scheme,r.returncode,r.stdout[-400:],r.stderr[-400:])
 print(scheme,'forced proxy overrides direct bypass and explicit proxy passed')
assert ('socks','proxy-fixture.invalid') in seen
# A closed proxy must not fall back to the target (the target is the fixture itself).
count=len(seen);env={**os.environ,'CYBERSTRIKE_TEST_PROXY':'http://127.0.0.1:1','NO_PROXY':'*'}
r=subprocess.run([python]+recipe['args']+['--url',f'http://127.0.0.1:{server.server_address[1]}/','--timeout','2'],env=env,capture_output=True,text=True,timeout=10)
assert r.returncode==76 and len(seen)==count,(r.returncode,r.stdout[-400:])
server.shutdown();server.server_close();print('proxy failure exit code and no direct request passed; SOCKS remote DNS passed')
