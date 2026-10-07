# Model gateway compatibility

## Audit model temperature

Configure the audit model independently when a gateway requires a particular
sampling temperature. The same field is available in the Audit Agent settings.

```yaml
hitl:
  audit_model:
    provider: openai_compatible
    temperature: 0.6
```

Use the value required by your provider; `0.6` is an example, not a universal
GLM setting. Omit the field or clear the settings input to retain the previous
audit default of `0.1`. Explicit zero is supported. Values must be finite and
between 0 and 2. Audit temperature does not inherit the main model's setting.
Saving a value preserves its precision. No fallback retries change the value
automatically, and no API keys belong in examples or bug reports.

An upstream failure continues to block execution. The audit record and UI
identify it as an audit service error, separately from a model rejection. The
existing `reject` decision remains for execution compatibility; `[audit_error]`
in the persisted comment distinguishes the operational failure. HTTP status and
allowlisted diagnostic categories are retained. Raw gateway bodies are not
logged or displayed because they can echo credentials and tool inputs.

## MCP function names

Provider-facing names use ASCII letters, digits, underscores and hyphens, with
a 64-byte limit. Existing compliant names remain unchanged. Modified names get
a deterministic digest suffix to prevent punctuation collisions such as
`fs.read` versus `fs_read`. Conflicting registered aliases fail initialization
instead of routing to the wrong tool.

MCP execution, role policy and approval retain the original registered name.
The model must use the names in the current tool definitions, rather than guess
aliases. A historical conversation with old invalid names may need a new turn
in which the model selects from the current definitions.

## Tool argument recovery

A single valid JSON object wrapped exactly in a Markdown code fence can be
unwrapped before authorization and approval. Both the reviewer and executor
receive the same normalized arguments. Multiple objects, trailing prose, arrays
and broken JSON are not silently truncated into executable calls; normal tool
error recovery asks the model to correct them.

## Validation and rollback

Regression tests use local mock gateways, not live model accounts. They cover
default, zero and custom temperatures, configuration persistence, HTTP failure
classification, routing identities, name collisions and conservative argument
recovery. Run the affected Go packages on Linux before deployment. Roll back
the application binary and matching static assets together; the optional
temperature field requires no database schema migration.
