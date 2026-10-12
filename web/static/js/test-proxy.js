// Account-scoped proxy management. Validation never changes routing or replays target traffic.
(() => {
    const labels = {
        zh: {title:'代理池',subtitle:'管理出口，明确验证状态',account:'当前账号出口',accountHint:'选择按账号保存，跨会话和重启保留。不会自动切换代理或因目标封锁开启代理。',direct:'不使用代理池',bind:'记住我的选择',load:'读取已保存选择',pool:'代理池',saved:'选择已保存',nodes:'节点与连通性',nodeHint:'每次验证仅向配置的检测地址发送一次 HEAD 请求。结果仅代表本次检测，不保证所有站点可用。',probeURL:'检测地址',noProbe:'尚未配置检测地址，请在 test_proxy.probe_urls 中登记 HTTP/HTTPS 地址并重启。',probe:'验证连通性',refresh:'刷新节点',untested:'未验证',checking:'验证中',pass:'验证通过',response:'收到响应，未通过',failed:'验证失败',disabled:'已停用',cooldown:'冷却中',active:'在途',failures:'连接失败',empty:'尚无代理池，请先导入节点。',importTitle:'导入代理',importHint:'支持 Markdown 表格、CSV、TSV、每行一个 URL。账号和密码两列均为 - 或 -- 时按匿名代理处理。',name:'名称',text:'代理内容',limits:'并发设置',concurrency:'池并发（1–16）',perNode:'节点并发（1–4）',preview:'检查格式',save:'确认导入',previewOK:'格式检查通过，请核对预览后导入。',invalid:'请先检查导入格式',remove:'删除',removeConfirm:'删除此代理池？仍被账号选择、旧项目绑定或在途请求使用时无法删除。',waiting:'处理中…',fail:'操作失败',scope:'当前仅 http-framework-test 支持代理出口；不支持的执行工具会被阻止。模型 API 不受影响。',total:'节点总数',enabled:'已启用',verified:'本页验证通过',resultHint:'验证结果仅保留在当前页面；改变检测地址会清空结果。',region:'地区',status:'状态',result:'本次验证',actions:'操作',address:'代理地址',imported:'导入成功',cancel:'取消验证'},
        en: {title:'Proxy pools',subtitle:'Manage egress and verify connectivity',account:'Current account egress',accountHint:'Saved across conversations and restarts for this account. Target blocks never enable or rotate proxies.',direct:'Do not use a pool',bind:'Remember my choice',load:'Read saved choice',pool:'Proxy pool',saved:'Preference saved',nodes:'Nodes and connectivity',nodeHint:'Each check sends one HEAD request to a configured endpoint. Results apply only to this check, not every website.',probeURL:'Probe endpoint',noProbe:'No endpoint configured. Add a controlled HTTP/HTTPS URL to test_proxy.probe_urls and restart.',probe:'Verify connection',refresh:'Refresh nodes',untested:'Not checked',checking:'Checking',pass:'Verified',response:'Response received; not verified',failed:'Check failed',disabled:'Disabled',cooldown:'Cooling down',active:'Active',failures:'Connection failures',empty:'No pools yet. Import nodes to get started.',importTitle:'Import proxies',importHint:'Markdown, CSV, TSV or one URL per line. Paired - or -- credential placeholders mean anonymous.',name:'Name',text:'Proxy data',limits:'Concurrency',concurrency:'Pool limit (1–16)',perNode:'Node limit (1–4)',preview:'Validate format',save:'Import',previewOK:'Format validated. Review the preview before importing.',invalid:'Validate the import first',remove:'Delete',removeConfirm:'Delete this pool? Account selections, legacy bindings and active requests prevent deletion.',waiting:'Working…',fail:'Operation failed',scope:'Only http-framework-test currently supports proxy routing. Unsupported execution tools are blocked. Model API traffic is unchanged.',total:'Total nodes',enabled:'Enabled',verified:'Verified on this page',resultHint:'Results last only for this page. Changing the endpoint clears results.',region:'Region',status:'Status',result:'Current check',actions:'Actions',address:'Proxy address',imported:'Import complete',cancel:'Cancel check'}
    };
    Object.assign(labels.zh,{batch:'批量检测已启用节点',stopBatch:'停止批量检测',queued:'等待检测',canceled:'已取消',batchDone:'批量检测完成',batchStopped:'批量检测已停止',progress:'已完成',batchHint:'最多同时检测 2 个节点，同一池串行；每个节点仅检测一次，失败后继续。'});
    Object.assign(labels.en,{batch:'Check all enabled nodes',stopBatch:'Stop batch',queued:'Queued',canceled:'Canceled',batchDone:'Batch complete',batchStopped:'Batch stopped',progress:'Completed',batchHint:'Up to 2 checks at once, one per pool. Each node is checked once; failures do not stop the batch.'});
    Object.assign(labels.zh,{cleanupHint:'连续 3 次连接失败自动清理节点，计数重启后保留。收到目标响应会清零；取消和繁忙不计数。',probeFailures:'连续检测失败',removed:'已清理不通节点'});
    Object.assign(labels.en,{cleanupHint:'Three consecutive connection failures remove a node; counts survive restarts. Target responses reset the streak. Cancellation and busy requests do not count.',probeFailures:'Consecutive failed checks',removed:'Unreachable node removed'});
    Object.assign(labels.zh,{capabilities:'工具代理支持状态',capabilityHint:'这是接入能力清单，不代表工具已安装或已启用。出口在同一会话内固定；目标拒绝不会触发自动轮换。',tool:'工具',supported:'已支持',local:'内部操作',unverified:'待验证（已阻止）',unsupported:'不支持（已阻止）',capabilityUnknown:'服务端未提供支持状态，请更新服务后刷新。',reason:'适用范围',supportedReason:'固定会话代理；故障不直连、不自动换节点。',localReason:'内部数据或工作区操作，不需要代理。',unverifiedReason:'工具模式、内部并发及出站流量尚未完成隔离验证。',unsupportedReason:'没有已验证的代理适配器，不能通过通用执行工具绕过。'});
    Object.assign(labels.en,{capabilities:'Tool proxy compatibility',capabilityHint:'Routing capabilities, not installed or enabled tools. Egress is fixed per conversation; target rejections never rotate proxies.',tool:'Tool',supported:'Supported',local:'Internal operation',unverified:'Unverified (blocked)',unsupported:'Unsupported (blocked)',capabilityUnknown:'Server capability metadata unavailable. Update the service and refresh.',reason:'Scope',supportedReason:'Fixed session proxy; no direct fallback or automatic rotation.',localReason:'Internal data or workspace operation; no proxy required.',unverifiedReason:'Tool modes, internal concurrency and outbound traffic need isolated verification.',unsupportedReason:'No verified adapter; generic execution cannot bypass this policy.'});
    /** Render server-owned support policy; absent or unknown states are never promoted to supported. */
    function renderCapabilities(items) {
        const target=el('capabilities');
        if(!Array.isArray(items)||!items.length){target.textContent=t('capabilityUnknown');return;}
        const statuses=['supported','local','unverified','unsupported'];
        target.innerHTML=`<div class="proxy-table-wrap"><table class="proxy-table"><thead><tr><th>${escape(t('tool'))}</th><th>${escape(t('reason'))}</th><th>${escape(t('status'))}</th></tr></thead><tbody>${items.map(item=>{
            const status=statuses.includes(item.status)?item.status:'unsupported';
            return `<tr><td><code>${escape(item.name)}</code><small>${escape(t(status+'Reason'))}</small></td><td>${escape(t(status+'Reason'))}</td><td><span class="proxy-badge ${status==='supported'?'pass':status==='local'?'':'response'}">${escape(t(status))}</span></td></tr>`;
        }).join('')}</tbody></table></div>`;
    }
    let validated = '', pools = [], health = {}, probeURLs = [], batch = null;
    const results = new Map(), pending = new Map();
    const t = key => labels[(document.documentElement.lang || 'zh').startsWith('zh') ? 'zh' : 'en'][key];
    const el = id => document.getElementById('test-proxy-' + id);
    const escape = value => String(value).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
    // request preserves authorization and cancellation; server errors contain no credentials.
    async function request(path, options) {
        const response = await apiFetch('/api/test-proxy-pools' + path, options);
        const data = await response.json();
        if (!response.ok) { const error=new Error(data.error || t('fail'));error.probe=data.probe;throw error; }
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
        el('batch').disabled=!!batch||pending.size>0||!probeURLs.length||!nodes.some(n=>n.enabled);
        el('stop-batch').hidden=!batch;
        el('stop-batch').disabled=!!batch?.canceled;
        el('refresh').disabled=!!batch;
    }
    // Render each result beside its node. Refresh never promotes untested nodes to healthy.
    function renderNodes() {
        updateStats();
        el('nodes').innerHTML=pools.length?pools.map(p=>`<section class="test-proxy-pool"><header><div><h4>${escape(p.name)}</h4><small>${p.nodes.length} ${escape(t('total'))}</small></div><button type="button" class="btn-secondary" data-delete-pool="${escape(p.id)}">${escape(t('remove'))}</button></header><div class="proxy-table-wrap"><table class="proxy-table"><thead><tr>${['address','region','status','result','actions'].map(k=>`<th scope="col">${escape(t(k))}</th>`).join('')}</tr></thead><tbody>${p.nodes.map(n=>{
            const key=p.id+':'+n.id,state=health[key]||{},result=results.get(key),busy=pending.has(key);
            const label=!n.enabled?t('disabled'):state.circuit_open?t('cooldown'):t('enabled');
            return `<tr><td><code>${escape(n.address)}</code></td><td>${escape(n.region||'—')}</td><td><span class="proxy-badge">${escape(label)}</span><small>${escape(t('active'))} ${Number(state.active||0)} · ${escape(t('failures'))} ${Number(state.failures||0)}</small><small>${escape(t('probeFailures'))} ${Number(n.probe_url===el('probe-url').value?n.probe_failures||0:0)}/3</small></td><td aria-live="polite"><span class="proxy-badge ${busy?'checking':result?.state||''}">${escape(busy?t('checking'):result?t(result.state):t('untested'))}</span>${result?`<small>${escape(result.detail)}</small><time>${escape(result.time)}</time>`:''}</td><td><button type="button" class="btn-secondary" data-probe-pool="${escape(p.id)}" data-probe-node="${escape(n.id)}" ${(!n.enabled||!probeURLs.length)?'disabled':''}>${escape(t(busy?'cancel':'probe'))}</button></td></tr>`;
        }).join('')}</tbody></table></div></section>`).join(''):`<div class="proxy-empty">${escape(t('empty'))}</div>`;
        el('nodes').querySelectorAll('[data-delete-pool]').forEach(button=>button.onclick=()=>{if(window.confirm(t('removeConfirm')))run(async()=>{await request('/'+encodeURIComponent(button.dataset.deletePool),{method:'DELETE'});await refresh();show(t('saved'));});});
        el('nodes').querySelectorAll('[data-probe-node]').forEach(button=>{button.disabled=button.disabled||!!batch;button.onclick=()=>probeNode(button.dataset.probePool,button.dataset.probeNode);});
        el('nodes').querySelectorAll('[data-delete-pool]').forEach(button=>button.disabled=!!batch||pending.size>0);
    }
    function setProbeFailures(poolID,nodeID,count) {
        if(count===undefined)return;
        const node=pools.find(pool=>pool.id===poolID)?.nodes.find(node=>node.id===nodeID);
        if(node){node.probe_failures=count;node.probe_url=el('probe-url').value;}
    }
    // One explicit check per node, with cancellation; no retry, redirect or routing change.
    async function probeNode(poolID,nodeID,job=null) {
        if(batch&&batch!==job)return;
        const key=poolID+':'+nodeID;
        if(pending.has(key)){pending.get(key).abort();return;}
        const url=el('probe-url').value;if(!probeURLs.includes(url)){show(t('noProbe'));return;}
        const controller=new AbortController();pending.set(key,controller);results.delete(key);renderNodes();
        try{
            const result=await request('/probe',{method:'POST',signal:controller.signal,headers:{'Content-Type':'application/json'},body:JSON.stringify({pool_id:poolID,node_id:nodeID,url})});
            if(!controller.signal.aborted&&el('probe-url').value===url)results.set(key,{state:result.usable?'pass':'response',detail:`HTTP ${result.http_status} · ${result.latency_ms} ms`,time:new Date().toLocaleTimeString()});
            setProbeFailures(poolID,nodeID,result.probe_failures);
        }catch(error){
            if(error.probe?.removed){for(const pool of pools)if(pool.id===poolID)pool.nodes=pool.nodes.filter(node=>node.id!==nodeID);results.delete(key);el('cleanup-status').textContent=`${t('removed')} · ${new Date().toLocaleTimeString()}`;}
            else {
                setProbeFailures(poolID,nodeID,error.probe?.probe_failures);
                if(el('probe-url').value===url)results.set(key,{state:controller.signal.aborted?'canceled':'failed',detail:controller.signal.aborted?'':error.message||t('fail'),time:new Date().toLocaleTimeString()});
            }
        }
        finally{pending.delete(key);renderNodes();}
    }
    // Stop scheduling immediately and abort in-flight requests. The batch owns its
    // snapshot so refreshes/imports cannot add work or retry a node midway through it.
    function stopBatch() {
        if(batch)batch.canceled=true;
        pending.forEach(controller=>controller.abort());
    }
    // Explicit batch action: two workers across pools, serial checks within each pool.
    // Errors are node results, not batch failures; cancellation never starts queued work.
    async function probeBatch() {
        if(batch||pending.size||!probeURLs.includes(el('probe-url').value))return;
        const groups=pools.map(pool=>({id:pool.id,nodes:pool.nodes.filter(node=>node.enabled)})).filter(pool=>pool.nodes.length);
        if(!groups.length)return;
        const job={canceled:false,total:groups.reduce((count,pool)=>count+pool.nodes.length,0),done:0,url:el('probe-url').value};
        batch=job;
        const progress=()=>{el('batch-progress').textContent=`${t('progress')} ${job.done}/${job.total}`;};
        groups.forEach(pool=>pool.nodes.forEach(node=>results.set(pool.id+':'+node.id,{state:'queued',detail:'',time:''})));
        renderNodes();progress();
        let next=0;
        async function worker() {
            while(!job.canceled&&next<groups.length){
                const pool=groups[next++];
                for(const node of pool.nodes){
                    if(job.canceled)break;
                    await probeNode(pool.id,node.id,job);job.done++;progress();
                }
            }
        }
        try{await Promise.all([worker(),worker()]);}
        finally{
            if(job.url===el('probe-url').value)for(const result of results.values())if(result.state==='queued')result.state='canceled';
            batch=null;renderNodes();show('');el('batch-progress').textContent=`${t(job.canceled?'batchStopped':'batchDone')} · ${t('progress')} ${job.done}/${job.total}`;
        }
    }
    async function refresh() {
        const data=await request('');pools=data.pools||[];health=data.health?.nodes||{};probeURLs=data.probe_urls||[];renderCapabilities(data.tool_capabilities);
        const selected=el('pool').value,url=el('probe-url').value;
        el('pool').innerHTML=`<option value="">${escape(t('direct'))}</option>`+pools.map(p=>`<option value="${escape(p.id)}">${escape(p.name)}</option>`).join('');el('pool').value=selected;
        el('probe-url').innerHTML=probeURLs.map(u=>`<option value="${escape(u)}">${escape(u)}</option>`).join('');
        if(probeURLs.includes(url))el('probe-url').value=url;
        if(url!==el('probe-url').value){stopBatch();results.clear();}
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
        <details class="proxy-section"><summary>${escape(t('capabilities'))}</summary><p class="test-proxy-hint">${escape(t('capabilityHint'))}</p><div id="test-proxy-capabilities" aria-live="polite">${escape(t('capabilityUnknown'))}</div></details>
        <section class="proxy-section"><h4>${escape(t('nodes'))}</h4><p class="test-proxy-hint">${escape(t('nodeHint'))}</p><div class="form-group"><label for="test-proxy-probe-url">${escape(t('probeURL'))}</label><select id="test-proxy-probe-url"></select></div><p class="proxy-warning" id="test-proxy-probe-warning">${escape(t('noProbe'))}</p><p class="test-proxy-hint">${escape(t('resultHint'))}</p><div id="test-proxy-nodes"></div></section>
        <details class="proxy-section" id="test-proxy-import-section" open><summary>${escape(t('importTitle'))}</summary><p class="test-proxy-hint">${escape(t('importHint'))}</p><div class="form-group"><label for="test-proxy-name">${escape(t('name'))}</label><input id="test-proxy-name" maxlength="100" autocomplete="off"></div><div class="form-group"><label for="test-proxy-text">${escape(t('text'))}</label><textarea id="test-proxy-text" rows="4" spellcheck="false" autocomplete="off" placeholder="socks5://proxy.example.invalid:1080"></textarea></div><details class="proxy-advanced"><summary>${escape(t('limits'))}</summary><div class="test-proxy-limits"><div class="form-group"><label for="test-proxy-concurrency">${escape(t('concurrency'))}</label><input type="number" id="test-proxy-concurrency" min="1" max="16" value="4"></div><div class="form-group"><label for="test-proxy-per-node">${escape(t('perNode'))}</label><input type="number" id="test-proxy-per-node" min="1" max="4" value="1"></div></div></details><div class="test-proxy-actions"><button type="button" class="btn-secondary" id="test-proxy-preview">${escape(t('preview'))}</button><button type="button" class="btn-primary" id="test-proxy-save" disabled>${escape(t('save'))}</button></div><pre id="test-proxy-preview-result" hidden></pre></details></div>`;
        parent.appendChild(panel);
        const batchControls=document.createElement('div');batchControls.className='test-proxy-actions';
        batchControls.innerHTML=`<button type="button" class="btn-primary" id="test-proxy-batch" disabled>${escape(t('batch'))}</button><button type="button" class="btn-secondary" id="test-proxy-stop-batch" hidden>${escape(t('stopBatch'))}</button><span id="test-proxy-batch-progress" role="status" aria-live="polite"></span>`;
        el('nodes').before(batchControls);
        const batchHint=document.createElement('p');batchHint.className='test-proxy-hint';batchHint.textContent=t('batchHint');batchControls.before(batchHint);
        const cleanupHint=document.createElement('p');cleanupHint.className='test-proxy-hint';cleanupHint.textContent=t('cleanupHint');batchControls.before(cleanupHint);
        const cleanupStatus=document.createElement('p');cleanupStatus.id='test-proxy-cleanup-status';cleanupStatus.setAttribute('role','status');batchControls.after(cleanupStatus);
        el('batch').onclick=()=>run(probeBatch);
        el('stop-batch').onclick=()=>{stopBatch();renderNodes();};
        ['text','name','concurrency','per-node'].forEach(id=>el(id).addEventListener('input',()=>{validated='';el('save').disabled=true;}));
        el('preview').onclick=()=>run(async()=>{const input=payload();const data=await request('/import',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({...input,preview:true})});if(JSON.stringify(payload())!==JSON.stringify(input))return;validated=JSON.stringify(input);el('save').disabled=false;el('preview-result').hidden=false;el('preview-result').textContent=data.nodes.map(n=>`${n.enabled?t('enabled'):t('disabled')}  ${n.address} ${n.region||''}`).join('\n');show(t('previewOK'));});
        el('save').onclick=()=>run(async()=>{const input=payload();if(validated!==JSON.stringify(input))throw new Error(t('invalid'));el('save').disabled=true;await request('/import',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(input)});el('text').value='';validated='';el('preview-result').hidden=true;await refresh();show(t('imported'));});
        el('refresh').onclick=()=>run(async()=>{await refresh();show('');});
        el('load').onclick=()=>run(async()=>{await loadBinding();show('');});
        el('bind').onclick=()=>run(async()=>{await request('/preference',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({pool_id:el('pool').value})});show(t('saved'));});
        el('probe-url').onchange=()=>{stopBatch();results.clear();renderNodes();};
        window.addEventListener('pagehide',stopBatch);
        if(typeof rbacAfterDynamicRender==='function')rbacAfterDynamicRender(panel.parentElement);
    }
    if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',mount);else mount();
})();
