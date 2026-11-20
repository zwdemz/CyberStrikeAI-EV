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

## Slow chat startup or history switching

Chat now requests `GET /api/conversations/:id?message_limit=40&include_process_details=0`. The optional `message_limit` accepts 1–100; `before_message_id` loads older messages using a cursor scoped to the same conversation. Responses keep the existing conversation fields and add `messagePage: {hasMore, beforeMessageId}`. Each page is chronological. Missing/deleted/foreign cursors return 400 and the UI offers a reload; a missing conversation returns 404. Paging cannot be combined with full process details. Authentication and resource access rules are unchanged. Omitting pagination retains the original full history, including for export and model context.

The browser renders the latest page in small batches. Use **Load earlier messages** for older history; prepending preserves reading position. Navigation cancels the previous request. A 15-second deadline covers fetch and JSON parsing and a visible retry replaces an indefinite blank/loading state. Approval metadata no longer blocks message display; sending still waits for approval configuration. Malformed Markdown falls back to escaped plain text.

Network/timeout fallback uses at most 12 recent page snapshots, no older than 60 seconds, with at most one million content characters per snapshot and four million total. Cached responses are visibly labelled. Access-denied/deleted/server-error responses never fall back to a cached conversation. No messages are removed from storage.

The optional graph layout engine is loaded only on opening a graph; a five-second loading failure falls back to the existing basic layout. Chat startup does not wait for it or for translation downloads. Offscreen content rendering and turn-marker updates reduce work as history grows. Search/export still use stored history; the visible turn navigator covers only loaded messages.

Deploy the binary, frontend assets and template together; asset version keys refresh browser caches. Startup creates the message ordering index without changing message data. Back up the binary, changed files and database before deployment; restore the binary and frontend together for rollback (the additional index can remain). Validate on Linux. Controlled browser fixtures with 2,000 messages confirm bounded rendering, paging, rapid switching and retry recovery; real latency still depends on message size, browser, network and auxiliary services.

## Parallel asset writes report `database is locked`

Asset batches now begin an immediate SQLite write transaction before reading the deduplication key. The primary SQLite pool permits eight connections, waits up to ten seconds for a busy writer, and retries a whole rolled-back asset batch at most twice more when SQLite reports BUSY. A single-asset update uses the same bounded retry. Agent and HTTP requests cancel waiting when their context ends. Validation, access denial and other SQL failures do not retry.

Normal concurrent `create_asset` calls should wait and persist rather than fail within milliseconds. An external writer holding the database beyond the bounded wait can still return BUSY; the Agent receives a retryable message and the HTTP asset endpoint returns 503. Inspect other processes, disk health, and long-running imports before retrying. Previously failed calls are not recreated automatically: compare the source results with stored assets and resubmit missing targets under the same authorization scope. Deduplication prevents duplicate rows. Deploy a rebuilt server after backing up the binary and SQLite database; there is no schema or configuration migration. Restore the prior binary to roll back.

## Deep repeats a completed step after a long run

Completed tool results are saved in conversation process details before the next step. Each Eino model call now receives a small index of recent successful tool names, timestamps, and process-detail IDs from that durable timeline, even after context reduction. It excludes arguments, raw output, failed/blocked calls, and background work. A tool's reported success is only a reminder to verify actual state, not proof that the target changed. Record each confirmed major finding with `upsert_project_fact` before moving on; the project fact retains the evidence and context across conversations. If no project is bound, keep a concise evidence summary in the conversation and bind a project when long-term sharing is needed. If a completed tool result cannot be stored, the run stops with a state-check warning instead of silently continuing. Existing sessions gain the index from their stored process details after upgrading the server; no data migration is required. Back up the binary and SQLite database before deployment, and restore the previous binary to roll back (the new index can remain).
