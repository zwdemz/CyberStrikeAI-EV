# Native Windows service (stage three)

Stage one covers the core service on Windows x64. Linux startup and container deployment remain unchanged. Command execution, interactive terminals and tool packaging are separate stages; this is not a claim that all security tools support Windows.

## Build and start

Install the Go version declared in `go.mod` and a matching GCC on PATH. SQLite requires CGO. Keep the repository resources alongside the executable; an exe alone is not a complete installation.

```powershell
.\scripts\build-windows.ps1
.\run.ps1
# Optional configuration, relative to the repository root:
.\run.ps1 -ConfigPath 'config.local.yaml'
```

The launcher sets the working directory to the repository root and preserves existing configuration. On first use the service copies the example configuration. Configure model endpoints and optional integrations separately. Follow the startup HTTPS address and change the generated administrator password after login. Do not share startup credentials. Use environment variables for secrets and Windows ACLs for configuration and data; POSIX permission bits do not establish equivalent ACL protection.

Press Ctrl+C and wait for exit before backing up. Retain configuration, databases (including associated WAL files), knowledge files, uploads, skills and required workspace data. For rollback, restore a consistent stopped-service backup of the executable, resources, configuration and data together. The launcher is a foreground console launcher, not a Windows service installer. Configuration startup errors return a nonzero exit code.

## Verification

```powershell
python -m pip install PyYAML==6.0.2
python tests/native/service_smoke.py --binary bin/cyberstrike-ai.exe
python tests/native/service_smoke.py --binary bin/cyberstrike-ai.exe --https
go test -p 2 ./internal/config ./internal/database ./internal/knowledge ./internal/processguard -count=1 -timeout 5m
```

The integration fixture runs the real service in a temporary path containing spaces and Unicode. It checks authentication, invalid inputs, settings, a deterministic local-model chat, project/conversation/asset/vulnerability/knowledge persistence, local embedding indexing, graceful restart and database integrity. No public model or scanning tool is invoked. The same fixture runs in Linux CI.

Windows filesystem capacity reporting, tool installers and release packaging remain outside stage one. A passed local-model fixture does not establish compatibility with every external provider.

## Stage two: execution and interactive terminals

An unspecified shell selects PowerShell on Windows and sh on Linux. Explicit cmd.exe and installed pwsh.exe are supported. Commands must use the selected shell syntax; POSIX scripts are not translated. Non-interactive PowerShell output uses UTF-8; cmd and external executables may retain their own code pages. Use managed background execution instead of a trailing POSIX &.

Each Windows command owns a Job for descendant cleanup and independent cancellation, nested under the task Job to retain aggregate resource limits. Guardians count toward process limits. Containment failures never fall back to uncontained execution.

ConPTY requires Windows 10 1809+ and supports input, resize and disconnect cleanup, retaining existing authentication and same-origin checks. Unavailable ConPTY returns an error; ordinary execution remains available. SSE streams long output without newlines in chunks; display is capped at 256 KiB while pipes continue to drain.

```powershell
go test -p 1 ./tests/internal/security ./tests/internal/handler ./tests/internal/terminalpty -run 'TestWindows|TestNativeConsole' -count=1 -timeout 2m
go test ./internal/multiagent -run TestEinoStreamingShellWrap -count=1 -timeout 2m
```

Tests cover Unicode/quotes, explicit cmd, Eino execution, descendants, independent cancellation, long output, request cancellation, console input/resize and WebSocket disconnect cleanup. Stage three adds the packaging and installation workflow below; not every recipe is Windows-compatible.

## Stage three: tool installation and platform status

Recipes accept optional `platforms: [linux, windows, darwin]` GOOS declarations. Omitted declarations retain legacy behavior and report `unverified`. Explicitly excluded platforms cannot register or execute the tool. Only the native exec recipe is newly certified here; validate other recipes and dependencies before declaring support.

Local entries in authenticated `GET /api/config/tools` and `GET /api/config` include `runtime` with platform, platforms, status and available. States are disabled, unsupported, missing, unverified and ready. Ready means a matching declaration and resolvable executable, not a functional health check. Paths are not disclosed. External MCP tools do not use local platform classification.

Download a standalone Windows executable from its official release channel and obtain its SHA-256 from a trusted source:

```powershell
.\scripts\install-windows-tool.ps1 -Name ffuf -Source 'C:\Downloads\ffuf.exe' -Sha256 '<64-character SHA256>'
```

Supported standalone names: nuclei, ffuf, subfinder, httpx, gobuster and waybackurls. Installation targets tools/runtime/bin without changing PATH, downloading, executing or overwriting tools. Reload tools or restart afterwards. Configure templates, certificates, licenses and additional dependencies separately. Installing an executable does not certify recipe compatibility. Python, Java, drivers and Linux-specific tools need separate provisioning.

## Distribution and offline upgrade

Packaging/upgrading requires Python 3.11+; running the server does not. Native service CI produces a tested Windows ZIP plus SHA-256 in the windows-native-distribution artifact (14-day retention). It does not publish Tags/Releases. Only committed resources and the build artifact are included; live configuration, databases, uploads and installed tools are excluded.

```powershell
.\scripts\build-windows.ps1
python scripts/windows_bundle.py build --binary bin/cyberstrike-ai.exe --output dist/CyberStrikeAI-EV-windows-amd64.zip
python scripts/windows_bundle.py verify --package dist/CyberStrikeAI-EV-windows-amd64.zip --sha256 '<trusted SHA256>'
python scripts/windows_bundle.py apply --package dist/CyberStrikeAI-EV-windows-amd64.zip --sha256 '<trusted SHA256>' --target 'C:\CyberStrikeAI-EV'
```

Resources come from Git HEAD; build the binary from that same clean revision, preferably using its CI artifact. Checksums establish integrity, not publisher identity. Obtain them through a trusted channel. Wait for active work to finish, stop the service and back up the complete installation before upgrade. The script never stops or starts services and rejects a busy executable.

Configuration, data and unmanaged files remain untouched. Locally modified managed resources cause a conflict before any replacement; save customizations separately and plan their merge. An older source installation without a manifest is not overwritten if files differ: install in a fresh directory and migrate configuration/data while stopped. Start using run.ps1.

Failed replacement rolls back changed files. Successful upgrades retain a sibling *-backup-* managed-file backup, which does not include databases. If starting a new version has migrated databases, rollback requires the complete pre-upgrade installation backup. Delete old backups manually only after verifying the new version.

The .upgrade.lock file serializes upgrades; run.ps1 refuses startup while it exists. After an interrupted upgrade, confirm the upgrader has exited and inspect/restore the managed-file backup before manually removing a stale lock. Do not bypass the launcher to start the executable during upgrade.
