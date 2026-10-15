# Native Windows service (stage one)

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

Windows filesystem capacity reporting, full command/terminal behavior, tool installers and release packaging remain outside stage one. A passed local-model fixture does not establish compatibility with every external provider.
