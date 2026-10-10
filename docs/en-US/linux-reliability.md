# Linux runtime reliability and containers

[中文](../zh-CN/linux-reliability.md)

## Streaming responses

Agentic streams normalize content block indices by original index and exact
content type, in first-appearance order. This prevents interleaved text,
reasoning and tool calls from being concatenated as incompatible blocks. Tool
arguments remain unchanged; malformed arguments still follow normal validation.
This does not repair network disconnections or provider billing failures.

## Reverse proxies

MCP SSE endpoints use `X-Forwarded-Proto` only when the immediate peer matches
`server.trusted_proxies`. Configure exact proxy IPs or narrow CIDRs and have the
edge overwrite the header with `http` or `https`. Multiple header values and
comma-separated chains are ignored. Empty trust lists ignore forwarding headers.
Restart after changing proxy trust. The separate MCP listener remains HTTP;
terminate public TLS at the proxy. Forwarded host/path rewriting is not provided.

## Restart recovery

Before starting workers, the application transactionally marks persisted running
batch children failed. Running queues with pending children pause; otherwise they
complete with an interruption notice. Assistant placeholder messages receive an
error event. Completed responses and pending tasks are not rewritten or replayed.
Recovery failure stops startup instead of serving inconsistent task state.
One application process must own each database. Back up the database before
upgrading; recovery changes existing task records but adds no schema migration.
Review partial results before resuming or retrying interrupted tasks.

## Docker Compose

Requirements: Linux Docker Engine and the Compose plugin. From the repository root:

```sh
docker compose config --quiet
docker compose up -d --build
docker compose ps
docker compose logs --tail 100 cyberstrike
```

The service is available at `http://127.0.0.1:8080`. Production access must use a
TLS reverse proxy. The image runs as UID/GID 10001; Compose drops capabilities and
sets no-new-privileges. Raw-socket scanners requiring extra capabilities are not
enabled by this deployment profile. Additional tool dependencies are not bundled.

On first start, `/app/runtime/config.yaml` is initialized from the container
template with internal TLS disabled and absolute resource paths. Existing config
is never overwritten. Set provider credentials through Settings or environment
references in the saved config. Do not publish configuration or bootstrap logs.
The directory volume supports atomic Settings saves; do not replace it with a
single-file bind mount. Data, logs, uploads, workspace, knowledge and skills have
separate named volumes. Custom role/tool files require explicit mounts; packaged
roles and tools otherwise update with the image.

```sh
# Stop before taking a consistent database/configuration backup.
docker compose stop
# Back up the named volumes with your normal backup tooling, then restart.
docker compose start
```

Do not use `docker compose down -v` for an upgrade: it deletes persistent volumes.
For rollback, stop the service, restore matching image/config/data backups and
restart. Database restoration discards records written after the backup. If you
enable application TLS or change its port, update the Compose healthcheck.

The container workflow builds and smoke-tests Linux amd64 images. Stable GitHub
Release publication can publish a versioned image to this repository's GHCR
namespace after verifying its commit belongs to main. Manual workflow runs only
validate the image. No image is published by a PR, and this does not deploy hosts.
No floating latest tag or arm64 validation is provided by this workflow.

## Knowledge index resume and recovery

Indexing embeds all chunks before replacing an item's vectors and completion
manifest in one SQLite transaction. Provider errors, cancellation, insert errors
and database source changes retain the previous vectors. Previously indexed
content may therefore remain searchable until a replacement succeeds.

The additive `knowledge_index_state` table stores item ID, source/config hashes,
chunk count, model and dimension. Source edits invalidate its marker. Resume
checks missing/duplicate/out-of-range chunks and model/dimension metadata; model,
endpoint, chunking and sub-index changes require reindexing. With
`prefer_source_file`, resume rereads files to detect changed content. File edits
after that read are detected on the next resume, not atomically with SQLite.

Legacy indexes have no completeness marker and are rebuilt once when you request
resume; this can consume embedding quota. This change does not force a full legacy rebuild on startup. Existing startup
behavior still indexes new/changed items and initializes empty indexes. Failed items remain eligible for retry. Empty content gets a valid
zero-chunk marker. This is an integrity check of indexing completion, not a
cryptographic audit of every stored vector. Back up both knowledge and conversation
databases/configuration while stopped before upgrading.

## Automated upgrade regression

The Linux container workflow builds the current image and a pinned EV v1.7.22
baseline. `tests/container/upgrade_rollback.py` verifies actual authenticated
Settings saves and a stored conversation, upgrades with all seven volumes,
recreates the new container, then restores offline volume snapshots and starts
the baseline. It verifies UID 10001 and the restored setting/data at each stage.
Tests use generated fixtures and do not invoke AI providers. Temporary containers
and volumes are removed afterward; bootstrap credentials are never printed.
This covers the pinned baseline and Linux amd64, not every historical migration.

```sh
python3 tests/container/upgrade_rollback.py \
  --current cyberstrike-ai-ev:check --baseline cyberstrike-ai-ev:baseline
```

The historical baseline has a Settings-save YAML-node panic. The fixture seeds
its initial setting offline; the upgraded image must save through the real API.
The current handler fixes the document-versus-mapping node error and preserves
the tool guard rules when saving other settings.

## Settings persistence and cancellation

Settings updates are staged separately and validated before saving. The main
configuration and its backup use private (`0600`) temporary files, file sync and
atomic rename. Runtime settings, approval defaults and MCP side effects are only
published after a successful save. A backup failure rejects the update. Symlink
configuration destinations are rejected; use regular writable files. Container
installations should mount the runtime directory, not only a single config file,
so sibling temporary files and rename are supported.

Tool YAML changes are prepared before writing. If a subsequent write fails,
already replaced files are rolled back and rollback failures are logged. This is
not a crash-atomic transaction across several files: retain deployment backups
for power-loss recovery. Atomic rename prevents readers from seeing a partially
written individual file; it does not guarantee persistence across every hardware
or filesystem failure.

Knowledge indexing responds to cancellation while waiting for the index slot,
RPM quota, fixed delay or retry backoff. Every retry consumes the configured
throttle budget. Cancellation stops queued work before a new embedding request;
a request already sent remains subject to the provider's cancellation behavior.
These changes do not force reindexing or alter existing embeddings.

Linux regression checks:

```sh
go test ./internal/handler ./internal/knowledge
go test -race ./internal/knowledge
```

## Avoiding unavailable tool calls

Enabled local tools whose executables cannot be resolved are omitted from MCP
registration, instead of attaching an unavailable warning to an advertised tool.
Their configuration remains enabled. Install the dependency in the managed tool
runtime or PATH, then apply/reload the configuration or restart to register it.
No command is executed by the availability check. Disabled tools stay disabled;
internal tools do not require an external executable. A dependency removed after
registration is still checked at execution time.

This reduces unnecessary tool schemas and calls, but does not validate Python
imports, remote reachability, credentials, provider quota or target behavior.
Monitor history is retained; old failures are not evidence of a new failure.
