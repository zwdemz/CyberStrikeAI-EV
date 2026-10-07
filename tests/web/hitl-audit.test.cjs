'use strict';

const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

test('persisted audit failures render differently from policy rejection', () => {
  const context = vm.createContext({
    window: {addEventListener() {}},
    document: {addEventListener() {}},
    console,
  });
  vm.runInContext(fs.readFileSync(path.join(__dirname, '../../web/static/js/hitl.js'), 'utf8'), context);
  const failed = context.hitlRecordDecisionLabel({decision: 'reject', comment: '[audit_error] HTTP 400'});
  const rejected = context.hitlRecordDecisionLabel({decision: 'reject', comment: 'policy violation'});
  assert.notEqual(failed, rejected);
  assert.match(failed, /异常|failed/i);
  assert.equal(context.hitlRecordDecisionLabel({decision: 'approve'}), context.hitlDecisionLabel('approve'));
});
