/* Recorded execution overview. Reuses conversation RBAC APIs; never runs tools. */
(function () {
    'use strict';
    const PAGE_SIZE = 50;
    const state = { conversations: [], conversation: '', turns: [], turn: '', details: [], summary: {},
        listOffset: 0, listTotal: 0, offset: 0, total: 0, before: '', filter: 'all', auto: true,
        failures: 0, updated: null, controller: null, timer: null, epoch: 0, active: false };
    const terminal = new Set(['completed', 'failed', 'cancelled', 'timeout']);
    const statusKeys = new Set(['completed', 'failed', 'cancelled', 'timeout', 'running', 'pending', 'queued',
        'blocked', 'result_missing', 'background_running', 'hard_timeout', 'orphaned']);
    const modelEvents = new Set(['iteration', 'thinking', 'reasoning_chain', 'planning', 'model_output_rejected']);
    const toolEvents = new Set(['tool_call', 'tool_result', 'workflow_tool_start']);
    const failedEvents = new Set(['error', 'timeout', 'cancelled', 'model_output_rejected']);
    const el = id => document.getElementById('execution-' + id);
    const text = key => typeof window.t === 'function' ? window.t('executionView.' + key) : key;
    const escape = value => String(value == null ? '' : value).replace(/[&<>"']/g, c => ({
        '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
    const count = value => Number.isFinite(Number(value)) ? Math.max(0, Math.floor(Number(value))) : 0;
    const time = value => {
        const date = new Date(value);
        return value && Number.isFinite(date.getTime()) ? date.toLocaleString() : '—';
    };
    const status = value => statusKeys.has(value) ? value : 'unknown';
    const badge = value => '<span class="execution-badge" data-status="' + status(value) + '">' + escape(text(status(value))) + '</span>';

    /** Classify persisted events without exposing reasoning, arguments or output payloads. */
    function describe(detail, executions) {
        const data = detail.data && typeof detail.data === 'object' ? detail.data : {};
        const type = String(detail.eventType || '');
        const execution = executions instanceof Map ? executions.get(String(detail.id)) : executions.find(item =>
            String(item.processDetailId) === String(detail.id) || String(item.resultDetailId) === String(detail.id));
        let outcome = execution ? status(execution.status) : 'unknown';
        if (!execution && type === 'tool_result') outcome = data.success === false || data.isError === true ? 'failed' :
            data.success === true || data.isError === false ? 'completed' : status(data.status);
        if (type === 'error' || type === 'timeout' || type === 'cancelled') outcome = type === 'error' ? 'failed' : type;
        if (type === 'hitl_interrupt' || type === 'workflow_hitl_waiting') outcome = 'pending';
        if (type === 'hitl_rejected' || type === 'workflow_hitl_rejected') outcome = 'blocked';
        if (type === 'hitl_resumed' || type === 'workflow_hitl_resumed') outcome = 'completed';
        const group = failedEvents.has(type) ? 'issues' : toolEvents.has(type) ? 'tools' : modelEvents.has(type) ? 'model' : 'control';
        const title = toolEvents.has(type) ? String(data.toolName || data.tool || text('tool')).slice(0, 100) : text(group);
        const issue = failedEvents.has(type) || ['failed', 'blocked', 'timeout', 'hard_timeout', 'orphaned', 'result_missing'].includes(outcome);
        return { group, title, outcome, issue, agent: String(data.einoAgent || '').slice(0, 80),
            iteration: count(data.iteration || data.iterationNumber), type: type.slice(0, 80) };
    }

    /** Stop reads on navigation, logout or hidden tabs; a stale response cannot repaint. */
    function cancel() {
        clearTimeout(state.timer);
        state.timer = null;
        if (state.controller) state.controller.abort();
        state.controller = null;
        state.epoch++;
    }

    function clearRecords() {
        state.conversations = []; state.turns = []; state.details = []; state.summary = {};
        state.conversation = ''; state.turn = ''; state.total = 0; state.before = '';
        state.listOffset = 0; state.listTotal = 0; state.offset = 0;
    }

    /** Fetch through existing authentication. Abort/failed responses never become empty success. */
    async function read(path, controller) {
        const response = await apiFetch(path, { signal: controller.signal });
        if (controller.signal.aborted) throw new DOMException('Aborted', 'AbortError');
        if (!response.ok) {
            const error = new Error('Execution view request failed');
            error.status = response.status;
            throw error;
        }
        const result = await response.json();
        if (controller.signal.aborted) throw new DOMException('Aborted', 'AbortError');
        return result;
    }

    function schedule() {
        if (!state.active || document.hidden || !state.auto || !state.turn || state.failures >= 3 ||
            terminal.has(state.summary.status)) return;
        state.timer = setTimeout(() => run(loadTurn), 10000 * Math.max(1, state.failures + 1));
    }

    /** Serialize refreshes, preserve a last-good snapshot on errors, pause after three failures. */
    async function run(job) {
        cancel();
        if (!state.active || document.hidden) return;
        const epoch = state.epoch;
        const controller = new AbortController();
        state.controller = controller;
        el('notice').textContent = text('loading');
        el('notice').setAttribute('role', 'status');
        el('content').setAttribute('aria-busy', 'true');
        try {
            await job(controller);
            if (epoch !== state.epoch) return;
            state.failures = 0;
            state.updated = new Date().toISOString();
            render();
            el('notice').textContent = text('updated') + ' ' + time(state.updated);
        } catch (error) {
            if (epoch !== state.epoch || controller.signal.aborted) return;
            state.failures++;
            if (error.status === 401 || error.status === 403 || error.status === 404) {
                clearRecords(); state.auto = false;
                render();
            }
            el('notice').setAttribute('role', 'alert');
            el('notice').textContent = text(error.status === 403 || error.status === 401 ? 'denied' :
                state.failures >= 3 ? 'pausedError' : 'readError');
        } finally {
            if (epoch === state.epoch) {
                state.controller = null;
                el('content').setAttribute('aria-busy', 'false');
                schedule();
            }
        }
    }

    async function loadConversations(controller) {
        const params = new URLSearchParams({ limit: '25', offset: String(state.listOffset), search: el('search').value });
        const result = await read('/api/conversations?' + params, controller);
        state.conversations = Array.isArray(result.conversations) ? result.conversations : [];
        state.listTotal = count(result.total);
        if (!state.conversation && state.conversations.length) {
            state.conversation = state.conversations[0].id;
            await loadTurns(controller);
        }
    }

    async function loadTurns(controller, older = false) {
        const params = new URLSearchParams({ message_limit: '20', include_process_details: '0' });
        if (older && state.before) params.set('before_message_id', state.before);
        const result = await read('/api/conversations/' + encodeURIComponent(state.conversation) + '?' + params, controller);
        state.turns = (result.messages || []).filter(message => message.role === 'assistant').reverse();
        state.before = result.messagePage && result.messagePage.hasMore ? result.messagePage.beforeMessageId : '';
        // Do not retain message content or reasoning in this view's state.
        state.turns = state.turns.map(message => ({ id: message.id, createdAt: message.createdAt }));
        state.turn = state.turns.length ? state.turns[0].id : '';
        state.offset = 0; state.details = []; state.summary = {}; state.total = 0;
        if (state.turn) await loadTurn(controller);
    }

    async function loadTurn(controller, offset = state.offset) {
        if (!state.turn) return;
        const base = '/api/messages/' + encodeURIComponent(state.turn) + '/process-details';
        // Sequential reads avoid competing refreshes and do not request full histories.
        const summary = await read(base + '?summary=1', controller);
        const result = await read(base + '?limit=' + PAGE_SIZE + '&offset=' + offset, controller);
        state.offset = offset;
        state.summary = summary.summary || {};
        state.details = Array.isArray(result.processDetails) ? result.processDetails : [];
        state.total = count(result.total);
    }

    function render() {
        const select = (name, items, selected, label) => {
            el(name).innerHTML = '<option value="">' + escape(text('select')) + '</option>' + items.map(item =>
                '<option value="' + escape(item.id) + '">' + escape(label(item)) + '</option>').join('');
            el(name).value = selected;
        };
        const conversations = state.conversations.slice();
        if (state.conversation && !conversations.some(item => item.id === state.conversation))
            conversations.unshift({ id: state.conversation, title: text('linkedConversation') });
        select('conversation', conversations, state.conversation, item => item.title || text('untitled'));
        select('turn', state.turns, state.turn, item => time(item.createdAt));
        const summary = state.summary;
        el('auto').checked = state.auto;
        el('filter').value = state.filter;
        el('older-turns').disabled = !state.before;
        el('latest-turns').disabled = !state.conversation;
        el('list-prev').disabled = state.listOffset === 0;
        el('list-next').disabled = state.listOffset + 25 >= state.listTotal;
        el('list-count').textContent = text('conversations') + ': ' + state.listTotal;
        el('prev').disabled = state.offset === 0;
        el('next').disabled = state.offset + PAGE_SIZE >= state.total;
        el('range').textContent = state.total ? (state.offset + 1) + '–' + Math.min(state.offset + PAGE_SIZE, state.total) + ' / ' + state.total : '0 / 0';
        el('open').disabled = !state.conversation;
        const executions = Array.isArray(summary.toolExecutions) ? summary.toolExecutions : [];
        const failed = executions.filter(item => ['failed', 'blocked', 'timeout', 'hard_timeout', 'orphaned', 'result_missing'].includes(item.status)).length;
        el('stats').innerHTML = [ ['turnStatus', state.turn ? badge(summary.status) : '—'],
            ['iterations', count(summary.iterationCount)], ['actions', count(summary.toolCount)],
            ['issues', failed], ['duration', summary.durationMs > 0 ? (summary.durationMs / 1000).toFixed(1) + ' s' : '—'] ]
            .map(([key, value]) => '<div class="execution-stat"><span>' + escape(text(key)) + '</span><strong>' + value + '</strong></div>').join('');
        el('stages').innerHTML = ['selected', 'model', 'tools', 'outcome'].map((key, index) => {
            const recorded = [!!state.turn, count(summary.iterationCount) > 0, count(summary.toolCount) > 0, terminal.has(summary.status)][index];
            return '<div class="execution-stage' + (recorded ? ' recorded' : '') + '"><span>' + (index + 1) + '</span>' + escape(text(key)) +
                '<small>' + escape(text(recorded ? 'recorded' : 'noRecord')) + '</small></div>';
        }).join('');
        const byDetail = new Map();
        executions.forEach(item => {
            if (item.processDetailId) byDetail.set(String(item.processDetailId), item);
            if (item.resultDetailId) byDetail.set(String(item.resultDetailId), item);
        });
        const items = state.details.map(detail => ({ detail, view: describe(detail, byDetail) }))
            .filter(item => state.filter === 'all' || (state.filter === 'issues' ? item.view.issue : item.view.group === state.filter));
        el('events').innerHTML = items.length ? items.map(({ detail, view }) =>
            '<li class="execution-event"><div class="execution-event-mark" aria-hidden="true"></div><div class="execution-event-main">' +
            '<div class="execution-event-heading"><strong>' + escape(view.title) + '</strong>' + badge(view.outcome) + '</div>' +
            '<p>' + escape(text(view.group + 'Hint')) + '</p><div class="execution-meta"><time>' + escape(time(detail.createdAt)) + '</time>' +
            (view.agent ? '<span>' + escape(view.agent) + '</span>' : '') + (view.iteration ? '<span>' + escape(text('iteration')) + ' ' + view.iteration + '</span>' : '') +
            '<code>' + escape(view.type) + '</code></div></div></li>').join('') :
            '<li class="execution-empty">' + escape(text(state.turn ? 'noEvents' : 'empty')) + '</li>';
    }

    function mount() {
        const root = document.getElementById('page-execution-view');
        root.innerHTML = '<div class="execution-shell"><header class="execution-header"><div><p class="execution-eyebrow">' + escape(text('eyebrow')) +
            '</p><h1>' + escape(text('title')) + '</h1><p>' + escape(text('intro')) + '</p></div><button id="execution-open" type="button">' + escape(text('open')) + '</button></header>' +
            '<section class="execution-panel" aria-label="' + escape(text('selection')) + '"><form id="execution-search-form" class="execution-search">' +
            '<label>' + escape(text('search')) + '<input id="execution-search" type="search" maxlength="200"></label><button type="submit">' + escape(text('search')) + '</button>' +
            '<span id="execution-list-count"></span><button id="execution-list-prev" type="button">‹</button><button id="execution-list-next" type="button">›</button></form>' +
            '<div class="execution-selectors"><label>' + escape(text('conversation')) + '<select id="execution-conversation"></select></label>' +
            '<label>' + escape(text('turn')) + '<select id="execution-turn"></select></label><button id="execution-latest-turns" type="button">' + escape(text('latestTurns')) + '</button><button id="execution-older-turns" type="button">' + escape(text('olderTurns')) + '</button></div></section>' +
            '<div id="execution-notice" class="execution-notice" role="status" aria-live="polite"></div><div id="execution-content">' +
            '<section id="execution-stats" class="execution-stats" aria-label="' + escape(text('overview')) + '"></section>' +
            '<section class="execution-panel"><h2>' + escape(text('stages')) + '</h2><div id="execution-stages" class="execution-stages"></div></section>' +
            '<section class="execution-panel"><div class="execution-toolbar"><h2>' + escape(text('timeline')) + '</h2><label>' + escape(text('filter')) +
            '<select id="execution-filter">' + ['all', 'model', 'tools', 'issues', 'control'].map(key => '<option value="' + key + '">' + escape(text(key)) + '</option>').join('') +
            '</select></label><label class="execution-auto"><input id="execution-auto" type="checkbox">' + escape(text('auto')) + '</label><button id="execution-refresh" type="button">' + escape(text('refresh')) + '</button></div>' +
            '<ol id="execution-events" class="execution-events"></ol><footer class="execution-pagination"><button id="execution-prev" type="button">' + escape(text('previous')) + '</button>' +
            '<span id="execution-range"></span><button id="execution-next" type="button">' + escape(text('next')) + '</button></footer><p class="execution-disclaimer">' + escape(text('privacy')) + '</p></section></div></div>';
        el('search-form').addEventListener('submit', event => { event.preventDefault(); state.listOffset = 0; run(loadConversations); });
        el('conversation').addEventListener('change', () => {
            state.conversation = el('conversation').value; state.turn = ''; state.turns = []; state.details = []; state.summary = {}; state.total = 0; state.before = '';
            render(); if (state.conversation) run(loadTurns); else cancel();
        });
        el('turn').addEventListener('change', () => { state.turn = el('turn').value; state.offset = 0; state.details = []; state.summary = {}; state.total = 0; render(); if (state.turn) run(loadTurn); else cancel(); });
        el('older-turns').addEventListener('click', () => run(controller => loadTurns(controller, true)));
        el('latest-turns').addEventListener('click', () => run(loadTurns));
        el('filter').addEventListener('change', () => { state.filter = el('filter').value; render(); });
        el('auto').addEventListener('change', () => { state.auto = el('auto').checked; cancel(); if (state.auto) run(loadTurn); });
        el('refresh').addEventListener('click', () => { state.failures = 0; run(state.turn ? loadTurn : loadConversations); });
        [['list-prev', -25], ['list-next', 25]].forEach(([id, delta]) => el(id).addEventListener('click', () => { state.listOffset = Math.max(0, state.listOffset + delta); run(loadConversations); }));
        [['prev', -PAGE_SIZE], ['next', PAGE_SIZE]].forEach(([id, delta]) => el(id).addEventListener('click', () => run(controller => loadTurn(controller, Math.max(0, state.offset + delta)))));
        el('open').addEventListener('click', () => { if (state.conversation) navigateToConversation(state.conversation); });
        el('list-prev').setAttribute('aria-label', text('previous'));
        el('list-next').setAttribute('aria-label', text('next'));
    }

    /** Initialize the read-only page. Existing API middleware remains the access authority. */
    window.initExecutionView = function () {
        cancel();
        if (!document.getElementById('page-execution-view').classList.contains('active')) return;
        state.active = true;
        mount();
        if (typeof hasPermission === 'function' && !hasPermission('chat:read')) {
            state.active = false; el('notice').textContent = text('denied'); return;
        }
        const params = new URLSearchParams(window.location.hash.split('?').slice(1).join('?'));
        const linked = params.get('conversation');
        if (linked && linked !== state.conversation) {
            state.conversation = linked; state.turn = ''; state.turns = []; state.details = []; state.summary = {};
        }
        render();
        run(async controller => { const selected = state.conversation; await loadConversations(controller); if (selected) await loadTurns(controller); });
    };
    window.stopExecutionView = function () {
        state.active = false; cancel(); clearRecords();
        const root = document.getElementById('page-execution-view');
        if (root) root.replaceChildren();
    };
    window.syncExecutionViewFromHash = function () {
        const linked = new URLSearchParams(window.location.hash.split('?').slice(1).join('?')).get('conversation');
        if (state.active && linked && linked !== state.conversation) window.initExecutionView();
    };
    document.addEventListener('visibilitychange', () => { cancel(); if (state.active && !document.hidden) run(state.turn ? loadTurn : loadConversations); });
    document.addEventListener('languagechange', () => { if (state.active) { mount(); render(); } });
    // A pure projection is shared with regression tests; it never returns raw payloads.
    if (typeof module !== 'undefined' && module.exports) module.exports = { describe, status, count };
})();
