# v1.7.24 integration

This EV integration preserves existing release tags. A new release is created only
after the EV `dev` pull request passes checks and merges into `main`.

## Module decisions

| Module | Integrated behavior | EV behavior retained |
| --- | --- | --- |
| Conversation recovery | Historical argument normalization, summary provenance, checkpoint recovery and workspace evidence | Bounded summary retry across providers, original-history retention on failure, paginated chat rendering |
| Token accounting | Cache-write counters, provider normalization, persisted aggregation and usage analysis | Account authorization, all-accessible-project dashboard summary and existing statistics dialog |
| Vulnerability retesting | Separate retest conversations, placeholder resolution and append-only retest evidence | Evidence-based severity, assessment history, project-aware deduplication and report formatting |
| SQLite | Additive schema reconciliation and a foreign-key rebuild that preserves extra columns, triggers and dependent rows | EV extension migrations, eight-connection pool, immediate writer transactions and bounded asset retry |
| Knowledge | Startup schema repair | Index manifest, atomic vector replacement, bounded search and pagination |
| Configuration | Candidate configuration persistence and rollback on save failure | Storage locking, tool guard and runtime tool metadata |
| Updates | Source selection, inspection, staged builds and rollback | Default source is the EV repository, never the upstream repository |
| Native execution | Upstream changes integrated around existing platform implementations | PowerShell/cmd execution, Job Objects, ConPTY, Linux process groups and explicit tool platform support |

The declarative upstream schema describes shared tables. EV initialization then
applies its extension migrations; it is deliberately not replaced wholesale.
The embedding helper retains its limited migration contract for isolated stores.
Neither truncated summaries nor failed index rebuilds replace the previous valid state.

## Upgrade and rollback

Use the signed EV release and read its validation results before upgrading. Back up
the executable, matching static resources, configuration and consistent databases.
Wait for active conversations, tools, queues and workflows to finish before stopping
the service. Never point an EV installation at an upstream source unintentionally.
Windows package installations retain the offline bundle procedure documented in
[Windows native deployment](windows-native.md). Configuration and runtime data are
not disposable build outputs.

On failure restore the matching executable and source resources. Restore a database
backup only after assessing any writes made since that backup. Schema reconciliation
adds missing structure; it does not erase EV columns or historical records.
