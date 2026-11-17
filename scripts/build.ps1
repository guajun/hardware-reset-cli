[CmdletBinding()]
param(
    [ValidateSet('Debug', 'Release', 'All')]
    [string]$Configuration = 'Debug',
    [switch]$SkipTests,
    [switch]$SkipFirmware,
    [switch]$SkipCli,
    [string]$NativeCompiler
)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$originalPath = $env:PATH
$originalSimulator = $env:HWRESET_SIM

function Add-ToolDirectory([string]$Directory) {
    if ((Test-Path -LiteralPath $Directory) -and ($env:PATH -split ';' -notcontains $Directory)) {
        $env:PATH = "$Directory;$env:PATH"
    }
}

function Invoke-Checked([string]$Executable, [string[]]$Arguments) {
    & $Executable @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Executable exited with code $LASTEXITCODE" }
}

try {
    # Only this process sees these PATH additions. No registry or user PATH edits.
    Add-ToolDirectory 'C:\Program Files\Go\bin'
    Add-ToolDirectory 'C:\Program Files\CMake\bin'
    $wingetRoot = Join-Path $env:LOCALAPPDATA 'Microsoft\WinGet\Packages'
    if (Test-Path -LiteralPath $wingetRoot) {
        foreach ($pattern in @('Kitware.CMake_*', 'Ninja-build.Ninja_*')) {
            Get-ChildItem -LiteralPath $wingetRoot -Directory -Filter $pattern | ForEach-Object {
                $packageDirectory = $_.FullName
                foreach ($executable in @('cmake.exe', 'ninja.exe')) {
                    Get-ChildItem -LiteralPath $packageDirectory -Recurse -File -Filter $executable |
                        ForEach-Object { Add-ToolDirectory $_.DirectoryName }
                }
            }
        }
    }
    $armRoot = 'C:\Program Files (x86)\Arm GNU Toolchain arm-none-eabi'
    if (Test-Path -LiteralPath $armRoot) {
        Get-ChildItem -LiteralPath $armRoot -Directory | Sort-Object Name |
            ForEach-Object { Add-ToolDirectory (Join-Path $_.FullName 'bin') }
    }

    if (-not $SkipTests) {
        if (-not $NativeCompiler) {
            foreach ($name in @('cc', 'gcc', 'clang', 'cl')) {
                $tool = Get-Command $name -ErrorAction SilentlyContinue
                if ($tool) { $NativeCompiler = $tool.Source; break }
            }
        }
        if (-not $NativeCompiler) {
            $localZig = Join-Path $projectRoot '.cache/tooling/ziglang/zig.exe'
            if (Test-Path -LiteralPath $localZig) { $NativeCompiler = $localZig }
        }
        if (-not $NativeCompiler) {
            throw 'Native C compiler required for protocol tests. Pass -NativeCompiler or explicitly use -SkipTests.'
        }
        $nativeBuild = Join-Path $projectRoot 'firmware/build/host'
        $nativeArguments = @('-S', "$projectRoot/firmware/tests", '-B', $nativeBuild, '-G', 'Ninja', "-DCMAKE_C_COMPILER=$NativeCompiler")
        if ([IO.Path]::GetFileNameWithoutExtension($NativeCompiler) -eq 'zig') {
            $nativeArguments += '-DCMAKE_C_COMPILER_ARG1=cc'
        }
        Invoke-Checked 'cmake' $nativeArguments
        Invoke-Checked 'cmake' @('--build', $nativeBuild)
        Invoke-Checked 'ctest' @('--test-dir', $nativeBuild, '--output-on-failure')
        $simulatorName = 'reset_protocol_sim'
        if ($env:OS -eq 'Windows_NT') { $simulatorName += '.exe' }
        $env:HWRESET_SIM = Join-Path $nativeBuild $simulatorName
    }

    if (-not $SkipCli) {
        Push-Location (Join-Path $projectRoot 'cli')
        try {
            if (-not $SkipTests) {
                Invoke-Checked 'go' @('test', './...')
                Invoke-Checked 'go' @('vet', './...')
            }
            $output = Join-Path $projectRoot 'dist/hardware-reset-cli'
            if ($env:OS -eq 'Windows_NT') { $output += '.exe' }
            New-Item -ItemType Directory -Path (Split-Path -Parent $output) -Force | Out-Null
            Invoke-Checked 'go' @('build', '-trimpath', '-o', $output, './cmd/hardware-reset-cli')
        } finally { Pop-Location }
    }

    if (-not $SkipFirmware) {
        $presets = @($Configuration)
        if ($Configuration -eq 'All') { $presets = @('Debug', 'Release') }
        Push-Location (Join-Path $projectRoot 'firmware')
        try {
            foreach ($preset in $presets) {
                Invoke-Checked 'cmake' @('--preset', $preset)
                Invoke-Checked 'cmake' @('--build', '--preset', $preset)
            }
        } finally { Pop-Location }
    }
} finally {
    $env:PATH = $originalPath
    $env:HWRESET_SIM = $originalSimulator
}
