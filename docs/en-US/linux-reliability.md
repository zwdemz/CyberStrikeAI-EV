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
