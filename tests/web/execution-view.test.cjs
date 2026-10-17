const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = path.resolve(__dirname, '../..');
const source = fs.readFileSync(root + '/web/static/js/execution-view.js', 'utf8');
const context = { module: { exports: {} }, window: { t: key => key }, document: { addEventListener() {} } };
vm.runInNewContext(source, context);
const { describe, status, count, boardTask, topology } = context.module.exports;

test('dispatch topology is bounded and never invents Agent-tool relationships', () => {
    const details = [{ eventType: 'tool_call', data: { toolName: 'unattributed' } },
        ...Array.from({ length: 9 }, (_, index) => ({ eventType: 'tool_call', data: { einoAgent: 'Agent ' + index, toolName: 'Tool ' + index, arguments: 'private' } }))];
    const graph = topology(details);
    assert.equal(graph.agents.length, 5); assert.equal(graph.tools.length, 5);
    assert.equal(graph.links.some(link => link.tool === 'unattributed'), false);
    assert.ok(graph.omitted > 0);
    assert.doesNotMatch(JSON.stringify(graph), /private/);
    assert.equal(topology([{ eventType: 'iteration', data: {} }]).links.length, 0);
});

test('board lanes reflect recorded task states without inventing verification', () => {
    assert.equal(boardTask({ status: 'pending' }).lane, 'plan');
    assert.equal(boardTask({ status: 'in_progress' }).lane, 'execute');
    const completed = boardTask({ status: 'completed', subject: 'Reported done' });
    assert.equal(completed.lane, 'verify'); assert.equal('verified' in completed, false);
    assert.equal(boardTask({ status: 'made-up' }).status, 'unknown');
    assert.equal(boardTask(null).lane, 'plan');
    assert.equal(boardTask({ description: 'x'.repeat(900) }).description.length, 600);
});

test('execution projection reconciles off-page results without exposing reasoning or payloads', () => {
    const detail = { id: 'call', eventType: 'tool_call', message: 'private-message', data: {
        toolName: 'query_assets', arguments: 'private-argument', resultPreview: 'private-output', reasoning: 'private-reasoning'
    } };
    const view = describe(detail, [{ processDetailId: 'call', status: 'completed' }]);
    assert.equal(view.outcome, 'completed');
    assert.equal(view.title, 'query_assets');
    assert.doesNotMatch(JSON.stringify(view), /private-/);
});
test('unrecorded tool result status is unknown and failure filters include tool failures', () => {
    assert.equal(describe({ eventType: 'tool_result', data: {} }, []).outcome, 'unknown');
    assert.equal(describe({ id: 'x', eventType: 'tool_call' }, [{ processDetailId: 'x', status: 'failed' }]).issue, true);
    assert.equal(describe({ eventType: 'hitl_interrupt' }, []).outcome, 'pending');
    assert.equal(describe({ eventType: 'hitl_rejected' }, []).outcome, 'blocked');
});
test('status, numeric and metadata boundaries tolerate invalid history', () => {
    assert.equal(status('<script>'), 'unknown');
    assert.equal(count(Infinity), 0); assert.equal(count(-9), 0);
    assert.equal(describe({ eventType: 'tool_call', data: { toolName: 'x'.repeat(1000) } }, []).title.length, 100);
});
test('execution page navigation and RBAC reuse conversation read permission', () => {
    const router = fs.readFileSync(root + '/web/static/js/router.js', 'utf8');
    const auth = fs.readFileSync(root + '/web/static/js/auth.js', 'utf8');
    assert.match(auth, /'execution-view': 'chat:read'/);
    assert.equal((router.match(/'dashboard', 'chat', 'execution-view'/g) || []).length, 2);
    assert.match(router, /stopExecutionView/);
    const html = fs.readFileSync(root + '/web/templates/index.html', 'utf8');
    assert.match(html, /id="page-execution-view" class="page" data-require-permission="chat:read"/);
});
