# Security Hardening

[中文](../zh-CN/security-hardening.md)

This checklist covers pre-production and continuous hardening for CyberStrikeAI.

## Before Going Live

- Change the initial `admin` password from the Web UI after first login.
- Use HTTPS or a trusted reverse proxy.
- Restrict access by IP, VPN, or bastion.
- Enable `audit.enabled`.
- Set `c2.enabled: false` when C2 is not required.
- Do not expose standalone HTTP MCP without strong auth and network isolation.
- Connect only trusted external MCP services.
- Back up `config.yaml`, `data/`, and custom resource directories.

## Reverse Proxy Baseline

```nginx
client_max_body_size 200m;
proxy_buffering off;
proxy_http_version 1.1;
proxy_set_header Host $host;
proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
proxy_set_header X-Forwarded-Proto https;
proxy_set_header Upgrade $http_upgrade;
proxy_set_header Connection "upgrade";
```

Recommended security headers:

```nginx
add_header X-Content-Type-Options nosniff;
add_header Referrer-Policy no-referrer;
add_header X-Frame-Options DENY;
```

## HTTP Trust and Resource Limits

Forwarding headers are ignored by default. Configure `server.trusted_proxies` with the actual reverse proxy IPs/CIDRs so IP rate limits identify clients correctly. An empty list supports direct access. Wildcards, catch-all CIDRs and hostnames are rejected. Restrict backend access to those proxies and overwrite client-supplied forwarding headers; for a single proxy use `proxy_set_header X-Forwarded-For $remote_addr;`.

```yaml
server:
  trusted_proxies: ["127.0.0.1", "::1"] # Example only: a proxy on the same machine
  read_header_timeout_seconds: 10
  read_timeout_seconds: 300
  idle_timeout_seconds: 120
  webhook_max_body_bytes: 1048576
```

Omitted or zero limits select these defaults. Timeout values must be 0–86400 seconds and the WeCom callback body limit 0–67108864 bytes; invalid values fail startup. Both the main server and the standalone HTTP MCP listener receive read/idle deadlines. Global write deadlines remain disabled for streaming. Adjust the request-read deadline for upload size and bandwidth. Missing WeCom signature parameters are rejected without consuming the body, and oversized callbacks receive HTTP 413. Signature and replay checks remain required. Other uploads and callbacks retain their existing body limits. Restart after configuration changes.

Access logs contain only method, route template, status, client IP and duration. Panic logs omit request dumps and panic values. Query-token authentication for SSE/WebSocket remains supported by the authentication middleware. Reverse proxies must also exclude query strings, Authorization and Cookie from logs. For Nginx, define this format in `http` and select it in the applicable `server` block; avoid `$request` and `$request_uri`:

```nginx
log_format cyberstrike_safe '$remote_addr $request_method $uri $status $body_bytes_sent';
access_log /var/log/nginx/cyberstrike-access.log cyberstrike_safe;
```

Existing logs and sessions are unchanged. If historical logs contain valid tokens, preserve audit evidence and revoke affected sessions through the incident response process.

Full external MCP configuration and connection diagnostics require global `mcp:write`. Read-only responses retain operational metadata but omit commands/arguments, mask environment/header values and reduce endpoint URLs to scheme and host.

## HITL Allowlist Baseline

Minimal allowlist:

```yaml
hitl:
  tool_whitelist:
    - read_file
    - glob
    - grep
    - tool_search
```

Do not globally allowlist:

- `execute`;
- WebShell write/execute tools;
- C2 task/payload tools;
- high-risk external MCP tools;
- delete, write, upload, persistence tools.

## File Permissions

```bash
chmod 600 config.yaml
chmod 700 data
```

Run under a dedicated OS user. Avoid root unless explicitly required.

## External MCP Review

Before connecting:

- Can it execute commands?
- Can it read/write local files?
- Does it send data to third parties?
- Does it authenticate?
- Can output contain untrusted model/web content?
- Should it run in a container or separate user?

After connecting:

- keep high-risk tools out of allowlist;
- review tool list changes;
- audit config changes.

## C2 and WebShell

C2:

- disabled by default;
- enabled only during authorized window;
- listener ports separated from admin UI;
- cleanup payloads, sessions, tasks, and events.

WebShell:

- authorized targets only;
- clear naming;
- write/delete/execute requires approval;
- delete connections after project end.

## Retention

Suggested:

- audit: 30-90 days;
- monitor: 90-180 days;
- uploads: clean after project;
- C2/WebShell outputs: keep only report evidence;
- knowledge base: no real credentials or customer secrets.

## Periodic Review

Weekly:

- failed logins and unusual IPs;
- config changes;
- external MCP changes;
- long-running tools;
- unexpected C2 enablement;
- stale WebShell connections;
- disk and DB size.

Project closeout:

- clean temp workspaces;
- delete unnecessary uploads;
- archive evidence;
- delete stale WebShell/C2 resources;
- export audit records.
