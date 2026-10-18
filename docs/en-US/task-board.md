# Task board, issues and evidence

[中文](../zh-CN/task-board.md) · [Execution view](execution-view.md)

Open **AI execution** in the sidebar. Issues and the Plan → Execute → Verify board are primary. Tool failures are grouped by tool/status across the selected turn; approval/error events are explicitly page-scoped historical records. **View source** opens the authenticated event page, including off-page results. A recorded failure may already have been resolved: inspect its source before retrying. The Swarm graph starts collapsed and allocates visualization/GPU resources only when expanded.

## Synchronization

The visible page refreshes active conversations every 10 seconds and idle conversations every 30 seconds, detecting subsequent runs. Latest-turn following is the default; selecting historical turns pins the selection. **Latest turns** resumes following. Disabling auto sync pauses polling. Hidden pages and navigation cancel reads, and three consecutive failures pause polling until manual refresh.

Task status remains Agent-authored. Agent completion goes to Verify with **awaiting verification**, never independently verified. The task manager records the real run boundary before execution; latest boards exclude older task files both while running and after stop/restart. Unfinished tasks in a stopped latest run retain a paused label. Legacy conversations without a recorded boundary are explicitly marked unscoped instead of claiming a latest-run snapshot. Timestamps and a partial-read warning expose synchronization state. Partial file writes retain the last complete board; expanded cards stay expanded. No timestamp heuristic changes the actual runtime state.

## Evidence workflow

Expand a task and choose **Linked evidence**. Select a tool result from the current event page, or search the latest 25 project facts/vulnerability records in the permitted project/conversation. Link supporting records explicitly, then open their source for review. Unlinking preserves the source. References do not prove impact or establish verification. They survive service restart. Clearing the turn selector keeps board-only polling active. Reauthentication restores the active execution page after account-specific DOM was cleared. Task ID plus subject determines identity; renaming/reusing an ID with a different subject does not inherit old references. Status changes retain links.

Source permission and resource scope are rechecked during search and link reads/writes. Reading links requires `chat:read`; adding requires `chat:write` plus source read access; unlinking requires `chat:delete`. Tool results must belong to this conversation. Facts/vulnerabilities must belong to the same project, or vulnerabilities to the same conversation when no project is bound. Facts additionally require access to their recorded source conversation and any related vulnerability. Inaccessible/deleted sources disclose no label, ID or hidden-reference count.

Fact provenance permissions are applied in SQL before the 25-row search limit, so hidden recent facts cannot displace older readable results. Every evidence endpoint resolves the same active/persisted run boundary as the latest board. Tasks excluded by that boundary return 404 for listing, searching, adding or unlinking; existing historical references remain stored.

Vulnerability candidates also apply exact source read scope before the limit. Without a bound project, only records whose primary conversation is selected are eligible; observation-only records remain available in ordinary vulnerability lists but do not consume evidence-picker capacity.

Before enforcing capacity for a new link, the same transaction removes references whose source is confirmed deleted. Existing sources hidden by a user's permissions are retained; lack of permission never triggers source cleanup.

## APIs

All endpoints use normal authenticated API middleware and conversation resource checks. IDs in examples are placeholders.

| Method and URL | Input | Successful response |
| --- | --- | --- |
| GET `/api/conversations/{id}/plan-tasks/{taskId}/evidence` | None | `{"items":[],"taskIdentity":"..."}` |
| GET `/api/conversations/{id}/plan-tasks/{taskId}/evidence-sources` | `kind=project_fact` or `vulnerability`, optional `search` (max 400 bytes) | `{"items":[]}`, up to 25 readable source labels |
| POST `/api/conversations/{id}/plan-tasks/{taskId}/evidence` | `{"kind":"tool_result","sourceId":"...","taskIdentity":"..."}`, max 4 KiB | `{"id":"..."}`; duplicate requests are idempotent |
| DELETE `/api/conversations/{id}/plan-tasks/{taskId}/evidence/{linkId}` | None | `{"removed":true}`; source remains |

Source metadata contains `id`, `kind`, `sourceId`, `label`, `createdAt` and navigation metadata where applicable (`messageId`, `projectId`, `factKey`). No raw tool output, fact body or vulnerability evidence is copied into this response. At most 20 references are allowed per task identity. Success is 200; invalid input 400; unauthenticated requests 401 through normal authentication; missing permission/scope 403; unavailable tasks/references/sources 404; changed identity, partial task reads or reference limits 409; storage failures a generic 500. Link/unlink operations are audited without source payloads.

The latest-board API adds `incomplete`, `observedAt`, `runStartedAt`, `boundaryKnown` and per-task `identity`, `updatedAt`, `runtimeStatus`. Runtime states include `pending`, `in_progress`, `paused`, `previous_run`, `awaiting_verification` and `unknown`. These describe display/runtime state without rewriting persisted Agent status.

## Storage and deployment

An additive SQLite table `plan_task_evidence` stores `id`, `conversation_id`, `task_id`, `task_identity` (SHA-256 of task ID/subject), `kind`, `source_id`, `created_at`. A composite unique index on conversation/task/identity/kind/source deduplicates references and supports scoped lookup. The conversation foreign key cascades deletes. Source tables and task files are not modified by linking; no raw source payload is stored. An additional `conversation_plan_runs` table stores the latest `conversation_id` (primary key / cascading foreign key), `run_id` and UTC `started_at`. Run recording happens outside the manager mutex; persistence failure prevents that run from executing, rather than losing its boundary.

Windows/Linux use the same Go, SQLite and static browser implementation. Back up the database before upgrade, wait for active work before restart, and update the server/HTML/scripts/styles/locales together. The table is created automatically; no new runtime setting or model request is required. Older binaries ignore the additional table; preserve the database on rollback.

Validation: `go test ./tests/internal/handler -run 'TestPlanTask|TestConversationBoardSnapshot' -count=1`, the normal frontend tests, and `node tests/browser/execution-view.cjs`. Fixtures use temporary databases and synthetic sources, with no real model/target traffic.
