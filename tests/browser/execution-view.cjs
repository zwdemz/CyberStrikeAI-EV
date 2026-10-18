// Real page module and styles, deterministic authorized API fixtures; no target/model traffic.
const { chromium } = require('playwright');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '../..');
(async () => {
    const browser = await chromium.launch({ headless: true, args: ['--enable-unsafe-swiftshader'], ...(process.env.EXECUTION_BROWSER_CHANNEL ? { channel: process.env.EXECUTION_BROWSER_CHANNEL } : {}) });
    try {
        const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
        const errors = []; page.on('pageerror', error => errors.push(error.message));
        const template = fs.readFileSync(root + '/web/templates/index.html', 'utf8');
        const markup = template.match(/<div id="page-execution-view"[^>]*><\/div>/)[0];
        await page.route('http://execution.test/**', route => {
            const url = new URL(route.request().url());
            if (url.pathname.startsWith('/static/vendor/swarm/')) {
                const name = path.basename(url.pathname);
                return route.fulfill({ contentType: 'application/javascript', body: fs.readFileSync(root + '/web/static/vendor/swarm/' + name) });
            }
            return route.fulfill({ contentType: 'text/html', body: '<html lang="zh-CN"><body><main style="height:100vh;display:flex;min-width:0">' + markup.replace('class="page"', 'class="page active"') + '</main></body></html>' });
        });
        await page.goto('http://execution.test/');
        for (const file of ['style.css', 'execution-view.css']) await page.addStyleTag({ path: root + '/web/static/css/' + file });
        await page.evaluate(strings => {
            window.t = key => strings[key.split('.').pop()] || key;
            window.allowed = true; window.hasPermission = () => allowed;
            window.navigateToConversation = id => { window.openedConversation = id; };
            window.activeReads = 0; window.peakReads = 0; window.calls = []; window.failReads = false;
            window.running = false; window.slowConversation = ''; window.responseStatus = 200;
            window.evidenceLinks = []; window.partialBoard = false; window.latestTurn = '';
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
                    else if (url.includes('/evidence-sources')) value = {items:[{sourceId:'fact-fixture',label:'人工核对的项目事实'}]};
                    else if (url.includes('/evidence')) {
                        if (options.method === 'POST') {
                            const body = JSON.parse(options.body);
                            evidenceLinks = [{id:'link-1',kind:body.kind,sourceId:body.sourceId,label:'已关联的执行结果',messageId:'turn-first'}]; value = {id:'link-1'};
                        } else if (options.method === 'DELETE') { evidenceLinks = []; value = {removed:true}; }
                        else value = { items:evidenceLinks };
                    }
                    else if (url.includes('/plan-tasks?')) value = { total: 3, running, boundaryKnown:true, incomplete:partialBoard, tasks: partialBoard ? [] : [
                        { id: '1', identity:'task-one', subject: '核对授权范围与已有事实', description: '只读取已授权项目的任务记录。', status: 'pending', blockedBy: ['2'] },
                        { id: '2', subject: '整理资产与工具执行结果', description: '汇总已记录的动作与结果，不重复执行。', status: window.taskFinished ? 'completed' : 'in_progress', owner: 'cyberstrike-eino-single' },
                        { id: '3', subject: '核对证据与实际影响', description: '完成执行后，等待人工独立核对证据。', status: 'completed' },
                        { id: '4', subject: '已删除的计划', status: 'deleted' }
                    ] };
                    else if (url.startsWith('/api/conversations/')) value = { projectId:'project-fixture', messages: [{ id: url.includes('second') ? 'turn-second' : latestTurn || 'turn-first', role: 'assistant', createdAt: timestamp, content: 'SECRET-CONTENT', reasoningContent: 'SECRET-REASONING' }], messagePage: { hasMore: false } };
                    else if (params.has('summary')) value = { summary: { status: running ? 'running' : 'completed', iterationCount: 4, toolCount: 3, durationMs: 12500,
                        toolExecutions: [{ processDetailId: 'event-0', status: 'completed' }, { processDetailId: 'event-1', status: 'failed' }] } };
                    else {
                        const offset = params.get('anchorId') === 'event-52' ? 50 : Number(params.get('offset')); const length = offset ? 5 : 50;
                        value = { total: 55, offset, processDetails: Array.from({ length }, (_, i) => ({ id: 'event-' + (offset + i), eventType: i < 2 ? 'tool_call' : i === 2 ? 'hitl_interrupt' : i % 4 === 0 ? 'iteration' : 'tool_result',
                            createdAt: timestamp, message: 'SECRET-MESSAGE', data: { toolName: i === 0 && !window.safeScreenshot ? '<img src=x onerror=alert(1)>' : i < 2 ? 'query_assets' : ['query_assets', 'search_knowledge_base', 'get_project_fact', 'http-framework-test'][i % 4], einoAgent: i < 2 ? 'cyberstrike-eino-single' : ['cyberstrike-eino-single', 'recon-agent', 'evidence-agent'][i % 3], success: true, arguments: 'SECRET-ARGUMENT', resultPreview: 'SECRET-OUTPUT' } })) };
                    }
                    return { ok: true, json: async () => value };
                } finally { activeReads--; }
            };
        }, JSON.parse(fs.readFileSync(root + '/web/static/i18n/zh-CN.json', 'utf8')).executionView);
        await page.addScriptTag({ path: root + '/web/static/js/execution-swarm.js' });
        await page.addScriptTag({ path: root + '/web/static/js/execution-evidence.js' });
        await page.addScriptTag({ path: root + '/web/static/js/execution-view.js' });
        await page.evaluate(() => window.initExecutionView());
        await page.waitForFunction(() => document.querySelectorAll('.execution-event').length === 50);
        assert.equal(await page.locator('#execution-board').isVisible(), true);
        assert.equal(await page.locator('#execution-swarm-toggle').getAttribute('open'), null);
        assert.equal(await page.locator('.execution-swarm-canvas').count(), 0, 'collapsed swarm allocated GPU');
        assert.ok((await page.locator('#execution-issues').innerText()).includes('异常'));
        await page.locator('#execution-swarm-toggle > summary').click();
        await page.waitForFunction(() => !document.getElementById('execution-replay').disabled);
        assert.equal(await page.locator('.execution-swarm-canvas').count(), 1, 'actual WebGL layer unavailable');
        const staticCanvas = await page.locator('.execution-swarm-canvas').screenshot();
        await page.locator('#execution-replay').click();
        assert.equal(await page.locator('#execution-replay').getAttribute('aria-pressed'), 'true');
        await page.waitForTimeout(500);
        assert.notDeepEqual(await page.locator('.execution-swarm-canvas').screenshot(), staticCanvas, 'recorded-event particles did not render');
        await page.locator('#execution-replay').click();
        assert.equal(await page.locator('#execution-replay').getAttribute('aria-pressed'), 'false');
        await page.evaluate(() => {
            window.savedAnimationFrame = requestAnimationFrame;
            let simulatedTime = 0;
            window.requestAnimationFrame = callback => savedAnimationFrame(() => callback(simulatedTime += 4000));
        });
        await page.locator('#execution-replay').click();
        await page.waitForFunction(() => document.getElementById('execution-replay').getAttribute('aria-pressed') === 'false');
        await page.evaluate(() => { window.requestAnimationFrame = savedAnimationFrame; });
        assert.equal(await page.locator('#execution-graph img').count(), 0);
        await page.locator('#execution-graph button[data-kind="agent"]').first().click();
        assert.equal(await page.locator('#execution-feed-title').innerText(), 'cyberstrike-eino-single');
        await page.locator('#execution-feed-reset').click();
        assert.equal(await page.locator('[data-lane="plan"] .execution-task-card').count(), 1);
        assert.equal(await page.locator('[data-lane="execute"] .execution-task-card').count(), 1);
        await page.locator('[data-task="1"] summary').click();
        await page.locator('[data-evidence-task="1"]').click();
        await page.waitForFunction(() => !document.querySelector('[data-evidence="save"]').disabled);
        await page.locator('[data-evidence="save"]').click();
        await page.waitForFunction(() => document.querySelector('[data-evidence="items"]').textContent.includes('已关联'));
        assert.equal(await page.evaluate(() => evidenceLinks.length), 1);
        if (process.env.EXECUTION_VIEW_SCREENSHOTS) {
            fs.mkdirSync(process.env.EXECUTION_VIEW_SCREENSHOTS,{recursive:true});
            await page.locator('#execution-evidence').screenshot({path:path.join(process.env.EXECUTION_VIEW_SCREENSHOTS,'evidence.png')});
        }
        await page.locator('[data-evidence="kind"]').selectOption('project_fact');
        await page.waitForFunction(() => document.querySelector('[data-evidence="source"]').value === 'fact-fixture');
        assert.equal(await page.locator('[data-evidence="source"]').innerText(), '人工核对的项目事实');
        await page.locator('[data-evidence="search"]').fill('人工');
        await page.locator('[data-evidence="search"]').press('Enter');
        await page.waitForFunction(() => calls.some(url => url.includes('/evidence-sources?') && url.includes('search=%E4%BA%BA%E5%B7%A5')));
        assert.equal(await page.evaluate(() => evidenceLinks[0].kind), 'tool_result', 'search Enter linked an unintended source');
        await page.locator('[data-evidence="items"] [data-remove]').click();
        await page.waitForFunction(() => document.querySelector('[data-evidence="items"]').textContent.includes('暂无'));
        await page.locator('[data-evidence="close"]').click();
        await page.evaluate(() => { partialBoard = true; });
        await page.locator('#execution-board-refresh').click();
        await page.waitForFunction(() => document.getElementById('execution-board-runtime').textContent.includes('正在更新'));
        assert.equal(await page.locator('.execution-task-card').count(), 3, 'partial write erased last complete board');
        assert.equal(await page.locator('[data-task="1"]').getAttribute('open'), '');
        await page.evaluate(() => { partialBoard = false; });
        await page.locator('#execution-board-refresh').click();
        assert.equal(await page.locator('[data-lane="verify"] .execution-task-card').count(), 1);
        assert.ok((await page.locator('[data-lane="verify"]').innerText()).includes('待核验'));
        assert.ok((await page.locator('[data-lane="execute"]').innerText()).includes('当前已停止'));
        await page.evaluate(() => { taskFinished = true; });
        await page.locator('#execution-board-refresh').click();
        await page.waitForFunction(() => document.querySelectorAll('[data-lane="verify"] .execution-task-card').length === 2);
        assert.equal(await page.locator('[data-lane="execute"] .execution-task-card').count(), 0);
        await page.evaluate(() => { taskFinished = false; });
        await page.locator('#execution-board-refresh').click();
        await page.waitForFunction(() => document.querySelectorAll('[data-lane="execute"] .execution-task-card').length === 1);
        await page.locator('#execution-tab-timeline').click();
        assert.equal(await page.locator('.execution-swarm-canvas').count(), 0, 'hidden board retained GPU canvas');
        assert.equal(await page.locator('#execution-events img').count(), 0);
        assert.ok((await page.locator('#execution-events').innerText()).includes('<img src=x'));
        assert.ok(!(await page.locator('#page-execution-view').innerText()).includes('SECRET-'));
        assert.equal(await page.locator('.execution-event').first().locator('[data-status="completed"]').count(), 1);
        assert.equal(await page.evaluate(() => peakReads), 1);
        assert.equal(await page.evaluate(() => calls.filter(url => url.startsWith('/api/conversations?')).length), 1, 'board refresh duplicated conversation discovery');
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
            await page.setViewportSize({ width: 1440, height: 1600 });
            await page.locator('#execution-tab-board').click();
            if (await page.locator('#execution-swarm-toggle').getAttribute('open') !== null) await page.locator('#execution-swarm-toggle > summary').click();
            for (const theme of ['light', 'dark']) {
                await page.locator('#execution-tab-board').click();
                await page.evaluate(value => document.documentElement.dataset.theme = value, theme);
                await page.evaluate(() => document.getElementById('page-execution-view').scrollTop = 0);
                await page.locator('#execution-board').screenshot({ path: path.join(folder, theme + '.png') });
            }
            await page.locator('#execution-swarm-toggle > summary').click();
        }
        await page.setViewportSize({ width: 375, height: 812 });
        await page.locator('#execution-tab-board').click();
        await page.emulateMedia({ reducedMotion: 'reduce' });
        await page.waitForFunction(() => document.getElementById('execution-replay').disabled);
        assert.equal(await page.locator('#execution-replay').isDisabled(), true);
        assert.equal(await page.locator('#execution-replay').getAttribute('aria-pressed'), 'false');
        await page.addStyleTag({ content: '.execution-shell {font-size:20px}' });
        await page.evaluate(() => document.getElementById('page-execution-view').scrollTop = 0);
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'mobile page overflow');
        const overlap = await page.locator('#execution-graph button').evaluateAll(buttons => {
            const boxes = buttons.map(button => button.getBoundingClientRect());
            return boxes.some((box, index) => boxes.slice(index + 1).some(other => box.left < other.right && box.right > other.left && box.top < other.bottom && box.bottom > other.top));
        });
        assert.equal(overlap, false, 'mobile graph nodes overlap');
        if (folder) await page.screenshot({ path: path.join(folder, 'mobile.png'), fullPage: false });
        await page.locator('#execution-tab-timeline').click();
        await page.setViewportSize({ width: 1280, height: 720 });
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'desktop page overflow');

        // Speed up only the module's polling delays, then prove its three-failure circuit.
        await page.evaluate(() => {
            const original = window.setTimeout;
            window.setTimeout = (callback, delay, ...args) => original(callback, delay >= 10000 ? 20 : delay, ...args);
            running = false;
        });
        await page.locator('#execution-refresh').click();
        await page.waitForFunction(() => !document.getElementById('execution-content').matches('[aria-busy="true"]'));
        await page.evaluate(() => { latestTurn = 'turn-after-idle'; });
        await page.waitForFunction(() => document.getElementById('execution-turn').value === 'turn-after-idle');
        await page.evaluate(() => { running = true; });
        await page.locator('#execution-refresh').click();
        await page.waitForFunction(() => document.querySelector('#execution-stats [data-status="running"]'));
        await page.evaluate(() => { latestTurn = 'turn-new-run'; });
        await page.waitForFunction(() => document.getElementById('execution-turn').value === 'turn-new-run');
        await page.locator('#execution-turn').selectOption('turn-new-run');
        await page.evaluate(() => { latestTurn = 'turn-later'; });
        await page.waitForTimeout(100);
        assert.equal(await page.locator('#execution-turn').inputValue(), 'turn-new-run', 'polling replaced deliberately selected history');
        await page.locator('#execution-latest-turns').click();
        await page.waitForFunction(() => document.getElementById('execution-turn').value === 'turn-later');
        await page.locator('#execution-turn').selectOption('');
        await page.waitForFunction(() => !document.getElementById('execution-content').matches('[aria-busy="true"]'));
        const boardReads = await page.evaluate(() => calls.filter(url => url.includes('/plan-tasks?')).length);
        await page.waitForFunction(before => calls.filter(url => url.includes('/plan-tasks?')).length > before,boardReads);
        assert.equal(await page.locator('#execution-turn').inputValue(),'','cleared turn did not retain board-only mode');
        await page.locator('#execution-latest-turns').click();
        await page.waitForFunction(() => document.getElementById('execution-turn').value === 'turn-later');
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
        await page.emulateMedia({ reducedMotion: 'no-preference' });
        const fallback = await page.evaluate(async () => {
            const host = document.createElement('div'); document.body.append(host);
            const original = HTMLCanvasElement.prototype.getContext;
            HTMLCanvasElement.prototype.getContext = function (type, ...args) {
                return type.startsWith('webgl') ? null : original.call(this, type, ...args);
            };
            let ready = true;
            let dispose;
            try {
                dispose = ExecutionSwarm.mount(host, { agents: ['fallback-agent'], tools: ['fallback-tool'], links: [{ agent: 'fallback-agent', tool: 'fallback-tool', events: [{ type: 'tool_call' }] }] }, {
                    text: key => key, onReplay() {}, setReplay() {}, onReady: value => { ready = value; }
                });
                await new Promise(resolve => setTimeout(resolve, 50));
                return { ready, nodes: host.querySelectorAll('button').length, canvases: host.querySelectorAll('canvas').length };
            } finally { if (dispose) dispose(); host.remove(); HTMLCanvasElement.prototype.getContext = original; }
        });
        assert.deepEqual(fallback, { ready: false, nodes: 2, canvases: 0 }, 'WebGL failure lost accessible graph fallback');
        // Exercise the real auth refresh hook after logout clears this unchanged route.
        await page.evaluate(()=>{window.executionFixtureFetch=window.apiFetch;});
        await page.addScriptTag({path:root+'/web/static/js/auth.js'});
        await page.evaluate(async () => {
            window.apiFetch=window.executionFixtureFetch;
            window.loadConversations=async()=>{};window.loadActiveTasks=async()=>{};
            window.renderUserMenuProfile=()=>{};window.applyRBACToUI=()=>{};
            window.refreshChatProjectSelector=undefined;
            window.stopExecutionView();clearAuthStorage();await refreshAppData();
        });
        assert.equal(await page.locator('#page-execution-view').innerHTML(),'','unauthenticated refresh mounted private view');
        await page.evaluate(async () => {
            responseStatus=200;
            authToken='fixture';authTokenExpiry=new Date(Date.now()+60000);authPermissions=new Set(['chat:read']);
            await refreshAppData();
        });
        await page.waitForFunction(() => document.querySelectorAll('.execution-event').length===50);
        await page.evaluate(() => window.stopExecutionView());
        assert.equal(errors.length, 0, errors.join('\n'));
        console.log('PASS: execution view paging, status reconciliation, safe text, cancellation, failure circuit, permissions and themes');
    } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
