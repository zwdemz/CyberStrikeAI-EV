// Account-scoped proxy management. Validation never changes routing or replays target traffic.
(() => {
    const labels = {
        zh: {title:'代理池',subtitle:'管理出口，明确验证状态',account:'当前账号出口',accountHint:'选择按账号保存，跨会话和重启保留。不会自动切换代理或因目标封锁开启代理。',direct:'不使用代理池',bind:'记住我的选择',load:'读取已保存选择',pool:'代理池',saved:'选择已保存',nodes:'节点与连通性',nodeHint:'每次验证仅向配置的检测地址发送一次 HEAD 请求。结果仅代表本次检测，不保证所有站点可用。',probeURL:'检测地址',noProbe:'尚未配置检测地址，请在 test_proxy.probe_urls 中登记 HTTP/HTTPS 地址并重启。',probe:'验证连通性',refresh:'刷新节点',untested:'未验证',checking:'验证中',pass:'验证通过',response:'收到响应，未通过',failed:'验证失败',disabled:'已停用',cooldown:'冷却中',active:'在途',failures:'连接失败',empty:'尚无代理池，请先导入节点。',importTitle:'导入代理',importHint:'支持 Markdown 表格、CSV、TSV、每行一个 URL。账号和密码两列均为 - 或 -- 时按匿名代理处理。',name:'名称',text:'代理内容',limits:'并发设置',concurrency:'池并发（1–16）',perNode:'节点并发（1–4）',preview:'检查格式',save:'确认导入',previewOK:'格式检查通过，请核对预览后导入。',invalid:'请先检查导入格式',remove:'删除',removeConfirm:'删除此代理池？仍被账号选择、旧项目绑定或在途请求使用时无法删除。',waiting:'处理中…',fail:'操作失败',scope:'当前仅 http-framework-test 支持代理出口；不支持的执行工具会被阻止。模型 API 不受影响。',total:'节点总数',enabled:'已启用',verified:'本页验证通过',resultHint:'验证结果仅保留在当前页面；改变检测地址会清空结果。',region:'地区',status:'状态',result:'本次验证',actions:'操作',address:'代理地址',imported:'导入成功',cancel:'取消验证'},
        en: {title:'Proxy pools',subtitle:'Manage egress and verify connectivity',account:'Current account egress',accountHint:'Saved across conversations and restarts for this account. Target blocks never enable or rotate proxies.',direct:'Do not use a pool',bind:'Remember my choice',load:'Read saved choice',pool:'Proxy pool',saved:'Preference saved',nodes:'Nodes and connectivity',nodeHint:'Each check sends one HEAD request to a configured endpoint. Results apply only to this check, not every website.',probeURL:'Probe endpoint',noProbe:'No endpoint configured. Add a controlled HTTP/HTTPS URL to test_proxy.probe_urls and restart.',probe:'Verify connection',refresh:'Refresh nodes',untested:'Not checked',checking:'Checking',pass:'Verified',response:'Response received; not verified',failed:'Check failed',disabled:'Disabled',cooldown:'Cooling down',active:'Active',failures:'Connection failures',empty:'No pools yet. Import nodes to get started.',importTitle:'Import proxies',importHint:'Markdown, CSV, TSV or one URL per line. Paired - or -- credential placeholders mean anonymous.',name:'Name',text:'Proxy data',limits:'Concurrency',concurrency:'Pool limit (1–16)',perNode:'Node limit (1–4)',preview:'Validate format',save:'Import',previewOK:'Format validated. Review the preview before importing.',invalid:'Validate the import first',remove:'Delete',removeConfirm:'Delete this pool? Account selections, legacy bindings and active requests prevent deletion.',waiting:'Working…',fail:'Operation failed',scope:'Only http-framework-test currently supports proxy routing. Unsupported execution tools are blocked. Model API traffic is unchanged.',total:'Total nodes',enabled:'Enabled',verified:'Verified on this page',resultHint:'Results last only for this page. Changing the endpoint clears results.',region:'Region',status:'Status',result:'Current check',actions:'Actions',address:'Proxy address',imported:'Import complete',cancel:'Cancel check'}
    };
    let validated = '', pools = [], health = {}, probeURLs = [];
    const results = new Map(), pending = new Map();
    const t = key => labels[(document.documentElement.lang || 'zh').startsWith('zh') ? 'zh' : 'en'][key];
    const el = id => document.getElementById('test-proxy-' + id);
    const escape = value => String(value).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
    // request preserves authorization and cancellation; server errors contain no credentials.
    async function request(path, options) {
        const response = await apiFetch('/api/test-proxy-pools' + path, options);
        const data = await response.json();
        if (!response.ok) throw new Error(data.error || t('fail'));
        return data;
    }
    function payload() { return {name:el('name').value.trim(),text:el('text').value,max_concurrent:Number(el('concurrency').value),per_node:Number(el('per-node').value)}; }
    function show(value) { el('message').textContent=value; }
    async function run(task) { show(t('waiting'));try{await task();}catch(error){show(error.message||t('fail'));} }
    function syncSelects() { if(typeof initSettingsCustomSelects==='function')initSettingsCustomSelects(el('panel')); }
    async function loadBinding() {
        el('bind').disabled=true;
        const data=await request('/preference');el('pool').value=data.pool_id||'';syncSelects();el('bind').disabled=false;
    }
    function updateStats() {
        const nodes=pools.flatMap(p=>p.nodes);
        el('total').textContent=nodes.length;el('enabled').textContent=nodes.filter(n=>n.enabled).length;
        el('verified').textContent=[...results.values()].filter(r=>r.state==='pass').length;
    }
    // Render each result beside its node. Refresh never promotes untested nodes to healthy.
    function renderNodes() {
        updateStats();
        el('nodes').innerHTML=pools.length?pools.map(p=>`<section class="test-proxy-pool"><header><div><h4>${escape(p.name)}</h4><small>${p.nodes.length} ${escape(t('total'))}</small></div><button type="button" class="btn-secondary" data-delete-pool="${escape(p.id)}">${escape(t('remove'))}</button></header><div class="proxy-table-wrap"><table class="proxy-table"><thead><tr>${['address','region','status','result','actions'].map(k=>`<th scope="col">${escape(t(k))}</th>`).join('')}</tr></thead><tbody>${p.nodes.map(n=>{
            const key=p.id+':'+n.id,state=health[key]||{},result=results.get(key),busy=pending.has(key);
            const label=!n.enabled?t('disabled'):state.circuit_open?t('cooldown'):t('enabled');
            return `<tr><td><code>${escape(n.address)}</code></td><td>${escape(n.region||'—')}</td><td><span class="proxy-badge">${escape(label)}</span><small>${escape(t('active'))} ${Number(state.active||0)} · ${escape(t('failures'))} ${Number(state.failures||0)}</small></td><td aria-live="polite"><span class="proxy-badge ${busy?'checking':result?.state||''}">${escape(busy?t('checking'):result?t(result.state):t('untested'))}</span>${result?`<small>${escape(result.detail)}</small><time>${escape(result.time)}</time>`:''}</td><td><button type="button" class="btn-secondary" data-probe-pool="${escape(p.id)}" data-probe-node="${escape(n.id)}" ${(!n.enabled||!probeURLs.length)?'disabled':''}>${escape(t(busy?'cancel':'probe'))}</button></td></tr>`;
        }).join('')}</tbody></table></div></section>`).join(''):`<div class="proxy-empty">${escape(t('empty'))}</div>`;
        el('nodes').querySelectorAll('[data-delete-pool]').forEach(button=>button.onclick=()=>{if(window.confirm(t('removeConfirm')))run(async()=>{await request('/'+encodeURIComponent(button.dataset.deletePool),{method:'DELETE'});await refresh();show(t('saved'));});});
        el('nodes').querySelectorAll('[data-probe-node]').forEach(button=>button.onclick=()=>probeNode(button.dataset.probePool,button.dataset.probeNode));
    }
    // One explicit check per node, with cancellation; no retry, redirect or routing change.
    async function probeNode(poolID,nodeID) {
        const key=poolID+':'+nodeID;
        if(pending.has(key)){pending.get(key).abort();return;}
        const url=el('probe-url').value;if(!probeURLs.includes(url)){show(t('noProbe'));return;}
        const controller=new AbortController();pending.set(key,controller);results.delete(key);renderNodes();
        try{
            const result=await request('/probe',{method:'POST',signal:controller.signal,headers:{'Content-Type':'application/json'},body:JSON.stringify({pool_id:poolID,node_id:nodeID,url})});
            if(!controller.signal.aborted&&el('probe-url').value===url)results.set(key,{state:result.usable?'pass':'response',detail:`HTTP ${result.http_status} · ${result.latency_ms} ms`,time:new Date().toLocaleTimeString()});
        }catch(error){if(!controller.signal.aborted&&el('probe-url').value===url)results.set(key,{state:'failed',detail:error.message||t('fail'),time:new Date().toLocaleTimeString()});}
        finally{pending.delete(key);renderNodes();}
    }
    async function refresh() {
        const data=await request('');pools=data.pools||[];health=data.health?.nodes||{};probeURLs=data.probe_urls||[];
        const selected=el('pool').value,url=el('probe-url').value;
        el('pool').innerHTML=`<option value="">${escape(t('direct'))}</option>`+pools.map(p=>`<option value="${escape(p.id)}">${escape(p.name)}</option>`).join('');el('pool').value=selected;
        el('probe-url').innerHTML=probeURLs.map(u=>`<option value="${escape(u)}">${escape(u)}</option>`).join('');
        if(probeURLs.includes(url))el('probe-url').value=url;
        if(url!==el('probe-url').value){pending.forEach(c=>c.abort());results.clear();}
        el('probe-url').disabled=!probeURLs.length;el('probe-warning').hidden=!!probeURLs.length;
        const valid=new Set(pools.flatMap(p=>p.nodes.map(n=>p.id+':'+n.id)));for(const key of results.keys())if(!valid.has(key))results.delete(key);
        syncSelects();renderNodes();
    }
    function mount() {
        const nav=document.querySelector('.settings-nav'),parent=document.querySelector('#settings-section-basic')?.parentElement;
        if(!nav||!parent||el('panel'))return;
        const button=document.createElement('div');button.className='settings-nav-item';button.dataset.section='test-proxy';button.dataset.requirePermission='config:write';button.textContent=t('title');
        button.onclick=()=>{switchSettingsSection('test-proxy');run(async()=>{await refresh();await loadBinding();show('');});};nav.appendChild(button);
        const panel=document.createElement('div');panel.id='settings-section-test-proxy';panel.className='settings-section-content';
        panel.innerHTML=`<div id="test-proxy-panel"><div class="proxy-page-heading"><div><h3>${escape(t('title'))}</h3><p>${escape(t('subtitle'))}</p></div><button class="btn-secondary" type="button" id="test-proxy-refresh">${escape(t('refresh'))}</button></div><p id="test-proxy-message" role="status" aria-live="polite"></p>
        <div class="proxy-summary">${['total','enabled','verified'].map(k=>`<div><span>${escape(t(k))}</span><strong id="test-proxy-${k}">0</strong></div>`).join('')}</div>
        <section class="proxy-section"><h4>${escape(t('account'))}</h4><p class="test-proxy-hint">${escape(t('accountHint'))}</p><div class="proxy-account-row"><div class="form-group"><label for="test-proxy-pool">${escape(t('pool'))}</label><select id="test-proxy-pool"></select></div><button type="button" class="btn-primary" id="test-proxy-bind" disabled>${escape(t('bind'))}</button><button type="button" class="btn-secondary" id="test-proxy-load">${escape(t('load'))}</button></div><p class="test-proxy-hint">${escape(t('scope'))}</p></section>
        <section class="proxy-section"><h4>${escape(t('nodes'))}</h4><p class="test-proxy-hint">${escape(t('nodeHint'))}</p><div class="form-group"><label for="test-proxy-probe-url">${escape(t('probeURL'))}</label><select id="test-proxy-probe-url"></select></div><p class="proxy-warning" id="test-proxy-probe-warning">${escape(t('noProbe'))}</p><p class="test-proxy-hint">${escape(t('resultHint'))}</p><div id="test-proxy-nodes"></div></section>
        <details class="proxy-section" id="test-proxy-import-section" open><summary>${escape(t('importTitle'))}</summary><p class="test-proxy-hint">${escape(t('importHint'))}</p><div class="form-group"><label for="test-proxy-name">${escape(t('name'))}</label><input id="test-proxy-name" maxlength="100" autocomplete="off"></div><div class="form-group"><label for="test-proxy-text">${escape(t('text'))}</label><textarea id="test-proxy-text" rows="4" spellcheck="false" autocomplete="off" placeholder="socks5://proxy.example.invalid:1080"></textarea></div><details class="proxy-advanced"><summary>${escape(t('limits'))}</summary><div class="test-proxy-limits"><div class="form-group"><label for="test-proxy-concurrency">${escape(t('concurrency'))}</label><input type="number" id="test-proxy-concurrency" min="1" max="16" value="4"></div><div class="form-group"><label for="test-proxy-per-node">${escape(t('perNode'))}</label><input type="number" id="test-proxy-per-node" min="1" max="4" value="1"></div></div></details><div class="test-proxy-actions"><button type="button" class="btn-secondary" id="test-proxy-preview">${escape(t('preview'))}</button><button type="button" class="btn-primary" id="test-proxy-save" disabled>${escape(t('save'))}</button></div><pre id="test-proxy-preview-result" hidden></pre></details></div>`;
        parent.appendChild(panel);
        ['text','name','concurrency','per-node'].forEach(id=>el(id).addEventListener('input',()=>{validated='';el('save').disabled=true;}));
        el('preview').onclick=()=>run(async()=>{const input=payload();const data=await request('/import',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({...input,preview:true})});if(JSON.stringify(payload())!==JSON.stringify(input))return;validated=JSON.stringify(input);el('save').disabled=false;el('preview-result').hidden=false;el('preview-result').textContent=data.nodes.map(n=>`${n.enabled?t('enabled'):t('disabled')}  ${n.address} ${n.region||''}`).join('\n');show(t('previewOK'));});
        el('save').onclick=()=>run(async()=>{const input=payload();if(validated!==JSON.stringify(input))throw new Error(t('invalid'));el('save').disabled=true;await request('/import',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(input)});el('text').value='';validated='';el('preview-result').hidden=true;await refresh();show(t('imported'));});
        el('refresh').onclick=()=>run(async()=>{await refresh();show('');});
        el('load').onclick=()=>run(async()=>{await loadBinding();show('');});
        el('bind').onclick=()=>run(async()=>{await request('/preference',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({pool_id:el('pool').value})});show(t('saved'));});
        el('probe-url').onchange=()=>{pending.forEach(c=>c.abort());results.clear();renderNodes();};
        if(typeof rbacAfterDynamicRender==='function')rbacAfterDynamicRender(panel.parentElement);
    }
    if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',mount);else mount();
})();
