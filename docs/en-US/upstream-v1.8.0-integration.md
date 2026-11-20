# EV v1.8.0 integration and migration

## Runtime and retained behavior

The upstream v1.8.0 baseline uses Pi 1.5.2. Go retains permissions, projects, MCP execution, evidence and persistence; an isolated Node worker handles model sessions. Go 1.26.9 and Node.js >=22.19 with npm are required. A standalone Go executable is insufficient.

EV retains fixed proxy routing and unsupported-tool blocking, SRC tool restrictions shared with child agents, MCP failure cooldowns, task indexing and evidence links, vulnerability deduplication, severity assessments and rating history, and permission-scoped Token statistics. Provider-safe tool aliases are translated back to original MCP identities before host authorization and execution. Historic Eino usage remains queryable and renderable.

## Build and configuration

From the repository root, prepare the worker before building the server:

```sh
go run ./cmd/build-runtime -root runtime/pi
go build -mod=readonly -trimpath -o cyberstrike-ai ./cmd/server
```

The configuration example uses `agent_runtime.engine: pi`, `multi_agent.runtime` and `multi_agent.skills`. Legacy configuration keys are migrated when loaded; settings saves use native keys. Preserve configuration, databases, installed tools, skills and uploads during deployment. Never replace a private configuration with the example.

Docker includes Node and a prepared worker at `/opt/cyberstrike/pi`, outside the persisted `/app/runtime` configuration volume. Existing runtime volumes therefore do not hide the worker. Windows builds prepare the worker before compiling; offline Windows packages include only the matching source-addressed worker and locked dependencies, excluding session state. Node must also be installed on the destination.

Generate offline Windows worker bundles on Windows; do not reuse Linux `node_modules` for a Windows distribution.

## Upgrade and rollback

1. Finish active conversations, tools, batches and workflows before stopping the service. Eino Workflow binary checkpoints cannot resume in Pi: finish those runs or export their evidence and create new tasks.
2. Build and validate in a separate directory. Retain a complete backup of the existing executable, sources, configuration, databases (including SQLite sidecars), installed tools and runtime assets. Copy databases only with the service stopped or through SQLite's backup API.
3. Update application resources and the prepared Pi worker together. Keep existing session/evidence files. Restart using the configured service manager, then check HTTPS, authentication, database integrity and a small authorized task.
4. On failure, stop the new service and restore the matching prior executable, sources, configuration and database backup together. Source rollback selects the matching previous Pi bundle; do not overwrite a running worker's dependency directory. Restoring a database snapshot discards later writes, so preserve any new evidence first.

Keep the latest two complete deployment backups. Existing published Tags and Releases remain unchanged; the new EV release must point to a verified main commit after its dev PR passes CI.

## Verification boundaries

Linux validation uses disposable databases, mock model responses and local listeners; it does not use paid model APIs or public proxies. Regression covers role boundaries, proxy policy, task evidence, severity history, deduplication, Token persistence, worker lifecycle and native service restart persistence. Provider availability and real-target behavior are outside these fixtures. Windows CI and container build remain release gates when local execution is unavailable.
