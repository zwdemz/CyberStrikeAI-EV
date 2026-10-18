const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const crypto = require('node:crypto');
const root = path.resolve(__dirname, '../..');
const context = { window: {}, module: { exports: {} } };
vm.runInNewContext(fs.readFileSync(root + '/web/static/js/execution-swarm.js', 'utf8'), context);

test('swarm radial layout is deterministic, bounded and keeps Agent/tool identities separate', () => {
    const graph = { agents: ['same', 'two', 'three', 'four', 'five'], tools: ['same', 'b', 'c', 'd', 'e'] };
    const first = context.module.exports.layout(graph);
    assert.equal(JSON.stringify(first), JSON.stringify(context.module.exports.layout(graph)));
    assert.equal(first.length, 10);
    assert.equal(first.filter(node => node.name === 'same').length, 2);
    first.forEach(node => {
        assert.ok(node.x >= 100 && node.x <= 900 && node.y >= 40 && node.y <= 760);
        const radius = Math.hypot(node.x - 500, node.y - 400);
        assert.ok(Math.abs(radius - (node.kind === 'agent' ? 190 : 350)) < .001);
    });
    assert.equal(context.module.exports.layout({ agents: [], tools: [] }).length, 0);
});

test('vendored visualization files match pinned source manifest and retain licenses', () => {
    const folder = root + '/web/static/vendor/swarm/';
    const manifest = JSON.parse(fs.readFileSync(folder + 'manifest.json', 'utf8'));
    assert.equal(manifest.length, 5);
    manifest.forEach(item => {
        assert.match(item.source, /^https:\/\/registry\.npmjs\.org\//);
        assert.match(item.integrity, /^sha512-/);
        assert.equal(crypto.createHash('sha256').update(fs.readFileSync(folder + item.file)).digest('hex'), item.sha256);
    });
    assert.ok(fs.readFileSync(folder + 'THREE-LICENSE', 'utf8').includes('MIT License'));
    assert.ok(fs.readFileSync(folder + 'D3-LICENSE', 'utf8').includes('Copyright'));
});

test('Three UUIDs use Web Crypto entropy and fail without an entropy source', () => {
    const source = fs.readFileSync(root + '/web/static/vendor/swarm/three.core.js','utf8');
    const start = source.indexOf('function generateUUID() {');
    const end = source.indexOf('\n}',start) + 2;
    const lut = Array.from({length:256},(_,index) => index.toString(16).padStart(2,'0'));
    const fixture = vm.createContext({_lut:lut,crypto:crypto.webcrypto,Uint32Array});
    vm.runInContext(source.slice(start,end),fixture);
    const uuids = new Set(Array.from({length:128},() => vm.runInContext('generateUUID()',fixture)));
    assert.equal(uuids.size,128);
    for (const uuid of uuids) assert.match(uuid,/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    fixture.crypto=undefined;
    assert.throws(() => vm.runInContext('generateUUID()',fixture));
    const patched=JSON.parse(fs.readFileSync(root+'/web/static/vendor/swarm/manifest.json')).find(item=>item.file==='three.core.js');
    assert.match(patched.upstreamSha256,/^[0-9a-f]{64}$/);
    assert.ok(fs.existsSync(root+'/web/static/vendor/swarm/'+patched.patch));
});
