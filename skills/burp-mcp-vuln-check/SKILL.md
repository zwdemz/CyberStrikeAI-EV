---
name: burp-mcp-vuln-check
description: Automate low-impact web vulnerability verification through Burp MCP. Use when Codex is asked to check, reproduce, triage, or write evidence for vulnerabilities using Burp Suite proxy history, Repeater, Collaborator/OOB payloads, HTTP replay, parameter mutation, response diffing, or scanner issues. Also use for mini program/微信小程序 Burp history, wildcard domains like *.example.com, root-domain traffic reviews, arbitrary login/任意登录, session_key/sessionKey/sessionkey/session-key/session key/sess_key/sessKey/wxSessionKey/wx_session_key/wechatSessionKey/thirdSessionKey/decryptKey/openDataKey, jscode2Session/jscode2session/code2Session, openid/unionid, getPhoneNumber, encryptedData/iv, appSecret, or phone-login checks; in those cases enumerate all Hosts and search response bodies for WeChat session leaks before reporting no issue.
---

# Burp MCP Vulnerability Check

## Operating Rules

Treat every check as authorized testing against the user's target. Keep probes low impact: prefer safe markers, benign arithmetic, DNS-only OOB payloads, timeout caps, and read-only requests. Do not mass exploit, persist shells, dump secrets, or run destructive payloads unless the user explicitly authorizes that exact action.

If the task is based on a writeup, first extract the vulnerability class, affected paths, required headers/cookies, trigger parameters, positive/negative indicators, and any OOB behavior. If article-specific details are missing, use the generic workflow below and ask for the missing PoC only when a targeted check cannot be inferred.

For article-derived checks, read `references/article-rule-template.md` and fill the relevant fields mentally or in notes before probing.

If the user asks broad website/SRC triage such as "帮我排查 xx 网站漏洞", "看看某域名有没有漏洞", wildcard/root-domain review, or wants multi-step investigation around Burp findings, use `cairn-collaborative-exploration` as the coordination/state layer first and keep this skill focused on HTTP evidence collection and low-impact verification.

## Burp MCP Workflow

### Mandatory Mini Program Preflight

When the user mentions a mini program, 微信小程序, 小程序漏洞, 任意登录, phone login, `session_key`, `jscode2Session`, `openid`, `unionid`, or asks to review Burp history for a wildcard/root domain such as `*.anjia.com`, do this before any other vulnerability conclusion:

Run `scripts/miniapp_burp_preflight.py <root-domain>` when a local Burp MCP proxy is available. Use its host list and sensitive matches as the initial evidence map, then continue with manual validation. If not running the script, perform the same steps manually with Burp MCP regex history searches.

1. Query the root/wildcard domain broadly and enumerate all observed `Host` values. Do not only analyze the most obvious API host such as `api.<root>`.
2. For every observed Host, search both request and response content for:
   session-key variants (`session_key`, `sessionKey`, `sessionkey`, `session-key`, `session key`, `sess_key`, `sessKey`, `wxSessionKey`, `wx_session_key`, `wechatSessionKey`, `weChatSessionKey`, `weixin_session_key`, `thirdSessionKey`, `3rd_session_key`, `decryptKey`, `decryptionKey`, `phoneDecryptKey`, `openDataKey`), plus `jscode2Session`, `jscode2session`, `code2Session`, `openid`, `unionid`, `getPhoneNumber`, `encryptedData`, `iv`, `phone`, `mobile`, `appSecret`, `api.weixin.qq.com`.
3. Use separate regex searches for Host and sensitive fields if the Burp history text may not match across request/response lines. Do not rely on `host.*session_key`.
4. Treat endpoints named like `jscode2Session4Xxx`, `getOpenId`, `getWxInfo`, `getSecretPhone`, `getPhone`, `phoneLogin`, and `bindPhone` as login-chain candidates even if they are not under `/api` or `/gateway`.
5. Do not say "no session_key leak found" until the host enumeration and session-key variant cross-search have been completed.
6. If a WeChat `session_key`/`openid`/`unionid` response is found, classify that before pursuing IDOR. IDOR can be a secondary finding, but it must not displace the login-key leak.

