# Troubleshooting

[中文](../zh-CN/troubleshooting.md)

Debug by layer. Do not change random config before locating the failing layer.

## Diagnostic Order

1. Process: is the service alive, any panic?
2. Network: port, HTTPS, reverse proxy, browser console.
3. Auth: does `/api/auth/validate` return 200?
4. Config: can `/api/config` be read and applied?
5. Model: does model test pass?
6. Tools: do tool list and schemas look right?
7. Database: is `data/` writable, any lock?
8. Subsystem: KB, MCP, C2, WebShell minimal action.

## Minimal Commands

```bash
lsof -i :8080
curl -k -I https://127.0.0.1:8080/
curl -k -I https://127.0.0.1:8080/static/logo.png
ls -lh data/
```

If a reverse proxy is involved, test both proxy address and upstream address.

## Common Issues

Page inaccessible:

- wrong protocol, especially HTTPS vs HTTP;
- self-signed cert warning;
- port occupied;
- reverse proxy loop.

Login fails:

- wrong RBAC user password;
- config not applied/restarted;
- stale cookie;
- audit throttling repeated failures.

### Recover a forgotten `admin` password

If another administrator with `rbac:write` is available, reset the password under **Platform permissions → User management**.

If no administrator session is available, the built-in `admin` account can be recovered on the server. Change to the project root and run:

```bash
./run.sh --reset-admin-password
```

Enter and confirm the new password when prompted. The script hides input and stores a bcrypt hash. If the service is running, restart it afterward to invalidate existing login sessions.

If `run.sh` is not available, run the command below manually. Enter and confirm the new password when prompted:

```bash
HASH=$(htpasswd -nBC 10 '' | cut -d: -f2 | tr -d '\n') && sqlite3 data/conversations.db "UPDATE rbac_users SET password_hash='$HASH', updated_at=CURRENT_TIMESTAMP WHERE id='admin' AND username='admin' AND is_builtin=1; SELECT changes();"
```

Output `1` means that the row was updated. The command requires `sqlite3` and `htpasswd`. If `database.path` in `config.yaml` is not the default, replace `data/conversations.db`. Password input is hidden and is not written to shell history.

Model fails:

- selected AI channel does not exist; empty selection follows `ai.default_channel`;
- wrong `ai.channels.<id>.base_url` path;
- invalid API key;
- model unavailable;
- reasoning fields unsupported by gateway. Try `ai.channels.<id>.reasoning.mode: off`.

Streaming stalls:

- proxy buffers SSE;
- model gateway timeout;
- context too large;
- browser/network interruption.

Tool fails:

- real command not installed;
- YAML schema wrong;
- HITL rejected or pending;
- timeout or no-output timeout.

Knowledge base empty:

- `knowledge.enabled` false;
- scan/index not run;
- embedding API failed;
- threshold or risk type too strict.

C2 returns 503:

- expected when `c2.enabled: false`.

## Common Misdiagnoses

- "Model is broken": HITL is waiting.
- "Tool missing": tool_search hides it from current context.
- "Knowledge base useless": index not rebuilt or risk type too narrow.
- "Config saved but ineffective": listener/TLS changes need restart.
- "Robot silent": platform callback or signature config wrong.

## Issue Template

```text
Version:
Startup method:
Access path:
Relevant config:
Steps:
Expected:
Actual:
Server logs:
Browser console:
API response:
```

## Summary failure: `finish_reason="length"`

The model reached an output limit before completing its summary. Partial text must not replace conversation history.

`multi_agent.eino_middleware.summarization_output_reserve_tokens` sets the summary output limit (default: 40960). The prompt targets half that budget, capped at 2048 tokens. A provider-confirmed output-limit response triggers one compact retry with the original input, targeting a quarter of the budget, capped at 1024 tokens. The output limit stays unchanged. Only nonempty, explicitly completed summaries enter subsequent context; another truncation returns an error and leaves the original history intact.

Check the failing conversation's actual model/channel, which may differ from the current default. The channel's `max_total_tokens` is a context budget, not the summary output limit. Before raising the summary reserve, verify provider output limits and available context headroom; hidden reasoning may also consume completion tokens. Restart after changing configuration, then continue the conversation.

Authentication failures, content filters, missing completion metadata and broken streams are not treated as output-limit recovery. Existing transient network retries still apply. The compact retry adds at most one request per summary generation attempt, with corresponding latency and cost. It does not call tools, continue partial text, or guarantee success with an insufficient output budget.

## HTTP tool timeouts and incomplete responses

`http-framework-test` exits with status 1 for network failures and emits a `Failure:` JSON object. `error_code` distinguishes connect/TLS, read, write and connection-pool timeouts, DNS, proxy and TLS errors. `phase` identifies the observed stage; `response_headers_or_redirect` includes redirect processing, while `response_body` means the final response headers arrived. `response_status` and `received_body_bytes` describe partial progress, never a complete or successful response. Raw exception messages are omitted to avoid disclosing credentials or proxy URLs.

The `timeout` parameter is a positive finite number of seconds, defaulting to 60. It limits each network wait phase, not the total duration; the executor's process deadline still applies. Invalid values fail before opening a connection. Normal calls make no separate DNS/TCP/TLS probe. `verbose_output=true` explicitly enables an extra diagnostic connection unless a proxy is used; each probe socket operation is capped at five seconds, but DNS still uses the system resolver. Failure output separates request time from operation time, including the optional probe.

For a timeout, check routing/proxy reachability for connection failures, service load and response size for reads, and request size for writes. Check DNS configuration or certificate chain/hostname/time for the corresponding errors. Only consider a bounded retry after confirming idempotency, authorization and the remaining call budget. A timed-out write may already have taken effect. There is no automatic retry, tool switch or TLS-verification downgrade. Education and enterprise SRC still enforce one request, a 10-second per-phase timeout, and their existing run limits.

## Nmap rejects `--host-timeout`

A value such as `720s` is valid. Earlier EV argument construction could insert `scan_type` between an option in `additional_args` and its value, producing `--host-timeout -sT -sV 720s`. The executor now places scan options before positional operands and appends additional arguments intact, including quoted values. No timeout-unit conversion is necessary. Rebuild and restart the server as well as updating the HTTP tool YAML for this fix.

This does not expand SRC permissions: restricted Nmap calls still accept one host and at most ten explicit ports, disallow port ranges, and use the enforced low-impact options. Argument construction tests never scan a remote host.

Regression checks (Python requires `httpx` and `PyYAML`):

```sh
go test ./internal/security ./tests/internal/rolepolicy ./tests/internal/config
python -m unittest discover -s tests/tools -v
```

Before deployment, back up the running binary and tool YAML, preserve local tool customizations, deploy the rebuilt binary and updated YAML together, and restart when no task is active. Roll back both files together if needed; no database migration or configuration reset is required.
