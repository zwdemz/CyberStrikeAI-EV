/** Bounded chat history requests. No automatic retries or stale authorization fallback. */
(function (root) {
    'use strict';
    const PAGE_SIZE = 40;

    /** Fetch one page, with cancellation covering authentication, headers and JSON.
     * Rejects HTTP errors with status, cancellation with AbortError, deadline with
     * TimeoutError, and malformed history with Error. The caller owns rendering.
     */
    async function requestPage(fetcher, conversationId, options = {}) {
        const controller = new AbortController();
        let timer;
        let cancel;
        const stopped = new Promise((_, reject) => {
            cancel = () => {
                controller.abort();
                reject(Object.assign(new Error('History request cancelled'), { name: 'AbortError' }));
            };
            timer = setTimeout(() => {
                controller.abort();
                reject(Object.assign(new Error('History request timed out'), { name: 'TimeoutError' }));
            }, options.timeoutMs || 15000);
        });
        const signal = options.signal;
        if (signal) signal.addEventListener('abort', cancel, { once: true });
        try {
            if (signal && signal.aborted) cancel();
            const params = new URLSearchParams({ include_process_details: '0', message_limit: String(PAGE_SIZE) });
            if (options.beforeMessageId) params.set('before_message_id', options.beforeMessageId);
            const work = async () => {
                if (controller.signal.aborted) return stopped;
                const response = await fetcher('/api/conversations/' + encodeURIComponent(conversationId) + '?' + params, { signal: controller.signal });
                if (!response.ok) throw Object.assign(new Error('History request failed (' + response.status + ')'), { status: response.status });
                const data = await response.json();
                if (!data || data.id !== conversationId || (data.messages != null && !Array.isArray(data.messages))) {
                    throw new Error('Invalid history response');
                }
                return data;
            };
            return await Promise.race([stopped, work()]);
        } finally {
            clearTimeout(timer);
            if (signal) signal.removeEventListener('abort', cancel);
        }
    }

    root.ChatHistory = { PAGE_SIZE, requestPage };
    if (typeof module !== 'undefined' && module.exports) module.exports = root.ChatHistory;
})(typeof window !== 'undefined' ? window : globalThis);
