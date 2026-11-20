const test = require('node:test');
const assert = require('node:assert/strict');
const { requestPage, PAGE_SIZE } = require('./chat-history.js');
const fs = require('node:fs');
const vm = require('node:vm');
const chat = fs.readFileSync('web/static/js/chat.js', 'utf8');

test('history fallback cache expires, evicts bounded snapshots and clears identity data', () => {
    let now = 100000;
    const context = { window: {}, Date: { now: () => now } };
    vm.createContext(context);
    vm.runInContext(chat.slice(chat.indexOf('const CONVERSATION_LITE_CACHE_MAX'), chat.indexOf('// @ 提及相关状态')), context);
    const page = size => ({ messagePage: { hasMore: false }, messages: [{ content: 'x'.repeat(size) }] });
    for (let index = 0; index < 13; index++) context.putConversationLiteCache(String(index), page(10));
    assert.equal(context.getConversationLiteFromCache('0'), null);
    assert.ok(context.getConversationLiteFromCache('1'));
    context.putConversationLiteCache('13', page(10));
    assert.equal(context.getConversationLiteFromCache('2'), null);
    now += 60001;
    assert.equal(context.getConversationLiteFromCache('1'), null);
    context.invalidateConversationLiteCache();
    for (let index = 0; index < 5; index++) context.putConversationLiteCache(String(index), page(1000000));
    assert.equal(context.getConversationLiteFromCache('0'), null);
    context.putConversationLiteCache('1', page(1000001));
    assert.equal(context.getConversationLiteFromCache('1'), null);
    context.putConversationLiteCache('2', { messages: [] });
    assert.equal(context.getConversationLiteFromCache('2'), null);
    context.window.invalidateConversationLiteCache();
    assert.equal(context.getConversationLiteFromCache('4'), null);
});

function functionText(name, next) {
    const start = chat.indexOf('async function ' + name + '(');
    const end = chat.indexOf('function ' + next + '(', start);
    assert.ok(start >= 0 && end > start);
    return chat.slice(start, end).replace(/\/\*\*[^]*$/, '');
}

function historyRuntime() {
    const pending = new Map();
    const rendered = [];
    const states = [];
    const container = { innerHTML: '', setAttribute() {} };
    const context = {
        console: { warn() {}, error() {} }, AbortController, URLSearchParams, setTimeout, clearTimeout, TypeError,
        loadConversationRequestSeq: 0, currentConversationId: null, loadConversationPendingId: '',
        loadConversationAbortController: null, chatHistoryState: null, chatHistoryLoadFailedId: '',
        hitlConfigSyncConversationId: '', hitlConfigSyncPromise: Promise.resolve(),
        document: { getElementById: id => id === 'chat-messages' ? container : null },
        CustomEvent: function () {},
        apiFetch: url => new Promise(resolve => pending.set(new URL(url, 'https://fixture.invalid').pathname.split('/').pop(), resolve)),
        markChatConversationNavigation() {}, syncChatConversationHash() {}, detachLiveChatStreamForNavigation() {},
        cancelPendingConversationLoad() { context.loadConversationAbortController?.abort(); },
        showChatHistoryState(id, failed) { states.push({ id, failed }); rendered.length = 0; },
        getConversationLiteFromCache() { return null; }, putConversationLiteCache() {}, applyConversationAgentMode() {},
        updateChatPrimaryActionState() {}, refreshHitlConfigByCurrentConversation() {}, updateActiveConversation() {},
        renderConversationHistoryMessage(message) { rendered.push(message.id); }, addAttackChainButton() {},
        installChatHistoryPager() {}, hydrateConversationTokenUsage: async () => {},
        prefetchLastAssistantProcessDetails: async () => {}, renderChatWelcomeEmptyState() {},
    };
    context.window = {
        ChatHistory: { requestPage }, dispatchEvent() {},
        syncHitlConfigFromServer: () => new Promise(() => {}),
        restoreHitlInlineForConversation: () => new Promise(() => {}),
        CyberStrikeChatScroll: { forceScrollToBottom() {} },
    };
    vm.createContext(context);
    vm.runInContext(functionText('renderChatHistoryBatch', 'renderConversationHistoryMessage'), context);
    vm.runInContext(functionText('loadConversation', 'attachDeleteTurnButton'), context);
    const respond = (id, messages, status = 200) => pending.get(id)({ ok: status === 200, status, json: async () => ({ id, messages, messagePage: { hasMore: false } }) });
    return { context, rendered, states, respond };
}

