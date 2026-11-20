'use strict';

const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = path.resolve(__dirname, '../..');
const i18next = require(path.join(root, 'web/static/vendor/i18next.min.js'));

/** Execute the shipped i18n loader against real catalogs with isolated browser state. */
async function loadUI(storage, language) {
  const events = new Map();
  const document = {
    documentElement: {}, querySelectorAll: () => [], getElementById: () => null,
    addEventListener: (name, callback) => events.set(name, callback), dispatchEvent: () => {},
  };
  const window = {};
  const engine = i18next.createInstance();
  const context = vm.createContext({window, document, i18next: engine, navigator: {language},
    localStorage: {getItem: key => storage.get(key), setItem: (key, value) => storage.set(key, value)},
    console, CustomEvent: class {constructor(name, options) {this.type = name; this.detail = options.detail;}},
    fetch: async url => ({ok: true, json: async () => JSON.parse(fs.readFileSync(path.join(root, 'web', url), 'utf8'))}),
  });
  vm.runInContext(fs.readFileSync(path.join(root, 'web/static/js/i18n.js'), 'utf8'), context);
  events.get('DOMContentLoaded')();
  await window.i18nReady;
  return {window, document, engine};
}

test('Russian selection follows browser locale and falls back to English for EV keys', async () => {
  const {window, document, engine} = await loadUI(new Map(), 'ru-RU');
  assert.equal(window.uiLocale(), 'ru-RU');
  assert.equal(document.documentElement.lang, 'ru-RU');
  engine.addResourceBundle('en-US', 'translation', {evOnly: 'EV fallback'}, true, true);
  assert.equal(window.t('evOnly'), 'EV fallback');
  assert.notEqual(window.t('lang.ruRU'), 'lang.ruRU');
});

test('language selection persists across page reloads', async () => {
  const storage = new Map();
  const first = await loadUI(storage, 'en-US');
  await first.window.changeLanguage('ru-RU');
  assert.equal(storage.get('csai_lang'), 'ru-RU');
  const reloaded = await loadUI(storage, 'en-US');
  assert.equal(reloaded.window.uiLocale(), 'ru-RU');
  await reloaded.window.changeLanguage('zh-CN');
  assert.equal(reloaded.window.uiLocale(), 'zh-CN');
});
