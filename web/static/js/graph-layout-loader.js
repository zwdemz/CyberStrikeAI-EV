/** Load the optional 1.6 MB graph layout engine only when a graph needs it. */
(function () {
    let pending = null;
    window.ensureGraphLayoutLibrary = function () {
        if (typeof window.ELK !== 'undefined') return Promise.resolve(true);
        if (pending) return pending;
        pending = new Promise(resolve => {
            const script = document.createElement('script');
            let finished = false;
            const finish = success => {
                if (finished) return;
                finished = true;
                clearTimeout(timer);
                script.onload = script.onerror = null;
                if (!success) { script.remove(); pending = null; }
                resolve(success);
            };
            const timer = setTimeout(() => finish(false), 5000);
            script.src = '/static/vendor/elk.bundled.js';
            script.async = true;
            script.onload = () => finish(typeof window.ELK !== 'undefined');
            script.onerror = () => finish(false);
            document.head.appendChild(script);
        });
        return pending;
    };
})();
