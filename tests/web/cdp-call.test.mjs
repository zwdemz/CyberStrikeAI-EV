import { test } from 'node:test';
import assert from 'node:assert/strict';
import vm from 'node:vm';
import { callPage } from '../../scripts/cdp-call.mjs';

test('CDP arguments remain values, including script delimiters and credentials', async () => {
  const input = ['\";globalThis.injected=true;//', '</script><script>bad()</script>', "'\\\n\u2028\u2029", ['x', 'y']];
  const calls = [];
  const context = vm.createContext({ injected: false });
  const send = async (method, params) => {
    calls.push({method,params});
    if (method === 'Runtime.evaluate') { assert.equal(params.expression,'globalThis'); return {result:{objectId:'global'}}; }
    if (method === 'Runtime.callFunctionOn') {
      for (const value of input.slice(0,3)) assert.ok(!params.functionDeclaration.includes(value));
      const fn = vm.runInContext('('+params.functionDeclaration+')',context);
      return {result:{value:fn(...params.arguments.map(arg=>arg.value))}};
    }
    return {};
  };
  const result=await callPage(send,'session',function(...values){return values;},input);
  assert.deepEqual(Array.from(result),input);
  assert.equal(context.injected,false);
  assert.equal(calls.at(-1).method,'Runtime.releaseObject');
});

test('browser errors are redacted and handles released',async()=>{
 const methods=[];
 const send=async(method)=>{methods.push(method); if(method==='Runtime.evaluate')return {result:{objectId:'global'}}; if(method==='Runtime.callFunctionOn')return {exceptionDetails:{text:'sensitive value'}}; return {};};
 assert.deepEqual(await callPage(send,'session',function(){}),{__err:'Browser function failed'});
 assert.equal(methods.at(-1),'Runtime.releaseObject');
});
