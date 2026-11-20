const { chromium } = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

// Real browser layout is required: DOM mocks cannot detect intrinsic-size jumps.
(async () => {
    const browser = await chromium.launch({ headless: true, args: ['--no-sandbox'] });
    try {
        const page = await browser.newPage({ viewport: { width: 1280, height: 720 } });
        const root = path.resolve(__dirname, '../..');
        await page.setContent('<div id="chat-messages"></div>');
        for (const file of ['style.css', 'chat-history.css']) {
            await page.addStyleTag({ content: fs.readFileSync(path.join(root, 'web/static/css', file), 'utf8') });
        }
        await page.addStyleTag({ content: '#chat-messages{position:absolute;inset:0;height:600px;overflow:auto;display:block} .message{display:block} .progress-timeline{max-height:none!important;overflow:visible!important}' });
        await page.evaluate(() => {
            const box = document.getElementById('chat-messages');
            for (let index = 0; index < 40; index++) {
                const message = document.createElement('div');
                message.className = 'message assistant assistant-turn-with-process';
                message.innerHTML = '<div class="progress-timeline">' + Array.from({ length: 5 }, (_, item) => '<div class="timeline-item"><pre>' + (`message ${index}, detail ${item}\n`).repeat(15 + index % 8) + '</pre></div>').join('') + '</div>';
                box.appendChild(message);
            }
        });
        await page.waitForTimeout(100);
        const initialHeight = await page.locator('#chat-messages').evaluate(box => box.scrollHeight);
        for (const fraction of [0.8, 0.4, 1, 0.2, 0]) {
            await page.locator('#chat-messages').evaluate((box, fraction) => { box.scrollTop = (box.scrollHeight - box.clientHeight) * fraction; }, fraction);
            await page.waitForTimeout(100);
            assert.equal(await page.locator('#chat-messages').evaluate(box => box.scrollHeight), initialHeight, 'Reading must not replace placeholder heights and move content');
        }
        await page.mouse.move(500, 300);
        for (let index = 0; index < 20; index++) {
            const before = await page.locator('#chat-messages').evaluate(box => box.scrollTop);
            await page.mouse.wheel(0, 120);
            await page.waitForTimeout(60);
            const after = await page.locator('#chat-messages').evaluate(box => box.scrollTop);
            assert.ok(Math.abs(after - before - 120) <= 2, 'Wheel reading skipped content');
        }
        console.log('PASS: variable-height messages and nested tool details retain stable layout and wheel position');
    } finally {
        await browser.close();
    }
})().catch(error => { console.error(error); process.exitCode = 1; });
