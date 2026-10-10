// Run with Playwright installed. All usage data and requests are synthetic.
const {chromium} = require('playwright');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '../..');
(async () => {
    const browser = await chromium.launch({headless: true, args: ['--no-sandbox']});
    try {
        const page = await browser.newPage({viewport: {width: 1280, height: 720}});
        const errors = []; page.on('pageerror', error => errors.push(error.message));
        const template = fs.readFileSync(path.join(root, 'web/templates/index.html'), 'utf8');
        const label = template.indexOf('data-i18n="dashboard.tokenUsage"');
        const start = template.lastIndexOf('<div class="dashboard-kpi-card"', label);
        const card = template.slice(start, template.indexOf('<!-- 两列主内容区 -->', label));
        assert.ok(card.includes('onclick="openDashboardTokenUsage()"'));
        assert.ok(!card.includes("switchPage('chat')"));
        await page.setContent('<html lang="zh-CN"><body>' + card + '</body></html>');
        await page.addStyleTag({path: path.join(root, 'web/static/css/style.css')});
        await page.addScriptTag({content: `
            window.mode = 'success'; window.requests=[];
            window.dashboardProjectScopedUrl = url => url + '&project_id=fixture%26project';
            window.apiFetch = async (url, options) => {
                requests.push(url);
                if(mode==='denied') return {ok:false,status:403};
                if(mode==='race' && url.includes('days=30')) await new Promise(resolve=>setTimeout(resolve,150));
                const total=mode==='empty'?0:(url.includes('days=30')?999:12345);
                return {ok:true,json:async()=>({summary:{events:total?1:0,totalTokens:total,promptTokens:10000,completionTokens:2345,cachedTokens:200,reasoningTokens:100,modelCalls:total?3:0},byDay:total?[{key:'2026-10-10',totalTokens:total,modelCalls:3,promptTokens:10000,completionTokens:2345}]:[],byModel:total?[{label:'<img src=x onerror=alert(1)>',totalTokens:total,modelCalls:3,promptTokens:10000,completionTokens:2345}]:[]})};
            };`});
        const script = fs.readFileSync(path.join(root, 'web/static/js/dashboard.js'), 'utf8');
        await page.addScriptTag({content: script.slice(script.indexOf('function openDashboardTokenUsage()'), script.indexOf('// sessionStorage：'))});
        await page.locator('.dashboard-kpi-card').focus(); await page.keyboard.press('Enter');
        await page.waitForSelector('.dashboard-token-metrics');
        assert.equal(await page.locator('.dashboard-token-metrics dd').first().innerText(), '12,345');
        assert.equal(await page.locator('#dashboard-token-dialog img').count(), 0);
        await page.selectOption('#dashboard-token-scope', 'all');
        await page.waitForFunction(()=>requests.at(-1).includes('days=7') && !requests.at(-1).includes('project_id='));
        await page.selectOption('#dashboard-token-scope', 'current');
        await page.waitForFunction(()=>requests.at(-1).includes('project_id=fixture%26project'));

        assert.ok((await page.evaluate(()=>requests[0])).includes('project_id=fixture%26project'));
        for (const theme of ['light','dark']) {
            await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);
            if(process.env.TOKEN_USAGE_SCREENSHOTS) {
                fs.mkdirSync(process.env.TOKEN_USAGE_SCREENSHOTS,{recursive:true});
                await page.screenshot({path:path.join(process.env.TOKEN_USAGE_SCREENSHOTS,'token-'+theme+'.png')});
            }
        }
        await page.evaluate(()=>mode='race');
        await page.locator('#dashboard-token-range').selectOption('30');
        await page.locator('#dashboard-token-range').selectOption('90');
        await page.waitForTimeout(250);
        assert.equal(await page.locator('.dashboard-token-metrics dd').first().innerText(), '12,345');
        await page.evaluate(()=>mode='denied'); await page.getByRole('button',{name:'刷新',exact:true}).click();
        await page.getByRole('status').filter({hasText:'没有查看用量的权限'}).waitFor();
        assert.equal(await page.locator('.dashboard-token-metrics').count(),0);
        await page.evaluate(()=>mode='empty'); await page.getByRole('button',{name:'刷新',exact:true}).click();
        await page.getByRole('status').filter({hasText:'暂无已记录'}).waitFor();
        await page.keyboard.press('Escape'); await page.locator('#dashboard-token-dialog').waitFor({state:'detached'});
        assert.deepEqual(errors,[]);
        console.log('PASS: keyboard entry, statistics, project scope, themes, safe text, latest-response wins, permission error, empty state and Escape');
    } finally { await browser.close(); }
})().catch(error=>{console.error(error);process.exitCode=1;});