test('history requests only a bounded page and encodes cursor/identity', async () => {
    const result = await requestPage(async (url, options) => {
        const parsed = new URL(url, 'https://fixture.invalid');
        assert.equal(parsed.pathname, '/api/conversations/a%2Fb');
        assert.equal(parsed.searchParams.get('message_limit'), String(PAGE_SIZE));
        assert.equal(parsed.searchParams.get('before_message_id'), 'older&id');
        assert.equal(options.signal.aborted, false);
        return { ok: true, json: async () => ({ id: 'a/b', messages: [] }) };
    }, 'a/b', { beforeMessageId: 'older&id' });
    assert.deepEqual(result.messages, []);
});

test('deadline covers hung authentication/fetch and hung JSON parsing', async () => {
    for (const fetcher of [() => new Promise(() => {}), async () => ({ ok: true, json: () => new Promise(() => {}) })]) {
        await assert.rejects(requestPage(fetcher, 'a', { timeoutMs: 15 }), { name: 'TimeoutError' });
    }
});

test('switch cancellation works even if the transport ignores abort', async () => {
    const controller = new AbortController();
    const pending = requestPage(() => new Promise(() => {}), 'old', { signal: controller.signal });
    controller.abort();
    await assert.rejects(pending, { name: 'AbortError' });
    let called = false;
    await assert.rejects(requestPage(() => { called = true; }, 'old', { signal: controller.signal }), { name: 'AbortError' });
    assert.equal(called, false);
});

test('access failures and malformed responses remain errors, never cached successes', async () => {
    for (const status of [401, 403, 404, 500]) {
        await assert.rejects(requestPage(async () => ({ ok: false, status }), 'a'), { status });
    }
    for (const data of [{ id: 'wrong' }, { id: 'a', messages: {} }, null]) {
        await assert.rejects(requestPage(async () => ({ ok: true, json: async () => data }), 'a'), /Invalid history/);
    }
});

test('rapid A/B switching ignores late A and does not wait for hung approval metadata', async () => {
    const runtime = historyRuntime();
    const first = runtime.context.loadConversation('a');
    const second = runtime.context.loadConversation('b');
    runtime.respond('b', [{ id: 'b-message', role: 'assistant', content: 'ready' }]);
    await second;
    runtime.respond('a', [{ id: 'old-message', role: 'assistant', content: 'stale' }]);
    await first;
    assert.deepEqual(runtime.rendered, ['b-message']);
    assert.equal(runtime.context.currentConversationId, 'b');
    assert.equal(runtime.context.loadConversationPendingId, '');
});

test('render failures in later chunks settle loading and allow a clean retry', async () => {
    const runtime = historyRuntime();
    runtime.context.renderConversationHistoryMessage = message => {
        if (message.id === 'broken') throw new Error('fixture renderer failure');
        runtime.rendered.push(message.id);
    };
    const first = runtime.context.loadConversation('a');
    runtime.respond('a', ['1', '2', '3', '4', 'broken'].map(id => ({ id, role: 'assistant', content: id })));
    await first;
    assert.equal(runtime.states.at(-1).failed, true);
    assert.equal(runtime.context.loadConversationPendingId, '');
    assert.equal(runtime.context.chatHistoryLoadFailedId, 'a');
    const retry = runtime.context.loadConversation('a');
    runtime.respond('a', [{ id: 'recovered', role: 'assistant', content: 'ok' }]);
    await retry;
    assert.deepEqual(runtime.rendered, ['recovered']);
    assert.equal(runtime.context.chatHistoryLoadFailedId, '');
});
