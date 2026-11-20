/* Explicit task references. Existing source APIs remain the authority for detail access. */
(function () {
    'use strict';
    let active = null;
    const escape = value => String(value == null ? '' : value).replace(/[&<>"']/g, c => ({ '&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;' }[c]));
    const allowed = permission => typeof window.hasPermission === 'function' && window.hasPermission(permission);

    /** Open one bounded task evidence panel. Reads cancel on task/navigation changes;
     * writes finish on the server and stale responses cannot repaint another task. */
    function open(host, options) {
        stop();
        const current = { controller: new AbortController(), sequence: 0, writing: false };
        active = current;
        const text = options.text, task = options.task;
        const base = '/api/conversations/' + encodeURIComponent(options.conversation) + '/plan-tasks/' + encodeURIComponent(task.id) + '/evidence';
        const find = name => host.querySelector('[data-evidence="' + name + '"]');
        const kinds = ['tool_result', ...(options.project && allowed('project:read') ? ['project_fact'] : []), ...(allowed('vulnerability:read') ? ['vulnerability'] : [])];
        host.hidden = false;
        host.innerHTML = '<header><h3 tabindex="-1">' + escape(text('evidenceTitle')) + ' · #' + escape(task.id) + ' ' + escape(task.subject) + '</h3><button type="button" data-evidence="close">' + escape(text('closeEvidence')) + '</button></header><p>' + escape(text('evidenceHint')) + '</p><p data-evidence="notice" role="status" aria-live="polite"></p><ul data-evidence="items"></ul>' +
            '<form data-evidence="form"' + (allowed('chat:write') ? '' : ' hidden') + '><label>' + escape(text('sourceKind')) + '<select data-evidence="kind">' + kinds.map(kind => '<option value="' + kind + '">' + escape(text(kind)) + '</option>').join('') + '</select></label><label>' + escape(text('searchEvidence')) + '<input data-evidence="search" maxlength="100" type="search"></label><button type="button" data-evidence="searchButton">' + escape(text('searchEvidence')) + '</button><label class="execution-evidence-picker">' + escape(text('sourceRecord')) + '<select data-evidence="source"></select></label><button type="submit" data-evidence="save">' + escape(text('linkEvidence')) + '</button></form>';
        find('close').onclick = () => { stop(); options.onClose(); };
        const live = () => active === current && host.isConnected;
        function clearDenied(response) {
            if (live() && [401,403,404,409].includes(response.status)) {
                find('items').replaceChildren(); find('source').replaceChildren(); find('form').hidden = true;
            }
        }
        async function read(url) {
            const response = await apiFetch(url, { signal: current.controller.signal });
            if (!response.ok) { clearDenied(response); throw new Error('Evidence unavailable'); }
            return response.json();
        }
        async function loadLinks() {
            const result = await read(base);
            if (!live()) return;
            find('items').innerHTML = (result.items || []).slice(0, 20).map((item, index) => '<li><div><strong>' + escape(item.label || item.sourceId) + '</strong><small>' + escape(text(item.kind)) + '</small></div><button type="button" data-open="' + index + '">' + escape(text('viewSource')) + '</button>' + (allowed('chat:delete') ? '<button type="button" data-remove="' + index + '">' + escape(text('unlinkEvidence')) + '</button>' : '') + '</li>').join('') || '<li>' + escape(text('noEvidence')) + '</li>';
            find('items').onclick = async event => {
                const button = event.target.closest('button'); if (!button) return;
                const item = (result.items || [])[Number(button.dataset.open ?? button.dataset.remove)];
                if (!item) return;
                if (button.dataset.remove !== undefined) return write(base + '/' + encodeURIComponent(item.id), { method:'DELETE' });
                if (item.kind === 'tool_result') {
                    options.onSource(item); return;
                }
                // Existing project/vulnerability pages recheck access when loading source details.
                if (item.kind === 'project_fact' && typeof window.openProjectFactFromDashboard === 'function') window.openProjectFactFromDashboard(item.projectId, item.factKey);
                else if (item.kind === 'vulnerability' && typeof window.setVulnerabilityIdFilter === 'function') {
                    window.switchPage('vulnerabilities'); window.setVulnerabilityIdFilter(item.sourceId);
                }
            };
        }
        async function sources() {
            const sequence = ++current.sequence, kind = find('kind').value, search = find('search').value.trim();
            find('source').replaceChildren(); find('save').disabled = true;
            try {
                let items;
                if (kind === 'tool_result') items = options.tools.filter(item => !search || item.label.toLowerCase().includes(search.toLowerCase()));
                else {
                    const result = await read(base.replace(/\/evidence$/, '/evidence-sources') + '?' + new URLSearchParams({kind,search}));
                    items = (result.items || []).map(item => ({id:item.sourceId,label:item.label}));
                }
                if (!live() || sequence !== current.sequence) return;
                find('source').innerHTML = items.slice(0, 100).map(item => '<option value="' + escape(item.id) + '">' + escape(String(item.label || item.id).slice(0,160)) + '</option>').join('');
                find('save').disabled = !items.length || current.writing;
                find('notice').textContent = text(items.length ? 'sourceScope' : 'noSources');
            } catch (_) { if (live() && sequence === current.sequence) find('notice').textContent = text('evidenceError'); }
        }
        /** Mutations are not aborted after sending; retry is idempotent. Source and
         * task permissions/identity are checked on the server for every write. */
        async function write(url, request) {
            if (!live() || current.writing) return;
            current.writing = true; find('save').disabled = true;
            try {
                const response = await apiFetch(url, request);
                if (!response.ok) { clearDenied(response); throw new Error('Unable to save reference'); }
                if (!live()) return;
                await loadLinks();
                if (live()) find('notice').textContent = text('evidenceSaved');
            } catch (_) { if (live()) find('notice').textContent = text('evidenceError'); }
            finally { current.writing = false; if (live()) find('save').disabled = !find('source').value; }
        }
        find('form').onsubmit = event => {
            event.preventDefault(); if (!find('source').value) return;
            write(base, {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({kind:find('kind').value,sourceId:find('source').value,taskIdentity:task.identity})});
        };
        find('kind').onchange = sources; find('searchButton').onclick = sources;
        find('search').onkeydown = event => { if (event.key === 'Enter') { event.preventDefault(); sources(); } };
        loadLinks().then(() => { if (live() && allowed('chat:write')) return sources(); }).catch(() => { if (live()) find('notice').textContent = text('evidenceError'); });
        host.querySelector('h3').focus({ preventScroll:true });
    }

    /** Cancel reference/source reads and remove account-specific panel content. */
    function stop() { if (active) { active.controller.abort(); active = null; } }
    window.ExecutionEvidence = { open, stop };
})();
