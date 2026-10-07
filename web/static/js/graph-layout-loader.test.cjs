const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync('web/static/js/graph-layout-loader.js', 'utf8');

test('graph engine is deferred, concurrent loads coalesce and failure allows retry', async () => {
    const scripts = [];
    let timeout;
    const context = {
        window: {},
        document: { createElement: () => ({ remove() {} }), head: { appendChild: script => scripts.push(script) } },
        setTimeout: callback => { timeout = callback; return 1; }, clearTimeout() {},
    };
    vm.runInNewContext(source, context);
    assert.equal(scripts.length, 0);
    const first = context.window.ensureGraphLayoutLibrary();
    assert.equal(first, context.window.ensureGraphLayoutLibrary());
    scripts[0].onerror();
    assert.equal(await first, false);
    const second = context.window.ensureGraphLayoutLibrary();
    assert.equal(scripts.length, 2);
    timeout();
    assert.equal(await second, false);
    const third = context.window.ensureGraphLayoutLibrary();
    context.window.ELK = function () {};
    scripts[2].onload();
    assert.equal(await third, true);
    assert.equal(await context.window.ensureGraphLayoutLibrary(), true);
    assert.equal(scripts.length, 3);
});
