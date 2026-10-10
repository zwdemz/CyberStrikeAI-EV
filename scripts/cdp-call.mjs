/** Call browser code with CDP by-value arguments, never interpolated source.
 * @param {Function} send CDP request function.
 * @param {string} sessionId Attached browser session.
 * @param {Function|string} fn Trusted function or static function declaration.
 * @param {Array} values JSON-serializable arguments, including untrusted strings.
 * @returns {Promise<unknown>} Result value; browser exceptions return an error marker.
 */
export async function callPage(send, sessionId, fn, values = []) {
  const global = await send('Runtime.evaluate', { expression: 'globalThis' }, sessionId);
  const objectId = global.result?.objectId;
  if (!objectId) throw new Error('Browser execution context unavailable');
  try {
    const result = await send('Runtime.callFunctionOn', {
      objectId,
      functionDeclaration: typeof fn === 'function' ? fn.toString() : fn,
      arguments: values.map(value => ({ value })),
      awaitPromise: true,
      returnByValue: true,
    }, sessionId);
    if (result.exceptionDetails) return { __err: 'Browser function failed' };
    return result.result?.value;
  } finally {
    await send('Runtime.releaseObject', { objectId }, sessionId);
  }
}
