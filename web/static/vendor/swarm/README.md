# Agent Swarm visualization dependencies

These packages are loaded on demand from this application's own origin. No CDN is required.

| Package | Version | Files | License |
| --- | --- | --- | --- |
| D3 | 7.9.0 | d3-7.9.0.min.js | ISC, see D3-LICENSE |
| Three.js | 0.186.1 | three.module.js, three.core.js | MIT, see THREE-LICENSE |

`manifest.json` records official npm tarball URLs, registry SHA-512 archive integrity and the shipped SHA-256 for each file, including licenses. Two Three.js files carry the documented local `three-csprng-uuid.patch`: UUID entropy uses Web Crypto with no insecure fallback, and the core import has a cache version. Their `upstreamSha256` values preserve the unmodified package hashes. Other files are unchanged. The browser loads Three.js as ES modules; keep both files together. Before upgrading, verify archive integrity, reapply/review the local patch, regenerate the file manifest, inspect license changes and run the frontend and actual WebGL browser regressions. Without Web Crypto the optional visualization falls back to its static view. The graph adapter intentionally excludes reasoning, raw tool payloads and inferred inter-agent messages.
