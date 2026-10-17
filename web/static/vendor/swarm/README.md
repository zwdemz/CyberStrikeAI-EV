# Agent Swarm visualization dependencies

These packages are loaded on demand from this application's own origin. No CDN is required.

| Package | Version | Files | License |
| --- | --- | --- | --- |
| D3 | 7.9.0 | d3-7.9.0.min.js | ISC, see D3-LICENSE |
| Three.js | 0.186.1 | three.module.js, three.core.js | MIT, see THREE-LICENSE |

`manifest.json` records official npm tarball URLs, registry SHA-512 archive integrity and SHA-256 for each unchanged extracted file, including licenses. The browser loads Three.js as ES modules; keep both files together. Before upgrading, verify archive integrity, regenerate the file manifest, inspect license changes and run the frontend and actual WebGL browser regressions. The graph adapter intentionally excludes reasoning, raw tool payloads and inferred inter-agent messages.