Concrete failure case to avoid: for `*.anjia.com`, `api.anjia.com/gateway/...` had IDOR-like signals, but the primary issue was on `ajia.anjia.com/Mini/FlagshipStore/jscode2Session4FlagShipStore`, whose response returned `session_key`, `openid`, and `unionid`. Missing this means the preflight was not done.

1. Scope the target
   - Get recent proxy history with `mcp__burp__get_proxy_http_history` or regex-filter it with `mcp__burp__get_proxy_http_history_regex`.
   - Identify host, scheme, candidate endpoints, auth state, content type, CSRF tokens, and reflection points.
   - Check scanner context with `mcp__burp__get_scanner_issues` when the user asks for triage or prioritization.

2. Preserve a baseline
   - Replay one unmodified candidate request with `mcp__burp__send_http1_request` or `mcp__burp__send_http2_request`.
   - Record status code, response length, key headers, stable body markers, timing, redirects, and errors.
   - Create a Repeater tab with `mcp__burp__create_repeater_tab` when manual follow-up would help the user.

3. Mutate one variable at a time
   - Change only the suspected path segment, query parameter, body field, header, cookie, or JSON key.
   - Keep a negative control that should not trigger the bug.
   - For encoded vectors, use `mcp__burp__url_encode`, `url_decode`, `base64_encode`, and `base64_decode` instead of hand-encoding.

4. Verify by differential evidence
   - Compare mutated vs baseline response code, length, body markers, error messages, redirects, and timing.
   - Prefer two independent indicators before calling a finding confirmed.
   - For blind vulnerabilities, generate a Burp Collaborator payload with `mcp__burp__generate_collaborator_payload`, inject it in the suspected sink, then poll `mcp__burp__get_collaborator_interactions`.

5. Report conservatively
   - Classify as `confirmed`, `likely`, `inconclusive`, or `not reproduced`.
   - Include exact request target, mutation, evidence, confidence, and why the probe is low impact.
   - Mention missing prerequisites, blocked auth, anti-CSRF, WAF interference, or environment limits.

## Common Check Patterns

Use these as starting points only; adapt them to the article's indicators.

- Path traversal / arbitrary file read: mutate filename/path parameters with safe reads such as known public files or application config names that should not expose secrets. Confirm with body markers and avoid dumping sensitive contents.
- SSRF / blind callback: place a Collaborator hostname in URL-like parameters, headers, import/fetch fields, webhook settings, XML/JSON URLs, or image/document fetchers. Confirm via DNS/HTTP interaction.
- SQL injection: use boolean/time-safe probes with short delays and negative controls. Avoid data extraction. Confirm through response differences or bounded timing deltas across repeated requests.
- Command/template/code injection: use harmless arithmetic or DNS-only OOB markers where possible. Avoid shell-spawning or file writes.
- Access control / IDOR: replay the same endpoint with changed object identifiers and current auth only. Confirm by stable object ownership markers; do not enumerate.
- Deserialization / framework gadget issues: prefer version/banner evidence and harmless callback probes. Do not send weaponized gadget chains unless explicitly authorized.
- File upload / parser bugs: upload inert marker files or polyglot probes only when the workflow already supports upload. Confirm storage path, transformation behavior, or callback without executing payloads.

## Burp MCP Tool Notes

- Use `send_http1_request` for raw HTTP/1.1 requests copied from proxy history. Preserve `Host`, cookies, content length semantics, and CRLF formatting.
- Use `send_http2_request` only when the original request is HTTP/2 or pseudo-headers matter.
- Use regex history search for article-specific paths, parameter names, file extensions, framework banners, or error keywords.
- Use Collaborator sparingly and poll more than once when the target may process jobs asynchronously.
- Use Intruder only for a small, user-approved candidate set; keep payload count minimal.

## Evidence Format

Return findings in this shape:

```text
Status: confirmed | likely | inconclusive | not reproduced
Target: https://host/path
Vulnerability class:
Mutated input:
Baseline:
Probe:
Evidence:
Impact:
Limitations:
Recommended fix:
```
