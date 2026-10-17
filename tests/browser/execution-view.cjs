// Real page module and styles, deterministic authorized API fixtures; no target/model traffic.
const { chromium } = require('playwright');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '../..');
(async () => {
    const browser = await chromium.launch({ headless: true, ...(process.env.EXECUTION_BROWSER_CHANNEL ? { channel: process.env.EXECUTION_BROWSER_CHANNEL } : {}) });
    try {
        const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
        const errors = []; page.on('pageerror', error => errors.push(error.message));
        const template = fs.readFileSync(root + '/web/templates/index.html', 'utf8');
        const markup = template.match(/<div id="page-execution-view"[^>]*><\/div>/)[0];
        await page.setContent('<html lang="zh-CN"><body><main style="height:100vh;display:flex;min-width:0">' + markup.replace('class="page"', 'class="page active"') + '</main></body></html>');
        for (const file of ['style.css', 'execution-view.css']) await page.addStyleTag({ path: root + '/web/static/css/' + file });
        await page.evaluate(strings => {
            window.t = key => strings[key.split('.').pop()] || key;
            window.allowed = true; window.hasPermission = () => allowed;
            window.navigateToConversation = id => { window.openedConversation = id; };
            window.activeReads = 0; window.peakReads = 0; window.calls = []; window.failReads = false;
            window.running = false; window.slowConversation = ''; window.responseStatus = 200;
            const timestamp = new Date('2026-10-17T04:00:00Z').toISOString();
            window.apiFetch = async (url, options) => {
                calls.push(url); activeReads++; peakReads = Math.max(peakReads, activeReads);
                try {
                    if (slowConversation && url.startsWith('/api/conversations/' + slowConversation + '?')) {
                        await new Promise((resolve, reject) => {
                            const timer = setTimeout(resolve, 300);
                            options.signal.addEventListener('abort', () => { clearTimeout(timer); reject(new DOMException('Aborted', 'AbortError')); }, { once: true });
                        });
                    }
                    if (failReads) throw new Error('private transport token');
                    if (responseStatus !== 200) return { ok: false, status: responseStatus };
                    const params = new URLSearchParams(url.split('?')[1]);
                    let value;
                    if (url.startsWith('/api/conversations?')) value = { conversations: [{ id: 'first', title: '授权站点评估 · 执行记录' }, { id: 'second', title: '第二会话' }], total: 2 };
                    else if (url.startsWith('/api/conversations/')) value = { messages: [{ id: url.includes('second') ? 'turn-second' : 'turn-first', role: 'assistant', createdAt: timestamp, content: 'SECRET-CONTENT', reasoningContent: 'SECRET-REASONING' }], messagePage: { hasMore: false } };
                    else if (params.has('summary')) value = { summary: { status: running ? 'running' : 'completed', iterationCount: 4, toolCount: 3, durationMs: 12500,
                        toolExecutions: [{ processDetailId: 'event-0', status: 'completed' }, { processDetailId: 'event-1', status: 'failed' }] } };
                    else {
                        const offset = Number(params.get('offset')); const length = offset ? 5 : 50;
                        value = { total: 55, processDetails: Array.from({ length }, (_, i) => ({ id: 'event-' + (offset + i), eventType: i < 2 ? 'tool_call' : i === 2 ? 'hitl_interrupt' : i % 4 === 0 ? 'iteration' : 'tool_result',
                            createdAt: timestamp, message: 'SECRET-MESSAGE', data: { toolName: i === 0 && !window.safeScreenshot ? '<img src=x onerror=alert(1)>' : 'query_assets', einoAgent: 'cyberstrike-eino-single', success: true, arguments: 'SECRET-ARGUMENT', resultPreview: 'SECRET-OUTPUT' } })) };
                    }
                    return { ok: true, json: async () => value };
                } finally { activeReads--; }
            };
        }, JSON.parse(fs.readFileSync(root + '/web/static/i18n/zh-CN.json', 'utf8')).executionView);
        await page.addScriptTag({ path: root + '/web/static/js/execution-view.js' });
        await page.evaluate(() => window.initExecutionView());
        await page.waitForFunction(() => document.querySelectorAll('.execution-event').length === 50);
        assert.equal(await page.locator('#execution-events img').count(), 0);
        assert.ok((await page.locator('#execution-events').innerText()).includes('<img src=x'));
        assert.ok(!(await page.locator('#page-execution-view').innerText()).includes('SECRET-'));
        assert.equal(await page.locator('.execution-event').first().locator('[data-status="completed"]').count(), 1);
        assert.equal(await page.evaluate(() => peakReads), 1);
        assert.equal(await page.evaluate(() => calls.length), 4, 'initial reads must not duplicate the turn');
        await page.locator('#execution-filter').selectOption('issues');
        assert.equal(await page.locator('.execution-event').count(), 1);
        await page.locator('#execution-filter').selectOption('all');
        await page.locator('#execution-next').click();
        await page.waitForFunction(() => document.querySelectorAll('.execution-event').length === 5);
        assert.equal(await page.locator('#execution-range').innerText(), '51–55 / 55');
        await page.locator('#execution-prev').click();
        await page.waitForFunction(() => document.querySelectorAll('.execution-event').length === 50);

        // An aborted older conversation cannot repaint after selecting a different one.
        await page.evaluate(() => { slowConversation = 'second'; });
        await page.locator('#execution-conversation').selectOption('second');
        await page.locator('#execution-conversation').selectOption('first');
        await page.waitForFunction(() => document.getElementById('execution-turn').value === 'turn-first');
        await page.waitForTimeout(350);
        assert.equal(await page.locator('#execution-turn').inputValue(), 'turn-first');
        assert.equal(await page.evaluate(() => activeReads), 0);

        const folder = process.env.EXECUTION_VIEW_SCREENSHOTS;
        await page.evaluate(() => { safeScreenshot = true; });
        await page.locator('#execution-refresh').click();
        await page.waitForFunction(() => !document.getElementById('execution-events').textContent.includes('<img'));
        if (folder) {
            fs.mkdirSync(folder, { recursive: true });
            for (const theme of ['light', 'dark']) {
                await page.evaluate(value => document.documentElement.dataset.theme = value, theme);
                await page.evaluate(() => document.getElementById('page-execution-view').scrollTop = 0);
                await page.screenshot({ path: path.join(folder, theme + '.png'), fullPage: false });
            }
        }
        await page.setViewportSize({ width: 375, height: 812 });
        await page.emulateMedia({ reducedMotion: 'reduce' });
        await page.addStyleTag({ content: '.execution-shell {font-size:20px}' });
        await page.evaluate(() => document.getElementById('page-execution-view').scrollTop = 0);
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'mobile page overflow');
        if (folder) await page.screenshot({ path: path.join(folder, 'mobile.png'), fullPage: false });
        await page.setViewportSize({ width: 1280, height: 720 });
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'desktop page overflow');

        // Speed up only the module's polling delays, then prove its three-failure circuit.
        await page.evaluate(() => {
            const original = window.setTimeout;
            window.setTimeout = (callback, delay, ...args) => original(callback, delay >= 10000 ? 20 : delay, ...args);
            running = true;
        });
        await page.locator('#execution-refresh').click();
        await page.waitForFunction(() => document.querySelector('#execution-stats [data-status="running"]'));
        await page.evaluate(() => { failReads = true; });
        await page.waitForFunction(() => document.getElementById('execution-notice').textContent.includes('连续三次'));
        const stoppedCount = await page.evaluate(() => calls.length);
        await page.waitForTimeout(100);
        assert.equal(await page.evaluate(() => calls.length), stoppedCount, 'polling continued after circuit opened');
        assert.equal(await page.locator('.execution-event').count(), 50, 'last successful snapshot disappeared');
        assert.ok(!(await page.locator('#execution-notice').innerText()).includes('private transport'));
        await page.evaluate(() => { failReads = false; running = false; });
        await page.locator('#execution-refresh').click();
        await page.waitForFunction(() => document.getElementById('execution-notice').textContent.startsWith('上次成功'));
        await page.evaluate(() => {
            Object.defineProperty(document, 'hidden', { configurable: true, value: true });
            document.dispatchEvent(new Event('visibilitychange'));
        });
        const hiddenCount = await page.evaluate(() => calls.length);
        await page.waitForTimeout(80);
        assert.equal(await page.evaluate(() => calls.length), hiddenCount, 'hidden tab continued polling');
        await page.evaluate(() => {
            Object.defineProperty(document, 'hidden', { configurable: true, value: false });
            document.dispatchEvent(new Event('visibilitychange'));
        });
        await page.waitForFunction(() => activeReads === 0);
        await page.evaluate(() => { responseStatus = 403; });
        await page.locator('#execution-refresh').click();
        await page.waitForFunction(() => document.getElementById('execution-notice').textContent.includes('无权'));
        assert.equal(await page.locator('.execution-event').count(), 0);
        await page.evaluate(() => window.stopExecutionView());
        assert.equal(await page.locator('#page-execution-view').innerHTML(), '', 'navigation retained prior account metadata');
        await page.evaluate(() => { allowed = false; window.initExecutionView(); });
        const before = await page.evaluate(() => calls.length);
        await page.waitForTimeout(50);
        assert.equal(await page.evaluate(() => calls.length), before, 'read-only permission denied user triggered API reads');
        assert.equal(errors.length, 0, errors.join('\n'));
        console.log('PASS: execution view paging, status reconciliation, safe text, cancellation, failure circuit, permissions and themes');
    } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
