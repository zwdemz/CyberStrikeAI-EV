# Build the native Windows service with the SQLite CGO driver.
# Requires Go (version from go.mod) and a compatible GCC on PATH; failures abort.
[CmdletBinding()]
param([string]$OutputPath = 'bin/cyberstrike-ai.exe')
$ErrorActionPreference = 'Stop'
$repositoryRoot = Split-Path $PSScriptRoot -Parent
$previousCgo = $env:CGO_ENABLED
$previousOS = $env:GOOS
Push-Location $repositoryRoot
try {
    if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) { throw 'Run this script on Windows.' }
    Get-Command go -ErrorAction Stop | Out-Null
    Get-Command gcc -ErrorAction Stop | Out-Null
    $outputFile = if ([IO.Path]::IsPathRooted($OutputPath)) {
        [IO.Path]::GetFullPath($OutputPath)
    } else {
        [IO.Path]::GetFullPath((Join-Path $repositoryRoot $OutputPath))
    }
    New-Item -ItemType Directory -Force -Path (Split-Path $outputFile -Parent) | Out-Null
    $env:CGO_ENABLED = '1'
    $env:GOOS = 'windows'
    & go build -mod=readonly -trimpath -o $outputFile ./cmd/server
    if ($LASTEXITCODE -ne 0) { throw "Native build failed (exit $LASTEXITCODE)." }
    Write-Output "Built $outputFile"
} finally {
    $env:CGO_ENABLED = $previousCgo
    $env:GOOS = $previousOS
    Pop-Location
}
