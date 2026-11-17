[CmdletBinding()]
param(
    [string]$Version = $env:HWRESET_VERSION,
    [string]$InstallDir = $env:HWRESET_INSTALL_DIR,
    [string]$ReleaseBaseUrl = $env:HWRESET_RELEASE_BASE_URL,
    [switch]$NoPathUpdate
)
$ErrorActionPreference = 'Stop'
$repo = 'guajun/hardware-reset-cli'
if (-not [Environment]::Is64BitOperatingSystem) { throw 'Release binaries require a 64-bit Windows OS.' }
$machine = $env:PROCESSOR_ARCHITEW6432
if (-not $machine) { $machine = $env:PROCESSOR_ARCHITECTURE }
switch ($machine.ToUpperInvariant()) {
    'ARM64' { $arch = 'arm64' }
    'AMD64' { $arch = 'amd64' }
    default { throw "Unsupported Windows architecture: $machine" }
}
if (-not $InstallDir) { $InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\hardware-reset-cli\bin' }
$InstallDir = [IO.Path]::GetFullPath($InstallDir)
$headers = @{ 'User-Agent' = 'hardware-reset-cli-installer' }
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
if (-not $Version) {
    $release = Invoke-RestMethod -Headers $headers -Uri "https://api.github.com/repos/$repo/releases/latest"
    $Version = $release.tag_name
}
$Version = $Version -replace '^v', ''
if ($Version -notmatch '^[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?$') { throw 'Expected a release version such as 0.1.0 or v0.1.0.' }
if (-not $ReleaseBaseUrl) { $ReleaseBaseUrl = "https://github.com/$repo/releases/download/v$Version" }
$asset = "hardware-reset-cli-$Version-windows-$arch.zip"
$temporary = Join-Path ([IO.Path]::GetTempPath()) "hardware-reset-cli-$([Guid]::NewGuid())"
New-Item -ItemType Directory -Path $temporary -Force | Out-Null
try {
    $archive = Join-Path $temporary $asset
    $manifest = Join-Path $temporary 'SHA256SUMS'
    Invoke-WebRequest -UseBasicParsing -Headers $headers -Uri "$ReleaseBaseUrl/$asset" -OutFile $archive
    Invoke-WebRequest -UseBasicParsing -Headers $headers -Uri "$ReleaseBaseUrl/SHA256SUMS" -OutFile $manifest
    $pattern = '^([0-9a-fA-F]{64})  ' + [regex]::Escape($asset) + '$'
    $entries = @(Get-Content -LiteralPath $manifest | Where-Object { $_ -match $pattern })
    if ($entries.Count -ne 1) { throw 'Missing or ambiguous archive checksum.' }
    $expected = [regex]::Match($entries[0], $pattern).Groups[1].Value
    $hasher = [Security.Cryptography.SHA256]::Create()
    $stream = [IO.File]::OpenRead($archive)
    try { $actual = [BitConverter]::ToString($hasher.ComputeHash($stream)).Replace('-', '') }
    finally { $stream.Dispose(); $hasher.Dispose() }
    if ($actual -ne $expected) { throw 'Archive checksum mismatch; nothing installed.' }
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [IO.Compression.ZipFile]::OpenRead($archive)
    try {
        $entries = @($zip.Entries | Where-Object FullName -CEQ 'hardware-reset-cli.exe')
        if ($entries.Count -ne 1) { throw 'Archive contains no unique hardware-reset-cli.exe.' }
        $binary = Join-Path $temporary 'hardware-reset-cli.exe'
        [IO.Compression.ZipFileExtensions]::ExtractToFile($entries[0], $binary, $true)
    } finally { $zip.Dispose() }
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    Copy-Item -LiteralPath $binary -Destination (Join-Path $InstallDir 'hardware-reset-cli.exe') -Force
    Write-Host "Installed hardware-reset-cli $Version to $InstallDir"
    if (-not $NoPathUpdate) {
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        $parts = @($userPath -split ';' | Where-Object { $_ })
        if ($parts -notcontains $InstallDir) {
            [Environment]::SetEnvironmentVariable('Path', (($parts + $InstallDir) -join ';'), 'User')
            Write-Host 'Added the install directory to user PATH. Open a new terminal to use it.'
        }
        if (($env:PATH -split ';') -notcontains $InstallDir) { $env:PATH = "$InstallDir;$env:PATH" }
    }
} finally {
    Remove-Item -LiteralPath $temporary -Recurse -Force -ErrorAction SilentlyContinue
}
