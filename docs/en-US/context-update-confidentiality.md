# Context and update-source confidentiality

## Automatic project context

Project blackboard builders do not receive a requesting principal. They therefore
must not query or inject vulnerability rows, even when the context budget permits
it. This applies to initial context, summary refresh and delegated tasks. Project
facts and recovery instructions remain available under their existing access rules.
Vulnerability retrieval must go through the tools that enforce `vulnerability:read`
and the caller's resource scope. Automatically including findings for privileged
roles would require propagating and validating that principal at every entry point.

## Update source output

The update HTTP boundary copies transport data and removes URL user information,
query strings and fragments from all string fields, including nested errors and
job results. Known source credentials echoed separately by Git are also redacted.
Audit messages/details and progress logs are sanitized before recording. Git and
configuration persistence retain the original source internally.

Prefer SSH keys or a credential helper over credentials embedded in repository
URLs. A redacted address displayed in the UI is not the original authenticated
address; replacing the source is an explicit configuration operation.

## Verification and limitations

Regression tests cover a large-budget blackboard with restricted findings,
status/job/check/source responses, persisted audit rows, nested error fields,
encoded credentials, unchanged internal source values and exact JSON numbers.
The focused tests run on both Windows and Linux in the Native service workflow.

This fix prevents new automatic vulnerability injection and new update output
exposure. It does not erase older conversations, model-side history or audit logs.
If credential-bearing sources were previously used, rotate those credentials and
review historical access separately. Source findings alone are not evidence that
an actual deployment was accessed or data was disclosed.
