/* Recorded execution overview. Reuses conversation RBAC APIs; never runs tools. */
(function () {
    'use strict';
    const PAGE_SIZE = 50;
    let disposeSwarm = null, replaySwarm = null;
    const state = { conversations: [], conversation: '', turns: [], turn: '', details: [], summary: {},
        listOffset: 0, listTotal: 0, offset: 0, total: 0, before: '', filter: 'all', auto: true,
        failures: 0, updated: null, controller: null, timer: null, epoch: 0, active: false,
        tasks: [], taskOffset: 0, taskTotal: 0, boardRunning: false, view: 'board', focus: null,
        swarmOpen: false, boardPartial: false, boundaryKnown: false, followLatest: true, project: '', evidenceTask: '', evidenceIdentity: '' };
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
        state.tasks = []; state.taskOffset = 0; state.taskTotal = 0; state.boardRunning = false;
        state.boundaryKnown = false;
        state.focus = null; state.boardPartial = false; state.project = ''; state.evidenceTask = '';
        if (window.ExecutionEvidence) window.ExecutionEvidence.stop();
        if (el('evidence')) { el('evidence').replaceChildren(); el('evidence').hidden = true; }
    }

    /** Map saved task states to board lanes. Completed is awaiting independent
     * verification; this projection never claims evidence has been verified. */
    function boardTask(raw) {
        raw = raw && typeof raw === 'object' ? raw : {};
        raw = { ...raw, status: String(raw.status || '').trim().toLowerCase() };
        const known = ['pending', 'in_progress', 'completed', 'deleted'].includes(raw.status);
        const recordedStatus = known ? raw.status : 'unknown';
        return { id: String(raw.id || '').slice(0, 80), subject: String(raw.subject || text('untitledTask')).slice(0, 200),
            description: String(raw.description || '').slice(0, 600), owner: String(raw.owner || '').slice(0, 80),
            blockedBy: (Array.isArray(raw.blockedBy) ? raw.blockedBy : []).slice(0, 10).map(id => String(id).slice(0, 40)),
            identity: String(raw.identity || ''), updatedAt: raw.updatedAt || '',
            runtimeStatus: ['paused', 'previous_run', 'in_progress', 'awaiting_verification', 'pending'].includes(raw.runtimeStatus) ? raw.runtimeStatus : '',
            status: recordedStatus, lane: recordedStatus === 'in_progress' ? 'execute' : recordedStatus === 'completed' ? 'verify' : 'plan' };
    }

    /** Group persisted failures by tool/status without copying upstream errors or
     * reasoning. Tool counts span the turn; approval/error events are page-scoped. */
    function issueGroups(executions, details) {
        const groups = new Map();
        const failures = new Set(['failed', 'blocked', 'timeout', 'hard_timeout', 'orphaned', 'result_missing']);
        (executions || []).forEach(item => {
            if (!failures.has(item.status)) return;
            const name = String(item.toolName || '').slice(0,100), key = JSON.stringify([name,item.status]);
            const previous = groups.get(key);
            if (previous) previous.count++;
            else groups.set(key,{ name, status:item.status, count:1, anchor:String(item.resultDetailId || item.processDetailId || '') });
        });
        const control = details.filter(item => ['hitl_interrupt','workflow_hitl_waiting','hitl_resumed','workflow_hitl_resumed','hitl_rejected','workflow_hitl_rejected'].includes(item.eventType)).slice(-1)[0];
        if (control && ['hitl_interrupt','workflow_hitl_waiting'].includes(control.eventType)) groups.set('approval',{ name:'',status:'pending',count:1,anchor:String(control.id || ''),page:true });
        const error = details.filter(item => failedEvents.has(item.eventType)).slice(-1)[0];
        if (error) groups.set('page_error',{name:'',status:error.eventType === 'timeout' ? 'timeout' : error.eventType === 'cancelled' ? 'cancelled' : 'failed',count:1,anchor:String(error.id || ''),page:true});
        return [...groups.values()];
    }

    /** Present current runtime separately from saved tool failures and task state. */
    function renderIssues(summary, executions) {
        const groups = issueGroups(executions,state.details);
        el('issue-list').innerHTML = groups.slice(0,8).map(item => '<article><div><strong>' + escape(item.name || text(item.status === 'pending' ? 'approvalWaiting' : 'issues')) + '</strong>' + badge(item.status) + '<span>×' + item.count + '</span></div><p>' + escape(text(item.page ? 'approvalPageScope' : 'toolFailureHint')) + '</p>' + (item.anchor ? '<button type="button" data-issue-anchor="' + escape(item.anchor) + '">' + escape(text('viewSource')) + '</button>' : '') + '</article>').join('') || '<p class="execution-issue-empty">' + escape(text('noRecordedIssues')) + '</p>';
        if (['failed','cancelled','timeout'].includes(summary.status)) el('issue-list').insertAdjacentHTML('afterbegin','<p>' + escape(text('recordedTurnState')) + ' ' + badge(summary.status) + '</p>');
        if (!state.boardRunning && state.tasks.some(task => task.status === 'in_progress')) el('issue-list').insertAdjacentHTML('afterbegin','<p>' + escape(text('pausedTaskHint')) + '</p>');
    }

    function closeEvidence() {
        state.evidenceTask = ''; state.evidenceIdentity = '';
        if (window.ExecutionEvidence) window.ExecutionEvidence.stop();
        if (el('evidence')) { el('evidence').hidden = true; el('evidence').replaceChildren(); }
    }

    /** References are manually chosen; temporal proximity never creates a link. */
    function openEvidence(id) {
        const task = state.tasks.find(item => item.id === id);
        if (!task || !window.ExecutionEvidence) return;
        state.evidenceTask = id;
        state.evidenceIdentity = task.identity;
        const tools = state.details.filter(detail => detail.eventType === 'tool_result').map(detail => ({id:detail.id,label:String(detail.data && detail.data.toolName || text('tool')).slice(0,100)}));
        window.ExecutionEvidence.open(el('evidence'),{ text, task, tools, project:state.project, conversation:state.conversation,
            onClose:() => { closeEvidence(); const button = [...el('board-columns').querySelectorAll('[data-evidence-task]')].find(node => node.dataset.evidenceTask === id); if (button) button.focus({preventScroll:true}); },
            onSource:item => openAnchor(item.sourceId,item.messageId) });
        el('evidence').scrollIntoView({block:'nearest'});
    }

    /** Resolve an off-page source through the existing authenticated anchor API. */
    function openAnchor(anchor, message = state.turn) {
        if (!anchor || !message) return;
        const conversation = state.conversation;
        state.followLatest = false; state.turn = message; state.view = 'timeline'; state.filter = 'all';
        if (!state.turns.some(turn => turn.id === message)) state.turns.unshift({id:message});
        run(controller => loadTurn(controller,0,anchor)).then(() => {
            if (!state.active || state.conversation !== conversation || state.turn !== message || !el('events')) return;
            const row = [...el('events').querySelectorAll('[data-detail-id]')].find(node => node.dataset.detailId === anchor);
            if (row) { row.tabIndex = -1; row.focus({preventScroll:true}); row.scrollIntoView({block:'nearest'}); }
        });
    }

    /** Build a bounded topology from recorded Agent/tool metadata only. Edges
     * never imply delegation, target reachability or unrecorded tool activity. */
    function topology(details) {
        const agents = new Set(), tools = new Set(), links = new Map();
        details.forEach(detail => {
            const data = detail.data && typeof detail.data === 'object' ? detail.data : {};
            const agent = typeof data.einoAgent === 'string' ? data.einoAgent.trim().slice(0, 80) : '';
            if (agent) agents.add(agent);
            if (!toolEvents.has(detail.eventType)) return;
            const tool = typeof data.toolName === 'string' ? data.toolName.trim().slice(0, 100) : '';
            if (!tool) return;
            tools.add(tool);
            if (agent) {
                const key = JSON.stringify([agent, tool]);
                if (!links.has(key)) links.set(key, { agent, tool, events: [] });
                if (links.get(key).events.length < PAGE_SIZE && ['tool_call', 'tool_result'].includes(detail.eventType)) {
                    links.get(key).events.push({ type: detail.eventType });
                }
            }
        });
        const visibleAgents = [...agents].slice(0, 5), visibleTools = [...tools].slice(0, 5);
        return { agents: visibleAgents, tools: visibleTools,
            links: [...links.values()].filter(link => visibleAgents.includes(link.agent) && visibleTools.includes(link.tool)),
            omitted: agents.size + tools.size - visibleAgents.length - visibleTools.length };
    }

    function renderDispatch(executions) {
        if (disposeSwarm) { disposeSwarm(); disposeSwarm = null; }
        if (!state.swarmOpen || state.view !== 'board') { el('graph').replaceChildren(); return; }
        const graph = topology(state.details);
        const y = (index, length) => 30 + (index + 1) * 300 / (length + 1);
        const positions = { agent: new Map(graph.agents.map((name, index) => [name, y(index, graph.agents.length)])),
            tool: new Map(graph.tools.map((name, index) => [name, y(index, graph.tools.length)])) };
        const node = (kind, name, index) => '<button type="button" class="execution-graph-node" data-kind="' + kind + '" data-index="' + index +
            '" title="' + escape(name) + '" aria-pressed="' + String(!!state.focus && state.focus.kind === kind && state.focus.name === name) + '" style="left:' + (kind === 'agent' ? 45 : 80) + '%;top:' + (positions[kind].get(name) / 360 * 100) + '%"><small>' +
            escape(text(kind === 'agent' ? 'graphAgent' : 'graphTool')) + '</small><strong>' + escape(name) + '</strong></button>';
        let paths = graph.agents.map(name => '<path d="M 180 180 C 250 180 260 ' + positions.agent.get(name) + ' 360 ' + positions.agent.get(name) + '"/>').join('');
        paths += graph.links.map(link => '<path d="M 360 ' + positions.agent.get(link.agent) + ' C 490 ' + positions.agent.get(link.agent) + ' 510 ' + positions.tool.get(link.tool) + ' 640 ' + positions.tool.get(link.tool) + '"/>').join('');
        el('graph').innerHTML = '<svg viewBox="0 0 800 360" preserveAspectRatio="none" aria-hidden="true"><g class="execution-graph-guides"><circle cx="150" cy="180" r="65"/><circle cx="150" cy="180" r="105"/><circle cx="150" cy="180" r="145"/></g><g class="execution-graph-links">' + paths +
            '</g></svg><div class="execution-graph-core"><small>CyberStrikeAI-EV</small><strong>' + escape(text('graphContext')) + '</strong><span>' + escape(state.conversation ? text('selected') : text('select')) + '</span></div>' +
            graph.agents.map((name, index) => node('agent', name, index)).join('') + graph.tools.map((name, index) => node('tool', name, index)).join('') +
            (!graph.agents.length && !graph.tools.length ? '<p class="execution-graph-empty">' + escape(text('graphEmpty')) + '</p>' : '');
        if (state.view === 'board' && window.ExecutionSwarm) {
            disposeSwarm = window.ExecutionSwarm.mount(el('graph'), graph, {
                text, focus: state.focus,
                setReplay: replay => { replaySwarm = replay; },
                onReplay: playing => { el('replay').textContent = text(playing ? 'pauseReplay' : 'replay'); el('replay').setAttribute('aria-pressed', String(playing)); },
                onReady: ready => { el('replay').disabled = !ready; el('renderer-status').textContent = text(ready ? 'swarmReady' : 'swarmStatic'); }
            });
        }
        el('graph-note').textContent = text('graphScope') + (graph.omitted ? ' · +' + graph.omitted + ' ' + text('graphOmitted') : '');
        const records = state.details.map(detail => ({ detail, view: describe(detail, executions) }));
        const filtered = state.focus ? records.filter(item => state.focus.kind === 'agent' ? item.view.agent === state.focus.name : item.view.group === 'tools' && item.view.title === state.focus.name) : records;
        el('feed-title').textContent = state.focus ? state.focus.name : text('feedTitle');
        el('feed-reset').hidden = !state.focus;
        el('feed').innerHTML = filtered.slice(-8).reverse().map(({ detail, view }) => '<li><div><strong>' + escape(view.title) + '</strong>' + badge(view.outcome) + '</div><p><time>' + escape(time(detail.createdAt)) + '</time><span>' + escape(view.agent || text(view.group)) + '</span></p></li>').join('') ||
            '<li class="execution-lane-empty">' + escape(text('noEvents')) + '</li>';
        el('dispatch-status').textContent = text(state.summary.status === 'running' ? 'dispatchActive' : 'dispatchRecorded');
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
        if (!state.active || document.hidden || !state.auto || !state.conversation || state.failures >= 3) return;
        const interval = state.boardRunning || state.summary.status === 'running' ? 10000 : 30000;
        state.timer = setTimeout(() => run(refreshCurrent), interval * Math.max(1, state.failures + 1));
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

    /** Follow the latest run unless a user deliberately selects historical turns. */
    async function refreshCurrent(controller) {
        if (!state.conversation) return loadConversations(controller);
        if (state.followLatest) return loadTurns(controller);
        return state.turn ? loadTurn(controller) : loadBoard(controller);
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
        const previousTurn = state.turn, previousOffset = state.offset;
        const params = new URLSearchParams({ message_limit: '20', include_process_details: '0' });
        if (older && state.before) params.set('before_message_id', state.before);
        const result = await read('/api/conversations/' + encodeURIComponent(state.conversation) + '?' + params, controller);
        state.project = String(result.projectId || '');
        state.turns = (result.messages || []).filter(message => message.role === 'assistant').reverse();
        state.before = result.messagePage && result.messagePage.hasMore ? result.messagePage.beforeMessageId : '';
        // Do not retain message content or reasoning in this view's state.
        state.turns = state.turns.map(message => ({ id: message.id, createdAt: message.createdAt }));
        state.turn = state.turns.length ? state.turns[0].id : '';
        state.offset = !older && previousTurn === state.turn ? previousOffset : 0;
        if (state.turn) await loadTurn(controller); else await loadBoard(controller);
    }

    async function loadBoard(controller, offset = state.taskOffset) {
        if (!state.conversation) return;
        const params = new URLSearchParams({ snapshot: 'latest', limit: String(PAGE_SIZE), offset: String(offset) });
        const result = await read('/api/conversations/' + encodeURIComponent(state.conversation) + '/plan-tasks?' + params, controller);
        state.boardRunning = result.running === true; state.boardPartial = result.incomplete === true;
        state.boundaryKnown = result.boundaryKnown === true;
        if (!state.boardPartial || !state.tasks.length) {
            state.tasks = (Array.isArray(result.tasks) ? result.tasks : []).map(boardTask).filter(task => task.status !== 'deleted');
            state.taskOffset = offset; state.taskTotal = count(result.total);
        }
        if (state.evidenceTask && !state.boardPartial && !state.tasks.some(task => task.id === state.evidenceTask && task.identity === state.evidenceIdentity)) closeEvidence();
    }

    async function loadTurn(controller, offset = state.offset, anchor = '') {
        if (!state.turn) return;
        const base = '/api/messages/' + encodeURIComponent(state.turn) + '/process-details';
        // Sequential reads avoid competing refreshes and do not request full histories.
        const summary = await read(base + '?summary=1', controller);
        const result = await read(base + '?' + new URLSearchParams({limit:String(PAGE_SIZE),...(anchor ? {anchorId:anchor} : {offset:String(offset)})}), controller);
        await loadBoard(controller);
        state.offset = anchor ? count(result.offset) : offset;
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
        el('board-auto').checked = state.auto;
        el('board').hidden = state.view !== 'board';
        el('timeline').hidden = state.view !== 'timeline';
        el('stats').hidden = state.view !== 'timeline';
        ['board', 'timeline'].forEach(name => {
            const selected = state.view === name;
            el('tab-' + name).setAttribute('aria-selected', String(selected));
            el('tab-' + name).tabIndex = selected ? 0 : -1;
        });
        el('board-prev').disabled = state.taskOffset === 0;
        el('board-next').disabled = state.taskOffset + PAGE_SIZE >= state.taskTotal;
        el('board-range').textContent = state.taskTotal ? (state.taskOffset + 1) + '–' + Math.min(state.taskOffset + PAGE_SIZE, state.taskTotal) + ' / ' + state.taskTotal : '0 / 0';
        el('board-runtime').textContent = text(state.boardRunning ? 'boardLive' : 'boardSaved') + ' · ' + text(state.followLatest ? 'followingLatest' : 'historySelected') + (state.conversation && !state.boundaryKnown ? ' · ' + text('legacyBoard') : '') + (state.boardPartial ? ' · ' + text('partialBoard') : '');
        const openCards = new Set([...el('board-columns').querySelectorAll('details[open]')].map(card => card.dataset.task));
        const focusedCard = document.activeElement && document.activeElement.closest('[data-task]');
        const focusedTask = focusedCard && focusedCard.dataset.task;
        const focusedButton = document.activeElement && document.activeElement.hasAttribute('data-evidence-task');
        el('board-columns').innerHTML = ['plan', 'execute', 'verify'].map(lane => {
            const tasks = state.tasks.filter(task => task.lane === lane);
            return '<section class="execution-lane" data-lane="' + lane + '"><header><h3>' + escape(text(lane)) + '</h3><span>' + tasks.length + '</span></header>' +
                '<p class="execution-lane-hint">' + escape(text(lane + 'Hint')) + '</p><div class="execution-lane-cards">' +
                (tasks.length ? tasks.map(task => '<details class="execution-task-card" data-task="' + escape(task.id) + '"' + (openCards.has(task.id) ? ' open' : '') + '><summary><span class="execution-task-id">#' + escape(task.id) + '</span><strong>' + escape(task.subject) +
                    '</strong><span class="execution-task-state">' + escape(text(task.status === 'completed' ? 'awaitingVerification' : task.status === 'in_progress' ? task.runtimeStatus === 'previous_run' ? 'previousRun' : state.boardRunning ? 'taskExecuting' : 'taskPaused' : task.status === 'pending' ? 'taskPlanned' : 'unknown')) + '</span></summary>' +
                    '<p>' + escape(task.description || text('noDescription')) + '</p>' + (task.owner ? '<p>' + escape(text('owner')) + ': ' + escape(task.owner) + '</p>' : '') +
                    (task.blockedBy.length ? '<p>' + escape(text('blockedBy')) + ': ' + escape(task.blockedBy.join(', ')) + '</p>' : '') + (task.updatedAt ? '<p>' + escape(text('taskUpdated')) + ': ' + escape(time(task.updatedAt)) + '</p>' : '') + '<button type="button" data-evidence-task="' + escape(task.id) + '">' + escape(text('evidenceTitle')) + '</button></details>').join('') : '<div class="execution-lane-empty">' + escape(text('noTasks')) + '</div>') + '</div></section>';
        }).join('');
        if (focusedTask) {
            const card = [...el('board-columns').querySelectorAll('[data-task]')].find(node => node.dataset.task === focusedTask);
            if (card) card.querySelector(focusedButton ? '[data-evidence-task]' : 'summary').focus({preventScroll:true});
        }
        const executions = Array.isArray(summary.toolExecutions) ? summary.toolExecutions : [];
        const failed = executions.filter(item => ['failed', 'blocked', 'timeout', 'hard_timeout', 'orphaned', 'result_missing'].includes(item.status)).length;
        el('stats').innerHTML = [ ['turnStatus', state.turn ? badge(summary.status) : '—'],
            ['iterations', count(summary.iterationCount)], ['actions', count(summary.toolCount)],
            ['issues', failed], ['duration', summary.durationMs > 0 ? (summary.durationMs / 1000).toFixed(1) + ' s' : '—'] ]
            .map(([key, value]) => '<div class="execution-stat"><span>' + escape(text(key)) + '</span><strong>' + value + '</strong></div>').join('');
        renderIssues(summary, executions);
        const byDetail = new Map();
        executions.forEach(item => {
            if (item.processDetailId) byDetail.set(String(item.processDetailId), item);
            if (item.resultDetailId) byDetail.set(String(item.resultDetailId), item);
        });
        renderDispatch(byDetail);
        const items = state.details.map(detail => ({ detail, view: describe(detail, byDetail) }))
            .filter(item => state.filter === 'all' || (state.filter === 'issues' ? item.view.issue : item.view.group === state.filter));
        el('events').innerHTML = items.length ? items.map(({ detail, view }) =>
            '<li class="execution-event" data-detail-id="' + escape(detail.id) + '"><div class="execution-event-mark" aria-hidden="true"></div><div class="execution-event-main">' +
            '<div class="execution-event-heading"><strong>' + escape(view.title) + '</strong>' + badge(view.outcome) + '</div>' +
            '<p>' + escape(text(view.group + 'Hint')) + '</p><div class="execution-meta"><time>' + escape(time(detail.createdAt)) + '</time>' +
            (view.agent ? '<span>' + escape(view.agent) + '</span>' : '') + (view.iteration ? '<span>' + escape(text('iteration')) + ' ' + view.iteration + '</span>' : '') +
            '<code>' + escape(view.type) + '</code></div></div></li>').join('') :
            '<li class="execution-empty">' + escape(text(state.turn ? 'noEvents' : 'empty')) + '</li>';
    }

    function mount() {
        closeEvidence();
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
            '<div class="execution-tabs" role="tablist" aria-label="' + escape(text('viewMode')) + '"><button id="execution-tab-board" type="button" role="tab" aria-controls="execution-board">' + escape(text('board')) + '</button><button id="execution-tab-timeline" type="button" role="tab" aria-controls="execution-timeline">' + escape(text('timeline')) + '</button></div>' +
            '<section id="execution-board" role="tabpanel" aria-labelledby="execution-tab-board" tabindex="0" class="execution-panel"><div class="execution-toolbar"><div><h2>' + escape(text('dispatchTitle')) + '</h2><p id="execution-board-runtime" class="execution-lane-hint"></p></div><label class="execution-auto"><input id="execution-board-auto" type="checkbox">' + escape(text('auto')) + '</label><button id="execution-board-refresh" type="button">' + escape(text('refresh')) + '</button></div>' +
            '<section id="execution-issues" class="execution-issues" aria-labelledby="execution-issues-title"><h3 id="execution-issues-title">' + escape(text('issuesTitle')) + '</h3><p>' + escape(text('issuesScope')) + '</p><div id="execution-issue-list"></div></section>' +
            '<h2 class="execution-board-heading">' + escape(text('boardTitle')) + '</h2><p class="execution-board-scope">' + escape(text('boardScope')) + '</p><div id="execution-board-columns" class="execution-board-columns"></div><footer class="execution-pagination"><button id="execution-board-prev" type="button">' + escape(text('previous')) + '</button><span id="execution-board-range"></span><button id="execution-board-next" type="button">' + escape(text('next')) + '</button></footer>' +
            '<section id="execution-evidence" class="execution-evidence" hidden></section>' +
            '<details id="execution-swarm-toggle" class="execution-swarm-toggle"><summary>' + escape(text('swarmOptional')) + '</summary><div class="execution-dispatch"><figure class="execution-graph-panel"><figcaption><span>' + escape(text('graphTitle')) + '</span><span id="execution-dispatch-status"></span></figcaption><div class="execution-swarm-toolbar"><span id="execution-renderer-status">' + escape(text('swarmStatic')) + '</span><button id="execution-replay" type="button" aria-pressed="false" disabled>' + escape(text('replay')) + '</button></div><div id="execution-graph" class="execution-graph" role="group" aria-label="' + escape(text('graphTitle')) + '"></div><p id="execution-graph-note"></p></figure><aside class="execution-feed-panel" aria-label="' + escape(text('feedTitle')) + '"><div><h3 id="execution-feed-title"></h3><button id="execution-feed-reset" type="button">' + escape(text('all')) + '</button></div><ol id="execution-feed"></ol></aside></div></details></section>' +
            '<section id="execution-timeline" role="tabpanel" aria-labelledby="execution-tab-timeline" tabindex="0" class="execution-panel" hidden><div class="execution-toolbar"><h2>' + escape(text('timeline')) + '</h2><label>' + escape(text('filter')) +
            '<select id="execution-filter">' + ['all', 'model', 'tools', 'issues', 'control'].map(key => '<option value="' + key + '">' + escape(text(key)) + '</option>').join('') +
            '</select></label><label class="execution-auto"><input id="execution-auto" type="checkbox">' + escape(text('auto')) + '</label><button id="execution-refresh" type="button">' + escape(text('refresh')) + '</button></div>' +
            '<ol id="execution-events" class="execution-events"></ol><footer class="execution-pagination"><button id="execution-prev" type="button">' + escape(text('previous')) + '</button>' +
            '<span id="execution-range"></span><button id="execution-next" type="button">' + escape(text('next')) + '</button></footer><p class="execution-disclaimer">' + escape(text('privacy')) + '</p></section></div></div>';
        el('swarm-toggle').open = state.swarmOpen;
        el('swarm-toggle').addEventListener('toggle', () => {
            if (state.swarmOpen === el('swarm-toggle').open) return;
            state.swarmOpen = el('swarm-toggle').open; render();
        });
        el('board-columns').addEventListener('click', event => {
            const button = event.target.closest('[data-evidence-task]');
            if (button) openEvidence(button.dataset.evidenceTask);
        });
        el('issue-list').addEventListener('click', event => {
            const button = event.target.closest('[data-issue-anchor]');
            if (button) openAnchor(button.dataset.issueAnchor);
        });
        el('search-form').addEventListener('submit', event => { event.preventDefault(); state.listOffset = 0; run(loadConversations); });
        el('conversation').addEventListener('change', () => {
            state.conversation = el('conversation').value; state.turn = ''; state.turns = []; state.details = []; state.summary = {}; state.total = 0; state.before = '';
            state.tasks = []; state.taskTotal = 0; state.taskOffset = 0; state.boardRunning = false;
            state.focus = null; state.followLatest = true; closeEvidence();
            render(); if (state.conversation) run(loadTurns); else cancel();
        });
        el('turn').addEventListener('change', () => { state.followLatest = false; state.turn = el('turn').value; state.offset = 0; state.details = []; state.summary = {}; state.total = 0; render(); run(state.turn ? loadTurn : loadBoard); });
        el('older-turns').addEventListener('click', () => { state.followLatest = false; run(controller => loadTurns(controller, true)); });
        el('latest-turns').addEventListener('click', () => { state.followLatest = true; run(loadTurns); });
        el('filter').addEventListener('change', () => { state.filter = el('filter').value; render(); });
        el('auto').addEventListener('change', () => { state.auto = el('auto').checked; cancel(); if (state.auto) run(refreshCurrent); });
        el('board-auto').addEventListener('change', () => { state.auto = el('board-auto').checked; cancel(); render(); if (state.auto) run(refreshCurrent); });
        el('board-refresh').addEventListener('click', () => { state.failures = 0; run(refreshCurrent); });
        el('graph').addEventListener('click', event => {
            const button = event.target.closest('button[data-kind]');
            if (!button) return;
            const graph = topology(state.details), kind = button.dataset.kind;
            if (kind !== 'agent' && kind !== 'tool') return;
            const names = kind === 'agent' ? graph.agents : graph.tools;
            const name = names[Number(button.dataset.index)];
            if (name) {
                state.focus = { kind, name }; render();
                el('graph').querySelector('button[data-kind="' + kind + '"][data-index="' + button.dataset.index + '"]').focus({ preventScroll: true });
            }
        });
        el('feed-reset').addEventListener('click', () => { state.focus = null; render(); });
        el('replay').addEventListener('click', () => { if (replaySwarm) replaySwarm(); });
        ['board', 'timeline'].forEach((name, index) => {
            const select = () => { state.view = name; render(); };
            el('tab-' + name).addEventListener('click', select);
            el('tab-' + name).addEventListener('keydown', event => {
                if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
                    event.preventDefault(); state.view = index ? 'board' : 'timeline'; render(); el('tab-' + state.view).focus();
                }
            });
        });
        [['board-prev', -PAGE_SIZE], ['board-next', PAGE_SIZE]].forEach(([id, delta]) => el(id).addEventListener('click', () => run(controller => loadBoard(controller, Math.max(0, state.taskOffset + delta)))));
        el('refresh').addEventListener('click', () => { state.failures = 0; run(refreshCurrent); });
        [['list-prev', -25], ['list-next', 25]].forEach(([id, delta]) => el(id).addEventListener('click', () => { state.listOffset = Math.max(0, state.listOffset + delta); run(loadConversations); }));
        [['prev', -PAGE_SIZE], ['next', PAGE_SIZE]].forEach(([id, delta]) => el(id).addEventListener('click', () => run(controller => loadTurn(controller, Math.max(0, state.offset + delta)))));
        el('open').addEventListener('click', () => { if (state.conversation) navigateToConversation(state.conversation); });
        el('list-prev').setAttribute('aria-label', text('previous'));
        el('list-next').setAttribute('aria-label', text('next'));
    }

    /** Report an authorized mounted lifecycle, including loading/error states;
     * a populated pre-auth denial shell is inactive and must be remounted. */
    window.isExecutionViewActive = function () { return state.active; };

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
            state.tasks = []; state.taskOffset = 0; state.taskTotal = 0; state.boardRunning = false;
        }
        render();
        run(async controller => { const selected = state.conversation; await loadConversations(controller); if (selected) await loadTurns(controller); });
    };
    window.stopExecutionView = function () {
        if (disposeSwarm) { disposeSwarm(); disposeSwarm = null; }
        state.active = false; cancel(); closeEvidence(); clearRecords();
        const root = document.getElementById('page-execution-view');
        if (root) root.replaceChildren();
    };
    window.syncExecutionViewFromHash = function () {
        const linked = new URLSearchParams(window.location.hash.split('?').slice(1).join('?')).get('conversation');
        if (state.active && linked && linked !== state.conversation) window.initExecutionView();
    };
    document.addEventListener('visibilitychange', () => { cancel(); if (state.active && !document.hidden) run(refreshCurrent); });
    document.addEventListener('languagechange', () => { if (state.active) { mount(); render(); } });
    // A pure projection is shared with regression tests; it never returns raw payloads.
    if (typeof module !== 'undefined' && module.exports) module.exports = { describe, status, count, boardTask, topology, issueGroups };
})();
