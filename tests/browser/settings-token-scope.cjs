// Synthetic usage only: run with Playwright and Chromium available.
const { chromium } = require('playwright');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
(async () => {
    const browser = await chromium.launch({ headless: true, ...(process.env.PLAYWRIGHT_CHANNEL ? { channel: process.env.PLAYWRIGHT_CHANNEL } : {}) });
    try {
        const page = await browser.newPage({ viewport: { width: 1280, height: 720 } });
        const errors = [];
        page.on('pageerror', error => errors.push(error.message));
        const template = fs.readFileSync('web/templates/index.html', 'utf8');
        const start = template.indexOf('<div id="settings-section-token-usage"');
        const end = template.indexOf('<div id="settings-section-', start + 10);
        await page.setContent('<html><body>' + template.slice(start, end) + '</body></html>');
        await page.addStyleTag({ path: 'web/static/css/style.css' });
        await page.locator('#settings-section-token-usage').evaluate(node => node.classList.add('active'));
        await page.addScriptTag({ content: `
            window.requests = []; window.activeProject = 'empty-project';
            window.getActiveProjectId = () => activeProject;
            window.formatTokenUsageCompact = value => String(value);
            window.getModelDisplayName = value => value;
            window.apiFetch = async url => {
                requests.push(url);
                const total = url.includes('project_id=') ? 0 : 12345;
                return { ok: true, json: async () => ({summary:{events:total?1:0,totalTokens:total,modelCalls:total?1:0},byDay:[],byModel:[],byOrchestration:[],recent:[]}) };
            };
            window.translations = ${JSON.stringify(JSON.parse(fs.readFileSync('web/static/i18n/zh-CN.json', 'utf8')))};
            window.t = (key, opts) => {
                let value = key.split('.').reduce((node, part) => node && node[part], translations) || key;
                for (const [name, text] of Object.entries(opts || {})) value = value.replaceAll('{{'+name+'}}', text);
                return value;
            };
        ` });
        await page.addScriptTag({ path: 'web/static/js/token-usage.js' });
        await page.evaluate(() => refreshTokenUsage());
        assert.ok(!(await page.evaluate(() => requests.at(-1))).includes('project_id='));
        assert.match(await page.locator('[data-metric="usageTotal"]').innerText(), /12,345/);
        await page.selectOption('#settings-token-scope', 'current');
        await page.selectOption('#settings-token-days', '90');
        await page.waitForFunction(() => document.querySelector('.token-usage-empty')?.textContent.includes('90'));
        assert.match(await page.locator('.token-usage-empty').innerText(), /当前项目.*90/);
        await page.selectOption('#settings-token-scope', 'all');
        await page.waitForSelector('[data-metric="usageTotal"]');
        assert.match(await page.locator('[data-metric="usageTotal"]').innerText(), /12,345/);
        for (const theme of ['light', 'dark']) {
            await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme);
            if (process.env.TOKEN_USAGE_SCREENSHOTS) {
                fs.mkdirSync(process.env.TOKEN_USAGE_SCREENSHOTS, { recursive: true });
                await page.screenshot({ path: path.join(process.env.TOKEN_USAGE_SCREENSHOTS, 'settings-token-' + theme + '.png') });
            }
        }
        await page.evaluate(async () => { activeProject = ''; await refreshTokenUsage(); });
        assert.equal(await page.locator('#settings-token-scope').inputValue(), 'all');
        assert.ok(await page.locator('#settings-token-scope option[value="current"]').isDisabled());
        await page.evaluate(async () => {
            const legacy = document.createElement('span');
            legacy.id = 'settings-token-scope';
            document.getElementById(legacy.id).replaceWith(legacy);
            activeProject = 'empty-project';
            await refreshTokenUsage();
        });
        assert.equal(await page.locator('select#settings-token-scope').inputValue(), 'all');
        await page.selectOption('#settings-token-scope', 'current');
        await page.waitForFunction(() => document.querySelector('.token-usage-empty')?.textContent.includes('当前项目'));
        assert.deepEqual(errors, []);
        console.log('Settings token scope browser regression passed');
    } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
