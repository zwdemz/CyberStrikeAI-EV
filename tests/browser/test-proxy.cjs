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
 await page.addScriptTag({content:`window.escapeHtml=s=>String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));window.switchSettingsSection=()=>{document.getElementById('settings-section-test-proxy').classList.add('active');initSettingsCustomSelects();};window.rbacAfterDynamicRender=()=>{};let stored=[],bound='';window.apiFetch=async(path,opt={})=>{let data={};const req=opt.body?JSON.parse(opt.body):{};if(path.startsWith('/api/projects'))data={projects:[{id:'project-fixture',name:'授权测试项目'}]};else if(path.endsWith('/import')){data={id:'pool-fixture',name:req.name,nodes:[{id:'fixture-node',address:'socks5://proxy.example.invalid:1080',region:'Lab',enabled:true}]};if(!req.preview)stored=[data];}else if(path.includes('/binding/')){if(opt.method==='PUT')bound=req.pool_id;data={pool_id:bound};}else data={pools:stored,health:{nodes:{}}};return {ok:true,json:async()=>data};};`});
 const settings=fs.readFileSync(root+'/web/static/js/settings.js','utf8');
 await page.addScriptTag({content:settings.slice(0,settings.indexOf('function getRobotStatus'))});
 await page.addScriptTag({content:fs.readFileSync(root+'/web/static/js/test-proxy.js','utf8')});
 await page.locator('[data-section="test-proxy"]').click();
 await page.locator('#test-proxy-name').fill('授权测试出口');await page.locator('#test-proxy-text').fill('socks5://proxy.example.invalid:1080');
 await page.locator('#test-proxy-preview').click();await page.locator('#test-proxy-save').click();
 await page.locator('#test-proxy-project').selectOption('project-fixture',{force:true});
 await page.waitForFunction(()=>!document.getElementById('test-proxy-bind').disabled);
 await page.locator('#test-proxy-pool').selectOption('pool-fixture',{force:true});await page.locator('#test-proxy-bind').click();await page.locator('#test-proxy-load').click();
 if(await page.locator('#test-proxy-pool').inputValue()!=='pool-fixture')throw Error('binding not persisted');
 for(const theme of ['light','dark']){await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);if(output)await page.screenshot({path:path.join(output,'proxy-'+theme+'.png'),fullPage:true});}
 await page.setViewportSize({width:1280,height:720});if(await page.evaluate(()=>document.documentElement.scrollWidth>1280))throw Error('horizontal overflow');
 if(errors.length)throw Error(errors.join('\n'));console.log('PASS: themed UI, preview/import, project selection, binding, custom selects, 1280px overflow');await browser.close();
})().catch(e=>{console.error(e);process.exit(1)});
