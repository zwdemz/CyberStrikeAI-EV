# Task timeout classification fix

## F0 — Intake and gates

- Branch: `codex/task-timeout-classification`; base: `deda642b`, clean and synchronized with origin/main on 2026-10-15.
- Workflow: P10 feedback → P8 backend correction → P9 verification. User authorized fixing the diagnosed bug and merging main.
- Existing evidence: customer screenshot and local handler analysis; customer deployed revision and raw error remain unknown.
- Workflow gate: implementation allowed. No UI/API/schema redesign required; required upstream contract is the existing task deadline and user-approved correction.
- Maintainability gate: narrow_fix; handlers are 318–673 lines, mix orchestration and persistence, moderate coupling. No broad refactor needed. Extract one small timeout predicate and cover context semantics and persisted batch outcome.
- Scope: single-agent, multi-agent, workflow, and batch task deadline classification. Unrelated robot request handling remains outside scope.

## F1 — Confirmed requirements

Confirmed by the user's request to fix the identified defect and merge main.

- R1: A live task must not be classified as globally timed out solely because a child operation returns DeadlineExceeded.
- R2: An expired task deadline must retain the existing timeout behavior, even if the returned error is wrapped or unrelated.
- R3: Cancellation must not be reclassified as deadline expiry, including cancellation whose cause wraps DeadlineExceeded.
- R4: Keep existing failure diagnostics, bounded execution limits, and cancellation behavior. This fix does not promise automatic continuation after every local failure or blindly replay tools.
- Acceptance: regression tests distinguish child timeout, task expiry, cancellation and wrapped HTTP errors; affected packages pass; main includes the reviewed change.

## F2 — Design

Introduce a package-local predicate using the task context's Err(), which reports DeadlineExceeded only when that context's deadline expires. Do not use the returned operation error or an arbitrary cancellation cause as evidence of task budget exhaustion.

Replace the four task-level OR checks with this predicate. Child timeouts follow the existing failed/error path with original error diagnostics and existing trace persistence. Real task expiry follows the unchanged timeout path. No new event schema or retry policy. No change to the 600-minute interactive or 6-hour batch budgets.

## F3 — Implementation plan

1. Add the shared predicate and update four handler call sites.
2. Add deterministic context tests (past deadlines, child contexts, cancellation causes) plus batch handler regression coverage for status/persistence.
3. Run targeted regression tests, affected package suites, and git diff --check; review the diff and update CHANGELOG.md.
4. Commit and push each phase; merge only after checks pass and remote main is rechecked. No release or deployment requested.

Rollback: revert the fix commit; no data migration. Customer-specific fault attribution remains a separate evidence gap.

## F4 — Implementation

Added `taskDeadlineExceeded`, based on task context Err rather than operation error or cancellation cause. Single-agent, multi-agent, role workflow and batch task paths now use it. Existing terminal error details, trace persistence and retry policy are unchanged.

Regression proof: before the fix, `TestBatchTaskDeadlineClassification` failed for both child DeadlineExceeded and wrapped HTTP timeout (got timeout, wanted failed). After the fix, targeted tests pass, including real expiry, unrelated returned error after expiry, custom deadline causes and user cancellation.

## F5 — Verification

- `go test ./internal/handler -run 'Test(BatchTaskDeadlineClassification|TaskDeadlineExceededContextOwnership)' -count=1`: passed after previously demonstrating the defect.
- `go test ./internal/handler ./internal/multiagent ./internal/workflow`: all three packages passed (17.789s, 17.230s, 2.545s).
- `git diff --check`: passed.
- Batch regression verifies finish status, queue failure and preservation of the original diagnostic in SQLite messages; context tests verify child timeout isolation, deadline expiry and cancellation semantics.
- No customer environment reproduction or deployment performed. The customer-specific original timeout source is still unknown; the deterministic classification defect is fixed.

## F6 — Review and merge readiness

- Reviewed all four call sites: each receives the task context created for its own execution budget; checks occur before timeout cleanup. Local errors now follow existing failed/error paths.
- Actual deadline expiry remains recognized even with a custom cause; a cancellation carrying a child timeout is no longer treated as exhausted task budget.
- Scope is four predicate substitutions plus a small shared helper and regression tests. No dependency, schema, public API, replay policy or task budget change.
- Added Unreleased/Fixed changelog entry. Implementation commit: `5ff781bd`; verification commit: `54213f82`.
- Merge is explicitly authorized by the user. Plan: re-fetch origin, fast-forward main if unchanged, push without force and verify remote equality. No deployment or release tag.
