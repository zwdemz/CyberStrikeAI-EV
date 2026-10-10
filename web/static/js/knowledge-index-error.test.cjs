const fs=require('node:fs'),vm=require('node:vm'),test=require('node:test'),assert=require('node:assert/strict');
const source=fs.readFileSync('web/static/js/knowledge.js','utf8').replace(/\r\n/g, '\n');
const c=vm.createContext({});vm.runInContext(source.slice(0,source.indexOf('\n\n',source.indexOf('return \'请根据'))+2),c);
test('embedding failures give the matching configuration or service advice',()=>{
 for(const [error,expected] of [['status code: 401 Incorrect API key provided','API 密钥'],['403 Forbidden','访问权限'],['429 rate limit','请求频率'],['context deadline exceeded','超时'],['dial: no such host','DNS'],['unknown error','上方错误']]) assert.ok(c.knowledgeIndexErrorAdvice(error).includes(expected));
});

test('actual status renderer shows specific advice even with zero items',async()=>{
 const container={style:{},innerHTML:''};
 const ctx=vm.createContext({apiFetch:async()=>({ok:true,json:async()=>({total_items:0,last_error:'401 Unauthorized test-only failure'})}),document:{getElementById:()=>container},escapeHtml:s=>s,indexProgressInterval:null,clearInterval(){},showNotification(){},console});
 vm.runInContext(source.slice(0,source.indexOf('\n\n',source.indexOf('return \'请根据'))+2),ctx);
 vm.runInContext(source.slice(source.indexOf('async function updateIndexProgress('),source.indexOf('function stopIndexProgressPolling(')),ctx);
 await ctx.updateIndexProgress();
 assert.equal(container.style.display,'block');assert.match(container.innerHTML,/API 密钥/);assert.match(container.innerHTML,/401 Unauthorized/);
});


test('idle incomplete coverage is not reported as a running job', async()=>{
 const container={style:{},innerHTML:''}; let cleared=false;
 const ctx=vm.createContext({apiFetch:async()=>({ok:true,json:async()=>({total_items:14,indexed_items:12,progress_percent:85.7,is_complete:false})}),document:{getElementById:()=>container},escapeHtml:s=>s,indexProgressInterval:123,clearInterval(){cleared=true},setInterval(){throw new Error('idle polling')},showNotification(){},console});
 vm.runInContext(source.slice(source.indexOf('async function updateIndexProgress('),source.indexOf('function stopIndexProgressPolling(')),ctx);
 await ctx.updateIndexProgress();
 assert.match(container.innerHTML,/待补建 2 项/);assert.doesNotMatch(container.innerHTML,/正在构建索引/);assert.equal(cleared,true);
});
