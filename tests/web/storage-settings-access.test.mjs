import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';

const source = fs.readFileSync('web/static/js/settings.js', 'utf8');
const sectionSource = source.slice(source.indexOf('function settingsSectionPermission('), source.indexOf('async function openSettings('));
const openSource = source.slice(source.indexOf('async function openSettings('), source.indexOf('function closeSettings('));

test('storage-only settings opens storage without requesting global config', async () => {
  const calls = [];
  const element = () => {
    const active = new Set();
    return {
      active,
      classList: { add: name => active.add(name), remove: name => active.delete(name) },
      querySelectorAll: () => [],
    };
  };
  const nav = { basic: element(), storage: element() };
  const content = { basic: element(), storage: element() };
  nav.basic.active.add('active');
  content.basic.active.add('active');
  const context = {
    hasPermission: permission => permission === 'storage:read',
    switchPage: page => calls.push(`page:${page}`),
    loadConfig: () => calls.push('config'),
    initSettingsCustomSelects: () => {},
    initStorageSection: () => calls.push('storage'),
    toolStateMap: new Map(),
    window: { location: { hash: '#settings' } },
    history: { replaceState: () => {} },
    setTimeout: fn => fn(),
    document: {
      querySelectorAll: selector => selector === '.settings-nav-item' ? Object.values(nav)
        : selector === '.settings-section-content' ? Object.values(content) : [],
      querySelector: selector => nav[selector.match(/data-section="([^"]+)"/)?.[1]] || null,
      getElementById: id => content[id.replace('settings-section-', '')] || null,
    },
  };
  vm.createContext(context);
  vm.runInContext(sectionSource + openSource, context);
  await vm.runInContext('openSettings()', context);
  assert.deepEqual(calls, ['page:settings', 'storage']);
  assert.equal(nav.storage.active.has('active'), true);
  assert.equal(content.storage.active.has('active'), true);
  assert.equal(nav.basic.active.has('active'), false);
  vm.runInContext("switchSettingsSection('basic')", context);
  assert.equal(content.basic.active.has('active'), false);
});

for (const [permission, expected] of [['config:read', 'basic'], ['storage:read', 'storage'], ['update:read', 'update'], ['dashboard:read', 'token-usage'], ['', null]]) {
  test(`settings entry respects ${permission || 'no permissions'}`, () => {
    const context = {hasPermission: value => value === permission};
    vm.createContext(context);
    vm.runInContext(sectionSource, context);
    assert.equal(vm.runInContext('defaultSettingsSection()', context), expected);
    assert.equal(vm.runInContext("settingsSectionPermission('update')", context), 'update:read');
    assert.equal(vm.runInContext("settingsSectionPermission('token-usage')", context), 'dashboard:read');
  });
}
