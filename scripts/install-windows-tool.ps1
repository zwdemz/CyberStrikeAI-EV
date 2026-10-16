# Install a previously downloaded standalone native tool; never execute it.
[CmdletBinding()]
param(
    [Parameter(Mandatory=$true)][ValidateSet('nuclei','ffuf','subfinder','httpx','gobuster','waybackurls')][string]$Name,
    [Parameter(Mandatory=$true)][string]$Source,
    [Parameter(Mandatory=$true)][ValidatePattern('^[a-fA-F0-9]{64}$')][string]$Sha256,
    [string]$InstallRoot = (Split-Path $PSScriptRoot -Parent)
)
$ErrorActionPreference = 'Stop'
# Hash a local file without depending on module auto-loading across PS versions.
function Get-ContentSha256([string]$Path) {
    $stream = [IO.File]::OpenRead($Path)
    $hash = [Security.Cryptography.SHA256]::Create()
    try { return [BitConverter]::ToString($hash.ComputeHash($stream)).Replace('-', '') }
    finally { $hash.Dispose(); $stream.Dispose() }
}
if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) { throw 'Windows only.' }
$sourceFile = Get-Item -LiteralPath $Source
if ($sourceFile.PSIsContainer -or $sourceFile.Extension -ne '.exe') { throw 'Source must be an executable file.' }
if ((Get-ContentSha256 $sourceFile.FullName) -ne $Sha256) { throw 'SHA256 mismatch; nothing installed.' }
$root = (Resolve-Path -LiteralPath $InstallRoot).Path
$recipe = Join-Path $root "tools/$Name.yaml"
if (-not (Test-Path -LiteralPath $recipe -PathType Leaf)) { throw 'Tool recipe missing from installation.' }
$destination = Join-Path $root 'tools/runtime/bin'
New-Item -ItemType Directory -Force -Path $destination | Out-Null
# Reject links in every existing parent to avoid writing outside the installation.
$parent = Get-Item -LiteralPath $destination
while ($null -ne $parent) {
    if ($parent.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Linked installation directories are unsupported.' }
    $parent = $parent.Parent
}
$target = Join-Path $destination "$Name.exe"
if (Test-Path -LiteralPath $target) { throw 'Tool already installed; back it up and remove it explicitly before replacement.' }
$temporary = Join-Path $destination ([Guid]::NewGuid().ToString() + '.tmp')
try {
    Copy-Item -LiteralPath $sourceFile.FullName -Destination $temporary
    if ((Get-ContentSha256 $temporary) -ne $Sha256) { throw 'Source changed during copying.' }
    $stream = [IO.File]::OpenRead($temporary)
    $reader = [IO.BinaryReader]::new($stream)
    try {
        if ($reader.ReadUInt16() -ne 0x5A4D -or $stream.Length -lt 64) { throw 'Not a Windows PE executable.' }
        $stream.Position = 60
        $offset = $reader.ReadUInt32()
        if ($offset -gt $stream.Length - 6) { throw 'Invalid PE header.' }
        $stream.Position = $offset
        if ($reader.ReadUInt32() -ne 0x4550 -or $reader.ReadUInt16() -ne 0x8664) { throw 'An amd64 Windows executable is required.' }
    } finally { $reader.Dispose(); $stream.Dispose() }
    [IO.File]::Move($temporary, $target)
    Write-Output "Installed $Name under tools/runtime/bin. Reload tools or restart to refresh availability. Recipe compatibility is not certified by installation."
} finally {
    if (Test-Path -LiteralPath $temporary) { Remove-Item -LiteralPath $temporary -Force }
}
