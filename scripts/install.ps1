# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 Nimote contributors
# Per-user installer. No downloaded shell scripts or execution-policy changes.
param([switch]$Help)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if ($Help) {
    Write-Output 'Usage: .\scripts\install.ps1'
    Write-Output 'From source: build with Go, then verify and install the complete bundle.'
    Write-Output 'From a distribution: verify and install without Go. Run as your normal user.'
    exit 0
}
if ($args.Count -ne 0) { throw 'Unexpected argument. Use -Help.' }
$bundleDir = Split-Path -Parent $PSScriptRoot
$hostExe = Join-Path $bundleDir 'nimotehost.exe'
if (-not ((Test-Path -LiteralPath $hostExe -PathType Leaf) -and
          (Test-Path -LiteralPath (Join-Path $bundleDir 'SHA256SUMS') -PathType Leaf))) {
    if (-not (Test-Path -LiteralPath (Join-Path $bundleDir 'tools/install/main.go') -PathType Leaf)) {
        throw 'Complete NimoteHost source or distribution required.'
    }
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        throw 'Source installation requires Go 1.26.5+. Prebuilt distributions do not need Go.'
    }
    Push-Location -LiteralPath $bundleDir
    try {
        & go run ./tools/install
        $installExitCode = $LASTEXITCODE
    } finally {
        Pop-Location
    }
    exit $installExitCode
}
& $hostExe setup
exit $LASTEXITCODE
