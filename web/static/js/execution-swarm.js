/* Bounded, read-only Eino event visualization. Dependencies load only on demand. */
(function () {
    'use strict';
    let dependencies;

    /** Project a bounded radial layout. D3 supplies stable angular spacing when loaded. */
    function layout(graph, pie) {
        const nodes = [];
        [['agent', graph.agents, 190], ['tool', graph.tools, 350]].forEach(([kind, names, radius]) => {
            const slices = pie ? pie().sort(null).value(() => 1)(names) : names.map((name, index) => ({
                startAngle: index * Math.PI * 2 / names.length, endAngle: (index + 1) * Math.PI * 2 / names.length
            }));
            slices.forEach((slice, index) => {
                const angle = (slice.startAngle + slice.endAngle) / 2 - Math.PI / 2 + (kind === 'tool' ? .28 : 0);
                nodes.push({ kind, name: names[index], index, x: 500 + Math.cos(angle) * radius,
                    y: 400 + Math.sin(angle) * radius });
            });
        });
        return nodes;
    }

    /** Load pinned, same-origin libraries once. Failure remains a usable static graph. */
    function load() {
        if (!dependencies) {
            const d3 = new Promise((resolve, reject) => {
                if (window.d3) return resolve(window.d3);
                const script = document.createElement('script');
                script.src = '/static/vendor/swarm/d3-7.9.0.min.js';
                script.onload = () => resolve(window.d3);
                script.onerror = () => { script.remove(); reject(new Error('Visualization dependency unavailable')); };
                document.head.append(script);
            });
            dependencies = Promise.all([d3, import('/static/vendor/swarm/three.module.js?v=ev-csprng-1')]);
        }
        return dependencies;
    }

    /** Mount accessible HTML nodes plus an optional WebGL layer. Returns complete cleanup;
     * a disconnected or superseded mount cannot allocate GPU resources after loading. */
    function mount(host, graph, options) {
        let disposed = false, renderer, observer, frame, scene, camera, resources = [], particles = [], last = 0;
        let nodes = layout(graph), started = 0, contextLost = false;
        const motion = window.matchMedia('(prefers-reduced-motion: reduce)');
        const narrow = window.matchMedia('(max-width: 600px)');
        const text = options.text;
        options.onReady(false);
        const create = (tag, className, value) => {
            const element = document.createElement(tag); element.className = className;
            if (value !== undefined) element.textContent = value;
            return element;
        };
        const svgNS = 'http://www.w3.org/2000/svg';
        const svg = document.createElementNS(svgNS, 'svg');
        svg.setAttribute('viewBox', '0 0 1000 800'); svg.setAttribute('preserveAspectRatio', 'none');
        svg.setAttribute('aria-hidden', 'true');
        const guides = document.createElementNS(svgNS, 'g'); guides.setAttribute('class', 'execution-graph-guides');
        [115, 190, 350].forEach(radius => {
            const circle = document.createElementNS(svgNS, 'circle');
            circle.setAttribute('cx', '500'); circle.setAttribute('cy', '400'); circle.setAttribute('r', radius); guides.append(circle);
        });
        const paths = document.createElementNS(svgNS, 'g'); paths.setAttribute('class', 'execution-graph-links');
        svg.append(guides, paths); host.replaceChildren(svg); host.classList.add('execution-swarm');
        const core = create('div', 'execution-graph-core');
        core.append(create('small', '', 'CyberStrikeAI-EV'), create('strong', '', 'AGENT OPS'), create('span', '', text('graphContext')));
        host.append(core);
        const buttons = nodes.map(node => {
            const button = create('button', 'execution-graph-node'); button.type = 'button';
            button.dataset.kind = node.kind; button.dataset.index = node.index; button.title = node.name;
            button.setAttribute('aria-pressed', String(!!options.focus && options.focus.kind === node.kind && options.focus.name === node.name));
            button.append(create('small', '', text(node.kind === 'agent' ? 'graphAgent' : 'graphTool')), create('strong', '', node.name));
            host.append(button); return button;
        });
        if (!nodes.length) host.append(create('p', 'execution-graph-empty', text('graphEmpty')));
        const find = (kind, name) => nodes.find(node => node.kind === kind && node.name === name);
        const endpoints = link => [find('agent', link.agent), find('tool', link.tool)];
        function draw() {
            nodes.forEach((node, index) => { buttons[index].style.left = node.x / 10 + '%'; buttons[index].style.top = node.y / 8 + '%'; });
            paths.replaceChildren();
            graph.links.forEach(link => {
                const [source, target] = endpoints(link); if (!source || !target) return;
                const path = document.createElementNS(svgNS, 'path');
                path.setAttribute('d', `M ${source.x} ${source.y} Q 500 400 ${target.x} ${target.y}`);
                paths.append(path);
            });
        }
        draw();

        /** Stop finite replay on hidden tabs, reduced motion, resize or view disposal. */
        function pause() {
            cancelAnimationFrame(frame); frame = null; started = 0;
            particles.forEach(item => { item.mesh.visible = false; });
            if (renderer) renderer.render(scene, camera);
            options.onReplay(false);
        }
        function eligible() { return !disposed && !contextLost && host.isConnected && !document.hidden && !motion.matches && !narrow.matches; }
        function animate(now) {
            if (!eligible()) return pause();
            if (!started) started = now;
            const elapsed = now - started;
            if (elapsed > 8000) return pause();
            frame = requestAnimationFrame(animate);
            if (now - last < 1000 / 30) return;
            last = now;
            particles.forEach((item, index) => {
                // Every particle represents exactly one recorded call/result. Replay is finite.
                const progress = (elapsed - index * 90) / 2400;
                item.mesh.visible = progress >= 0 && progress <= 1;
                if (!item.mesh.visible) return;
                const t = item.reverse ? 1 - progress : progress, u = 1 - t;
                item.mesh.position.set(u * u * item.source.x + 2 * u * t * 500 + t * t * item.target.x,
                    u * u * item.source.y + 2 * u * t * 400 + t * t * item.target.y, 1);
            });
            renderer.render(scene, camera);
        }
        function replay() {
            if (!renderer || !eligible() || !particles.length) return;
            if (frame) return pause();
            options.onReplay(true); frame = requestAnimationFrame(animate);
        }
        function availability() {
            pause(); options.onReady(!!renderer && eligible() && particles.length > 0);
        }
        document.addEventListener('visibilitychange', availability);
        motion.addEventListener('change', availability); narrow.addEventListener('change', availability);
        options.setReplay(replay);
        // Static/mobile/reduced-motion views need neither dependency downloads nor a GPU.
        if (nodes.length && !motion.matches && !narrow.matches) load().then(([d3, THREE]) => {
            if (disposed || !host.isConnected) return;
            nodes = layout(graph, d3.pie); draw();
            if (!eligible()) return;
            try {
                renderer = new THREE.WebGLRenderer({ alpha: true, antialias: true });
                renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 1.5));
                renderer.domElement.className = 'execution-swarm-canvas'; renderer.domElement.setAttribute('aria-hidden', 'true');
                host.prepend(renderer.domElement);
                renderer.domElement.addEventListener('webglcontextlost', event => {
                    if (disposed) return;
                    event.preventDefault(); contextLost = true; pause(); options.onReady(false);
                });
                renderer.domElement.addEventListener('webglcontextrestored', () => { if (!disposed) { contextLost = false; availability(); } });
                scene = new THREE.Scene(); camera = new THREE.OrthographicCamera(0, 1000, 0, 800, .1, 100);
                camera.position.z = 10;
                const color = getComputedStyle(host).getPropertyValue('--primary-color').trim() || '#3b82f6';
                const geometry = new THREE.SphereGeometry(5, 8, 6), material = new THREE.MeshBasicMaterial({ color });
                resources.push(geometry, material);
                graph.links.forEach(link => {
                    const [source, target] = endpoints(link); if (!source || !target) return;
                    (link.events || []).slice(0, 50 - particles.length).forEach(event => {
                        const mesh = new THREE.Mesh(geometry, material); mesh.visible = false; scene.add(mesh);
                        particles.push({ source, target, mesh, reverse: event.type === 'tool_result' });
                    });
                });
                observer = new ResizeObserver(() => {
                    if (disposed) return;
                    pause(); renderer.setSize(host.clientWidth, host.clientHeight, false); renderer.render(scene, camera);
                });
                observer.observe(host); availability();
            } catch (_) {
                if (renderer) { renderer.dispose(); renderer.forceContextLoss(); renderer.domElement.remove(); renderer = null; }
                resources.forEach(resource => resource.dispose()); resources = [];
                options.onReady(false);
            }
        }).catch(() => { if (!disposed) options.onReady(false); });
        return () => {
            disposed = true; pause(); options.onReady(false); if (observer) observer.disconnect();
            document.removeEventListener('visibilitychange', availability);
            motion.removeEventListener('change', availability); narrow.removeEventListener('change', availability);
            resources.forEach(resource => resource.dispose());
            if (renderer) { renderer.dispose(); renderer.forceContextLoss(); renderer.domElement.remove(); }
            particles = []; options.setReplay(null);
        };
    }
    window.ExecutionSwarm = { mount };
    if (typeof module !== 'undefined' && module.exports) module.exports = { layout };
})();
