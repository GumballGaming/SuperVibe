<#
.SYNOPSIS
    Builds SuperVibe for Windows (amd64 and/or arm64) with the app icon embedded.

.DESCRIPTION
    Icon, manifest and version info ship as pre-built Windows resource objects:

        build\windows\SuperVibe-res_windows_amd64.syso
        build\windows\SuperVibe-res_windows_arm64.syso

    Both are generated from build\appicon.png (-> build\windows\icon.ico),
    build\windows\wails.exe.manifest and build\windows\info.json by:

        go -C tools/syso run .

    which mirrors what `wails build` does internally, so the icon a plain
    `go build` produces is identical to the packaged one.

    Why they are not simply left in the repo root: the Go linker only sees
    objects in the package root, and it auto-selects by architecture - a file
    named *_windows_arm64.syso is ignored by an amd64 link and vice versa. But
    linking two .rsrc sections fails with "too many .rsrc sections", which is
    exactly what happens if one of these sits next to the <Name>-res.syso that
    Wails writes for the duration of `wails build`. So the matching object gets
    copied in, compiled, and removed again.

.EXAMPLE
    .\build.ps1                  # amd64 + arm64 -> build\bin
    .\build.ps1 -Arch arm64      # a single architecture
    .\build.ps1 -Wails           # package through `wails build` (its own syso)
    .\build.ps1 -RegenSyso       # rebuild the .syso objects from appicon.png first
#>
[CmdletBinding()]
param(
    [ValidateSet('amd64', 'arm64', 'all')]
    [string] $Arch = 'all',

    # Package through `wails build` instead of plain `go build`. Wails builds the
    # frontend itself and writes its own resource object, so nothing is copied.
    [switch] $Wails,

    # Regenerate the .syso objects from build\windows\icon.ico / wails.exe.manifest
    # / info.json before building. Run this after replacing build\appicon.png
    # once the new build\windows\icon.ico is in place.
    [switch] $RegenSyso,

    [string] $OutDir = 'build\bin',

    # Reuse the existing frontend bundle instead of rebuilding it.
    [switch] $SkipFrontend,

    [switch] $Cgo
)

$ErrorActionPreference = 'Stop'

$expectedMachine = @{ amd64 = '0x8664'; arm64 = '0xAA64' }

# go, wails and vite all report progress on stderr. With
# $ErrorActionPreference='Stop' PowerShell turns that stream into a terminating
# error even when the tool exits 0, so run them relaxed and judge the exit code
# instead. The code lands in $script:nativeExit rather than being returned:
# PowerShell functions return everything written to the pipeline, and that would
# include the tool's own output.
$script:nativeExit = 0

function Invoke-Native([scriptblock] $Command) {
    $previous = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        & $Command
        $script:nativeExit = $LASTEXITCODE
    }
    finally { $ErrorActionPreference = $previous }
}

# Wails' own object plus any copy left behind by an interrupted build.
function Reset-Syso([string] $root) {
    Get-ChildItem -Path $root -Filter '*res*.syso' -File -ErrorAction SilentlyContinue |
        Remove-Item -Force -ErrorAction SilentlyContinue
}

function Get-PEMachine([string] $path) {
    $fs = [System.IO.File]::OpenRead($path)
    try {
        $buf = New-Object byte[] 4096
        [void] $fs.Read($buf, 0, 4096)
        $pe = [System.BitConverter]::ToUInt32($buf, 0x3C)
        '0x{0:X4}' -f [System.BitConverter]::ToUInt16($buf, $pe + 4)
    }
    finally { $fs.Close() }
}

Push-Location $PSScriptRoot
try {
    $targets = if ($Arch -eq 'all') { @('amd64', 'arm64') } else { @($Arch) }

    if ($RegenSyso) {
        Write-Host 'Regenerating resource objects...' -ForegroundColor Cyan
        Invoke-Native { go -C tools/syso run . }
        if ($script:nativeExit -ne 0) { throw 'syso generation failed' }
    }

    New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
    $env:GOOS = 'windows'
    # No mingw on this machine; Wails' WebView2 loader is pure Go, so cgo buys
    # nothing here and only breaks the cross-arch link.
    $env:CGO_ENABLED = if ($Cgo) { '1' } else { '0' }

    foreach ($target in $targets) {
        Reset-Syso $PSScriptRoot
        $out = Join-Path $OutDir "SuperVibe-windows-$target.exe"

        if ($Wails) {
            Write-Host "wails build windows/$target -> $out" -ForegroundColor Cyan
            Invoke-Native { wails build -platform "windows/$target" -o (Split-Path $out -Leaf) }
            if ($script:nativeExit -ne 0) { throw "wails build failed for $target" }
        }
        else {
            $source = Join-Path 'build\windows' "SuperVibe-res_windows_$target.syso"
            if (-not (Test-Path $source)) {
                throw "missing $source - run: go -C tools/syso run ."
            }
            if (-not $SkipFrontend) {
                Invoke-Native { bun run --cwd frontend build }
                if ($script:nativeExit -ne 0) { throw 'frontend build failed' }
            }

            $staged = "SuperVibe-res_windows_$target.syso"
            Copy-Item $source ".\$staged" -Force
            try {
                Write-Host "go build windows/$target -> $out" -ForegroundColor Cyan
                $env:GOARCH = $target
                Invoke-Native { go build -o $out . }
                if ($script:nativeExit -ne 0) { throw "go build failed for $target" }
            }
            finally {
                Remove-Item ".\$staged" -Force -ErrorAction SilentlyContinue
            }
        }

        $machine = Get-PEMachine $out
        $ok = $machine -eq $expectedMachine[$target]
        $colour = if ($ok) { 'Green' } else { 'Red' }
        Write-Host ("  {0}  machine={1} (expected {2})  {3:N1} MB" -f (
            Split-Path $out -Leaf), $machine, $expectedMachine[$target], ((Get-Item $out).Length / 1MB)) -ForegroundColor $colour
        if (-not $ok) { throw "$out is not a $target binary" }
    }
}
finally {
    Reset-Syso $PSScriptRoot
    Pop-Location
}
