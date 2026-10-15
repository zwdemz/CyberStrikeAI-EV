# Native Windows service (stage two)

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

Tests cover Unicode/quotes, explicit cmd, Eino execution, descendants, independent cancellation, long output, request cancellation, console input/resize and WebSocket disconnect cleanup. Tool installation, capability lists and release packaging remain stage three work; not every recipe is Windows-compatible.
