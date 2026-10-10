// Test proxies affect opted-in account tool requests, never the model provider.
(() => {
    const labels = {
        zh: { title: '测试代理池', hint: '仅测试流量使用代理。保存当前账号的代理偏好后，当前只支持 http-framework-test；不支持的执行工具会被阻止。代理失效不会直连或自动重放请求。', name: '代理池名称', import: '粘贴 Markdown 表格、CSV、TSV 或每行一个代理 URL', preview: '检查导入', save: '确认导入', refresh: '刷新状态', project: '选择项目', projectSearch: '按名称搜索项目', pool: '选择代理池', bind: '记住我的选择', direct: '不使用代理池（记住此选择）', status: '节点状态', ready: '未触发冷却（不代表已验证连通）', open: '冷却中', empty: '尚无代理池', fail: '请求失败', waiting: '处理中…', saved: '已保存', previewOK: '格式检查通过；请核对节点后确认导入。', concurrency: '池并发上限（1–16）', perNode: '单节点并发（1–4）', invalid: '请先检查导入格式', bindingHint: '仅影响当前账号，跨会话和重启保留，无需绑定项目。须主动选择并保存；不会因目标封锁自动开启或切换代理。', load: '读取我的选择', remove: '删除未使用的代理池', removeConfirm: '删除这个代理池？被账号选择、旧项目绑定或正在使用的池不能删除。', probe: '测试此节点', probeURL: '连通性测试地址（须在 test_proxy.probe_urls 中登记）' },
        en: { title: 'Test proxy pools', hint: 'Only test traffic uses these proxies. Accounts opting in currently support http-framework-test; unsupported execution tools are blocked. No direct fallback or automatic request replay.', name: 'Pool name', import: 'Paste Markdown, CSV, TSV or one proxy URL per line', preview: 'Validate import', save: 'Confirm import', refresh: 'Refresh status', project: 'Select project', projectSearch: 'Search projects by name', pool: 'Select pool', bind: 'Remember my choice', direct: 'Do not use a pool (remember this choice)', status: 'Node status', ready: 'Not cooling down (connectivity unverified)', open: 'Cooling down', empty: 'No pools yet', fail: 'Request failed', waiting: 'Working…', saved: 'Saved', previewOK: 'Format validated. Review nodes before importing.', concurrency: 'Pool concurrency (1–16)', perNode: 'Per-node concurrency (1–4)', invalid: 'Validate the import first', bindingHint: 'Saved for this account across conversations and restarts. No project is required. Target blocks never enable or rotate proxies.', load: 'Read my preference', remove: 'Delete unused pool', removeConfirm: 'Delete this pool? Pools selected by accounts, legacy bindings or active requests cannot be deleted.', probe: 'Test node', probeURL: 'Probe URL (must be configured in test_proxy.probe_urls)' }
    };
    let validated = '', pools = [];
    const t = key => labels[(document.documentElement.lang || 'zh').startsWith('zh') ? 'zh' : 'en'][key];
    const el = id => document.getElementById('test-proxy-' + id);
    const escape = value => String(value).replace(/[&<>"']/g, ch => ({'&':'&amp;', '<':'&lt;', '>':'&gt;', '"':'&quot;', "'":'&#39;'}[ch]));
    async function request(path, options) {
        const response = await apiFetch('/api/test-proxy-pools' + path, options);
        const data = await response.json();
        if (!response.ok) throw new Error(data.error || t('fail'));
        return data;
    }
    function payload() { return {name: el('name').value.trim(), text: el('text').value, max_concurrent: Number(el('concurrency').value), per_node: Number(el('per-node').value)}; }
    function show(value) { el('message').textContent = value; }
    async function run(task) { show(t('waiting')); try { await task(); } catch (error) { show(error.message || t('fail')); } }
    function syncSelects() { if (typeof initSettingsCustomSelects === 'function') initSettingsCustomSelects(el('panel')); }
    async function loadBinding() {
        el('bind').disabled=true;
        const data=await request('/preference');
        el('pool').value=data.pool_id||'';syncSelects();el('bind').disabled=false;
    }
    async function refresh() {
        const data = await request(''); pools = data.pools || [];
        const selectedPool=el('pool').value;
        el('pool').innerHTML = `<option value="">${escape(t('direct'))}</option>` + pools.map(p => `<option value="${escape(p.id)}">${escape(p.name)}</option>`).join('');
        el('pool').value=selectedPool;syncSelects();
        el('nodes').innerHTML = pools.length ? pools.map(p => `<section class="test-proxy-pool"><h4>${escape(p.name)}</h4><button type="button" class="btn-secondary" data-delete-pool="${escape(p.id)}">${escape(t('remove'))}</button><ul>${p.nodes.map(n => {
            const health = data.health.nodes[p.id + ':' + n.id] || {};
            return `<li><code>${escape(n.id)}</code> <span>${escape(n.address)}</span> <span>${escape(n.region || '')}</span><small>${n.enabled ? escape(health.circuit_open ? t('open') : t('ready')) : 'Disabled'} · active: ${Number(health.active || 0)} · failures: ${Number(health.failures || 0)}</small><button type="button" class="btn-secondary" data-probe-pool="${escape(p.id)}" data-probe-node="${escape(n.id)}">${escape(t('probe'))}</button></li>`;
        }).join('')}</ul></section>`).join('') : escape(t('empty'));
        el('nodes').querySelectorAll('[data-delete-pool]').forEach(button=>button.onclick=()=>{if(window.confirm(t('removeConfirm')))run(async()=>{await request('/'+encodeURIComponent(button.dataset.deletePool),{method:'DELETE'});await refresh();show(t('saved'));});});
        el('nodes').querySelectorAll('[data-probe-node]').forEach(button=>button.onclick=()=>run(async()=>{button.disabled=true;try{const result=await request('/probe',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({pool_id:button.dataset.probePool,node_id:button.dataset.probeNode,url:el('probe-url').value.trim()})});show(`${result.usable ? (document.documentElement.lang.startsWith("zh") ? "验证通过" : "Verified") : (document.documentElement.lang.startsWith("zh") ? "已收到响应，目标未验证成功" : "Response received; target not verified")} · HTTP ${result.http_status} · ${result.latency_ms} ms`);}finally{button.disabled=false;}}));
    }
    function mount() {
        const nav = document.querySelector('.settings-nav'), parent = document.querySelector('#settings-section-basic')?.parentElement;
        if (!nav || !parent || el('panel')) return;
        const button = document.createElement('div'); button.className='settings-nav-item';button.dataset.section='test-proxy';button.dataset.requirePermission='config:write';button.textContent=t('title');
        button.onclick=()=>{switchSettingsSection('test-proxy');run(async()=>{await refresh();await loadBinding();show('');});};nav.appendChild(button);
        const panel=document.createElement('div');panel.id='settings-section-test-proxy';panel.className='settings-section-content';
        panel.innerHTML=`<div id="test-proxy-panel"><div class="settings-section-header"><h3>${escape(t('title'))}</h3></div><p class="test-proxy-hint">${escape(t('hint'))}</p>
        <div class="form-group"><label for="test-proxy-name">${escape(t('name'))}</label><input id="test-proxy-name" maxlength="100" autocomplete="off"></div>
        <div class="test-proxy-limits"><div class="form-group"><label for="test-proxy-concurrency">${escape(t('concurrency'))}</label><input type="number" id="test-proxy-concurrency" min="1" max="16" value="4"></div><div class="form-group"><label for="test-proxy-per-node">${escape(t('perNode'))}</label><input type="number" id="test-proxy-per-node" min="1" max="4" value="1"></div></div>
        <div class="form-group"><label for="test-proxy-text">${escape(t('import'))}</label><textarea id="test-proxy-text" rows="6" spellcheck="false" autocomplete="off"></textarea></div>
        <div class="test-proxy-actions"><button type="button" class="btn-secondary" id="test-proxy-preview">${escape(t('preview'))}</button><button type="button" class="btn-primary" id="test-proxy-save" disabled>${escape(t('save'))}</button><button type="button" class="btn-secondary" id="test-proxy-refresh">${escape(t('refresh'))}</button></div>
        <p id="test-proxy-message" role="status" aria-live="polite"></p><pre id="test-proxy-preview-result"></pre>
        <hr><p class="test-proxy-hint">${escape(t('bindingHint'))}</p><div class="form-group"><label for="test-proxy-pool">${escape(t('pool'))}</label><select id="test-proxy-pool"></select></div><div class="test-proxy-actions"><button type="button" class="btn-secondary" id="test-proxy-load">${escape(t('load'))}</button><button type="button" class="btn-primary" id="test-proxy-bind" disabled>${escape(t('bind'))}</button></div><div class="form-group"><label for="test-proxy-probe-url">${escape(t('probeURL'))}</label><input id="test-proxy-probe-url" type="url" placeholder="https://your-controlled-host/"></div><h4>${escape(t('status'))}</h4><div id="test-proxy-nodes"></div></div>`;
        parent.appendChild(panel);
        ['text','name','concurrency','per-node'].forEach(id=>el(id).addEventListener('input',()=>{validated='';el('save').disabled=true;}));
        el('preview').onclick=()=>run(async()=>{const input=payload();const data=await request('/import',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({...input,preview:true})});validated=JSON.stringify(input);el('save').disabled=false;el('preview-result').textContent=data.nodes.map(n=>`${n.enabled?'✓':'–'} ${n.address} ${n.region||''}`).join('\n');show(t('previewOK'));});
        el('save').onclick=()=>run(async()=>{const input=payload();if(validated!==JSON.stringify(input))throw new Error(t('invalid'));el('save').disabled=true;await request('/import',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(input)});el('text').value='';validated='';await refresh();show(t('saved'));});
        el('refresh').onclick=()=>run(async()=>{await refresh();show('');});
        el('load').onclick=()=>run(async()=>{await loadBinding();show('');});
        el('bind').onclick=()=>run(async()=>{await request('/preference',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({pool_id:el('pool').value})});show(t('saved'));});
        if(typeof rbacAfterDynamicRender==='function')rbacAfterDynamicRender(panel.parentElement);
    }
    if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',mount);else mount();
})();
