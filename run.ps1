# Run from the repository root so static assets and relative data paths resolve.
# Configuration paths are relative to the repository unless absolute. No existing
# config is overwritten; the service creates one from its example on first use.
[CmdletBinding()]
param(
    [string]$ConfigPath = 'config.yaml',
    [string]$BinaryPath = 'bin/cyberstrike-ai.exe'
)
$ErrorActionPreference = 'Stop'
Push-Location $PSScriptRoot
try {
    if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) { throw 'Use run.sh on Linux.' }
    if (-not (Test-Path -LiteralPath $BinaryPath -PathType Leaf)) {
        throw 'Binary missing. Run .\scripts\build-windows.ps1 first.'
    }
    $binary = (Resolve-Path -LiteralPath $BinaryPath).Path
    & $binary -config $ConfigPath
    $serviceExitCode = $LASTEXITCODE
} finally {
    Pop-Location
}
exit $serviceExitCode
