'use strict';

const {test} = require('node:test');
const assert = require('node:assert/strict');
const {evaluateRoute, publishRoutes} = require('../../.github/scripts/pr-route.cjs');
const REPOSITORY_ID = 42;
const HEAD_SHA = 'a'.repeat(40);

/** Build mock API metadata; all values are local fixtures. */
function pull(base, head, headRepositoryId = REPOSITORY_ID, sha = HEAD_SHA) {
  return {base: {ref: base, repo: {id: REPOSITORY_ID}},
    head: {ref: head, repo: {id: headRepositoryId}, sha}};
}

/** Build an isolated publisher fixture, recording checks without network access. */
function fixture(pulls) {
  const checks = [];
  return {checks, dependencies: {
    github: {paginate: async () => pulls, rest: {
      pulls: {list: () => {}}, repos: {createCommitStatus: async check => checks.push(check)},
    }},
    context: {eventName: 'workflow_run', repo: {owner: 'fixture', repo: 'project'},
      payload: {repository: {id: REPOSITORY_ID},
        workflow_run: {event: 'pull_request', head_sha: HEAD_SHA}}},
    core: {info: () => {}},
  }};
}

test('work branches including codex target dev', () => {
  for (const branch of ['codex/task', 'feature/task', 'fix/task', 'docs/task']) {
    assert.equal(evaluateRoute(pull('dev', branch), REPOSITORY_ID).allowed, true);
    assert.equal(evaluateRoute(pull('main', branch), REPOSITORY_ID).allowed, false);
  }
});

test('only the receiving repository dev can release to main', () => {
  assert.equal(evaluateRoute(pull('main', 'dev'), REPOSITORY_ID).allowed, true);
  for (const branch of ['dev', 'main', 'codex/task']) {
    assert.equal(evaluateRoute(pull('main', branch, 43), REPOSITORY_ID).allowed, false);
  }
});

test('fork contributions can target dev; protected branches cannot be work branches', () => {
  assert.equal(evaluateRoute(pull('dev', 'main', 43), REPOSITORY_ID).allowed, true);
  for (const branch of ['dev', 'main']) {
    assert.equal(evaluateRoute(pull('dev', branch), REPOSITORY_ID).allowed, false);
  }
});

test('unknown targets and incomplete or spoofed metadata fail closed', () => {
  for (const request of [null, {}, pull('release', 'codex/task'),
    {...pull('main', 'dev'), head: {ref: 'dev', repo: null}},
    {...pull('main', 'dev'), base: {ref: 'main', repo: {id: 43}}},
    pull('main', '', 42), pull('main', 'dev', -1)]) {
    assert.equal(evaluateRoute(request, REPOSITORY_ID).allowed, false);
  }
  assert.equal(evaluateRoute(pull('main', 'dev'), null).allowed, false);
});

test('publisher posts target-specific checks on the PR SHA', async () => {
  const {checks, dependencies} = fixture([pull('dev', 'codex/task')]);
  assert.equal(await publishRoutes(dependencies), 1);
  assert.equal(checks[0].context, 'pr-route/dev');
  assert.equal(checks[0].sha, HEAD_SHA);
  assert.equal(checks[0].state, 'success');
});

test('same-SHA valid release cannot mask an invalid main PR', async () => {
  const {checks, dependencies} = fixture([pull('main', 'dev'), pull('main', 'codex/task')]);
  await publishRoutes(dependencies);
  assert.equal(checks.length, 1);
  assert.equal(checks[0].context, 'pr-route/main');
  assert.equal(checks[0].state, 'failure');
});

test('retargeting is evaluated with live metadata and independent target contexts', async () => {
  const {checks, dependencies} = fixture([pull('main', 'codex/task'), pull('dev', 'codex/task')]);
  await publishRoutes(dependencies);
  assert.deepEqual(checks.map(check => [check.context, check.state]),
    [['pr-route/main', 'failure'], ['pr-route/dev', 'success']]);
});

test('invalid destination is reported without echoing user-supplied text', async () => {
  const {checks, dependencies} = fixture([pull('untrusted-text', 'codex/task')]);
  await publishRoutes(dependencies);
  assert.equal(checks[0].context, 'pr-route/invalid');
  assert.equal(checks[0].state, 'failure');
  assert.equal(JSON.stringify(checks).includes('untrusted-text'), false);
});

test('stale and closed PRs do not receive new passing checks', async () => {
  for (const pulls of [[], [pull('main', 'dev', 42, 'b'.repeat(40))]]) {
    const {checks, dependencies} = fixture(pulls);
    assert.equal(await publishRoutes(dependencies), 0);
    assert.equal(checks.length, 0);
  }
});

test('unexpected events and API failures cannot produce a passing check', async () => {
  const {checks, dependencies} = fixture([pull('main', 'dev')]);
  dependencies.context.eventName = 'pull_request';
  await assert.rejects(publishRoutes(dependencies));
  dependencies.context.eventName = 'workflow_run';
  dependencies.github.paginate = async () => { throw new Error('fixture API failure'); };
  await assert.rejects(publishRoutes(dependencies));
  assert.equal(checks.length, 0);
});
