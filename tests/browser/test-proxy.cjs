const {chromium}=require('playwright');
const fs=require('fs');
const path=require('path');
const root=path.resolve(__dirname,'../..');
const output=process.env.TEST_PROXY_SCREENSHOTS;
if(output)fs.mkdirSync(output,{recursive:true});
(async()=>{
 const browser=await chromium.launch({headless:true,args:['--no-sandbox']});
 const page=await browser.newPage({viewport:{width:1280,height:900}});const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.setContent('<html lang="zh-CN"><head></head><body><div id="page-settings" style="display:block;padding:24px"><h2>设置</h2><div class="settings-layout"><aside class="settings-sidebar"><nav class="settings-nav"><div class="settings-nav-item">基本设置</div></nav></aside><main class="settings-main"><div id="settings-section-basic" class="settings-section-content"></div></main></div></div></body></html>');
 await page.addStyleTag({content:fs.readFileSync(root+'/web/static/css/style.css','utf8')+'body{overflow:auto} .settings-main{flex:1;min-width:0} .settings-layout{display:flex;gap:24px} .settings-sidebar{width:200px;flex-shrink:0}'});
 await page.addScriptTag({content:`window.escapeHtml=s=>String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));window.switchSettingsSection=()=>{document.getElementById('settings-section-test-proxy').classList.add('active');initSettingsCustomSelects();};window.rbacAfterDynamicRender=()=>{};let stored=[],bound='';window.probeURLs=[];window.probeMode='pass';window.probeCalls=0;window.apiFetch=async(path,opt={})=>{let data={};const req=opt.body?JSON.parse(opt.body):{};if(path.startsWith('/api/projects'))data={projects:[{id:'project-fixture',name:'授权测试项目'}]};else if(path.endsWith('/import')){data={id:'pool-fixture',name:req.name,nodes:[{id:'fixture-node',address:'socks5://proxy.example.invalid:1080',region:'Lab',enabled:true}]};if(!req.preview)stored=[data];}else if(path.endsWith('/preference')){if(opt.method==='PUT')bound=req.pool_id;data={pool_id:bound};}else if(path.endsWith('/probe')){probeCalls++;if(probeMode==='failed')return {ok:false,json:async()=>({error:'proxy authentication failed (HTTP 407)'})};data={usable:probeMode==='pass',reachable:true,http_status:probeMode==='pass'?204:429,latency_ms:42};}else data={pools:stored,health:{nodes:{}},probe_urls:probeURLs};return {ok:true,json:async()=>data};};`});
 const settings=fs.readFileSync(root+'/web/static/js/settings.js','utf8');
 await page.addScriptTag({content:settings.slice(0,settings.indexOf('function getRobotStatus'))});
 await page.addScriptTag({content:fs.readFileSync(root+'/web/static/js/test-proxy.js','utf8')});
 await page.locator('[data-section="test-proxy"]').click();
 await page.locator('#test-proxy-name').fill('授权测试出口');await page.locator('#test-proxy-text').fill('socks5://proxy.example.invalid:1080');
 await page.locator('#test-proxy-preview').click();await page.locator('#test-proxy-save').click();
 if(await page.locator('#test-proxy-project').count())throw Error('project binding still shown');
 await page.waitForFunction(()=>!document.getElementById('test-proxy-bind').disabled);
 await page.locator('#test-proxy-pool').selectOption('pool-fixture',{force:true});await page.locator('#test-proxy-bind').click();await page.locator('#test-proxy-load').click();
 if(await page.locator('#test-proxy-pool').inputValue()!=='pool-fixture')throw Error('binding not persisted');

 if(await page.locator('[data-section="test-proxy"]').innerText()!=='代理池')throw Error('wrong title');
 if(!(await page.locator('[data-probe-node]').isDisabled()))throw Error('probe enabled without endpoint');
 await page.evaluate(()=>{probeURLs=['https://controlled.example.invalid/health'];});
 await page.locator('#test-proxy-refresh').click();await page.locator('[data-probe-node]').click();
 await page.waitForFunction(()=>document.querySelector('.proxy-badge.pass'));
 if(await page.locator('#test-proxy-verified').innerText()!=='1')throw Error('missing verified total');
 await page.evaluate(()=>probeMode='response');await page.locator('[data-probe-node]').click();
 await page.waitForFunction(()=>document.querySelector('.proxy-badge.response'));
 if(await page.locator('#test-proxy-verified').innerText()!=='0')throw Error('429 counted as verified');
 await page.evaluate(()=>probeMode='failed');await page.locator('[data-probe-node]').click();
 await page.waitForFunction(()=>document.querySelector('.proxy-badge.failed'));
 if(!(await page.locator('#test-proxy-nodes').innerText()).includes('407'))throw Error('missing failure reason');
 await page.evaluate(()=>probeMode='pass');await page.locator('[data-probe-node]').click();await page.waitForFunction(()=>document.querySelector('.proxy-badge.pass'));
 if(await page.evaluate(()=>probeCalls)!==4)throw Error('unexpected retries');
 // Batch fixtures exercise actual UI scheduling and AbortSignals without public traffic.
 await page.evaluate(()=>{
  stored=['first','second','third'].map(id=>({id,name:id,nodes:[0,1,2].map(n=>({id:String(n),address:'socks5://proxy.example.invalid:1080',enabled:n!==2}))}));
  const original=window.apiFetch;
  window.batchCalls=[];window.batchActive=0;window.batchPeak=0;window.batchDelay=40;window.batchPoolActive={};window.batchPoolPeak=0;
  window.apiFetch=async(path,opt={})=>{
   if(!path.endsWith('/probe'))return original(path,opt);
   const req=JSON.parse(opt.body);batchCalls.push(req);batchActive++;batchPeak=Math.max(batchPeak,batchActive);
   batchPoolActive[req.pool_id]=(batchPoolActive[req.pool_id]||0)+1;batchPoolPeak=Math.max(batchPoolPeak,batchPoolActive[req.pool_id]);
   try{
    await new Promise((resolve,reject)=>{const timer=setTimeout(resolve,batchDelay);opt.signal.addEventListener('abort',()=>{clearTimeout(timer);reject(new DOMException('Aborted','AbortError'));},{once:true});});
    return {ok:req.node_id!=='0',json:async()=>req.node_id==='0'?{error:'fixture timeout'}:{usable:true,http_status:204,latency_ms:40}};
   }finally{batchActive--;batchPoolActive[req.pool_id]--;}
  };
 });
 await page.locator('#test-proxy-refresh').click();await page.waitForFunction(()=>document.getElementById('test-proxy-total').textContent==='9');
 await page.locator('#test-proxy-batch').click();
 await page.waitForFunction(()=>document.getElementById('test-proxy-batch-progress').textContent.includes('批量检测完成'));
 const batchResult=await page.evaluate(()=>({calls:batchCalls.length,peak:batchPeak,poolPeak:batchPoolPeak,unique:new Set(batchCalls.map(r=>r.pool_id+':'+r.node_id)).size,disabled:batchCalls.some(r=>r.node_id==='2')}));
 if(batchResult.calls!==6||batchResult.unique!==6||batchResult.peak!==2||batchResult.poolPeak!==1||batchResult.disabled)throw Error('bad batch scheduling: '+JSON.stringify(batchResult));
 if(await page.locator('.proxy-badge.pass').count()!==3||await page.locator('.proxy-badge.failed').count()!==3)throw Error('batch results lost');
 await page.evaluate(()=>{batchCalls=[];batchDelay=10000;});
 await page.locator('#test-proxy-batch').click();await page.waitForFunction(()=>batchActive===2);
 if(!(await page.locator('#test-proxy-batch').isDisabled()))throw Error('duplicate batch allowed');
 await page.locator('#test-proxy-stop-batch').click();
 await page.waitForFunction(()=>batchActive===0&&document.getElementById('test-proxy-batch-progress').textContent.includes('批量检测已停止'));
 if(await page.evaluate(()=>batchCalls.length)!==2)throw Error('queued probes started after cancellation');
 if(await page.locator('.proxy-badge.canceled').count()!==6)throw Error('canceled results missing');
 await page.evaluate(()=>{batchCalls=[];probeURLs.push('https://other.example.invalid/health');});
 await page.locator('#test-proxy-refresh').click();await page.locator('#test-proxy-batch').click();await page.waitForFunction(()=>batchActive===2);
 await page.locator('#test-proxy-probe-url').selectOption('https://other.example.invalid/health',{force:true});
 await page.waitForFunction(()=>batchActive===0&&!document.getElementById('test-proxy-batch').disabled);
 if(await page.evaluate(()=>batchCalls.length)!==2||await page.locator('.proxy-badge.canceled').count())throw Error('endpoint change retained stale work');
 await page.evaluate(()=>{batchDelay=20;batchCalls=[];});await page.locator('#test-proxy-batch').click();
 await page.waitForFunction(()=>document.getElementById('test-proxy-batch-progress').textContent.includes('批量检测完成'));
 await page.locator('#test-proxy-import-section').evaluate(e=>e.open=false);
 await page.evaluate(()=>{
  const original=window.apiFetch;let failures=0;
  window.apiFetch=async(path,opt={})=>{
   if(!path.endsWith('/probe'))return original(path,opt);
   const req=JSON.parse(opt.body);failures++;
   return {ok:false,json:async()=>({error:'fixture connection failure',probe:{probe_failures:failures,removed:failures===3,node_id:req.node_id}})};
  };
 });
 for(let attempt=1;attempt<=3;attempt++){
  await page.locator('[data-probe-pool="first"][data-probe-node="0"]').click();
  if(attempt<3)await page.waitForFunction(count=>document.querySelector('[data-probe-pool="first"][data-probe-node="0"]').closest('tr').textContent.includes(count+'/3'),attempt);
  else await page.waitForFunction(()=>!document.querySelector('[data-probe-pool="first"][data-probe-node="0"]'));
 }
 if(await page.locator('#test-proxy-total').innerText()!=='8'||!(await page.locator('#test-proxy-cleanup-status').innerText()).includes('已清理'))throw Error('cleanup UI missing');
 for(const theme of ['light','dark']){await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);if(output)await page.screenshot({path:path.join(output,'proxy-'+theme+'.png'),fullPage:true});}
 await page.setViewportSize({width:1280,height:720});if(await page.evaluate(()=>document.documentElement.scrollWidth>1280))throw Error('horizontal overflow');
 if(errors.length)throw Error(errors.join('\n'));console.log('PASS: themed UI, import, preferences, bounded batch, partial failure, cancellation, endpoint changes, 1280px overflow');await browser.close();
})().catch(e=>{console.error(e);process.exit(1)});
