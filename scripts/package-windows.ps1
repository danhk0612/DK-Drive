# Requires Windows PowerShell 5.1 or PowerShell 7 on Windows.
[CmdletBinding()]
param()
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
if ($env:OS -ne 'Windows_NT') { throw 'Run this script on Windows.' }
$Root = Split-Path -Parent $PSScriptRoot
$OldGOOS = $env:GOOS
$OldGOARCH = $env:GOARCH
$OldCGO = $env:CGO_ENABLED
Push-Location $Root
$Stage = $null
try {
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'
    $Version = (go run ./cmd/dkdrive --version)
    if ($LASTEXITCODE -ne 0) { throw 'Version lookup failed.' }
    $Version = "$Version".Trim()
    if ($Version -notmatch '^\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?$') { throw 'Invalid application version.' }
    $Commit = git rev-parse HEAD
    if ($LASTEXITCODE -ne 0) { throw 'Git revision lookup failed.' }
    $Changes = @(git status --porcelain)
    if ($LASTEXITCODE -ne 0) { throw 'Git status failed.' }
    $Toolchain = go version
    if ($LASTEXITCODE -ne 0) { throw 'Go version lookup failed.' }
    $Dist = Join-Path $Root 'dist'
    New-Item -ItemType Directory -Path $Dist -Force | Out-Null
    $Stage = Join-Path $Dist ([guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $Stage | Out-Null
    $Exe = Join-Path $Stage 'dkdrive.exe'
    go build -trimpath -buildvcs=true -ldflags='-H=windowsgui' -o $Exe ./cmd/dkdrive
    if ($LASTEXITCODE -ne 0) { throw 'Windows build failed.' }
    # Bundle the byte-for-byte official MSI; never extract/repackage its DLL/driver.
    $WinFsp = Get-Content -LiteralPath 'internal/prerequisite/winfsp.json' -Raw | ConvertFrom-Json
    $Installer = Join-Path $Stage $WinFsp.file
    Invoke-WebRequest -Uri $WinFsp.url -OutFile $Installer
    if ((Get-FileHash -LiteralPath $Installer -Algorithm SHA256).Hash -ne $WinFsp.sha256) { throw 'WinFsp installer checksum mismatch.' }
    Copy-Item -LiteralPath 'licenses' -Destination $Stage -Recurse
    $LicenseDir = Join-Path $Stage 'licenses'
    $GoRoot = go env GOROOT
    if ($LASTEXITCODE -ne 0) { throw 'Go runtime license lookup failed.' }
    Copy-Item -LiteralPath (Join-Path $GoRoot 'LICENSE') -Destination (Join-Path $LicenseDir 'Go-LICENSE.txt')
    $Modules = @(go list -deps -f '{{if .Module}}{{if not .Module.Main}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}{{end}}' ./cmd/dkdrive | Sort-Object -Unique)
    if ($LASTEXITCODE -ne 0) { throw 'Dependency license lookup failed.' }
    $ModuleIndex = @('Bundled Go dependencies (their original license texts follow in this directory):')
    foreach ($Module in $Modules) {
        if (-not $Module.Trim()) { continue }
        $Parts = $Module.Split('|')
        $Notices = @(Get-ChildItem -LiteralPath $Parts[2] -File | Where-Object { $_.Name -match '^(LICENSE|COPYING|NOTICE|AUTHORS|PATENTS)([.-].*)?$' })
        if ($Notices.Count -eq 0) { throw "Missing dependency license: $($Parts[0])" }
        $Prefix = ($Parts[0] + '@' + $Parts[1]) -replace '[^A-Za-z0-9._@-]', '_'
        foreach ($Notice in $Notices) { Copy-Item -LiteralPath $Notice.FullName -Destination (Join-Path $LicenseDir "$Prefix-$($Notice.Name).txt") }
        $ModuleIndex += "$($Parts[0]) $($Parts[1])"
    }
    $ModuleIndex | Set-Content -LiteralPath (Join-Path $LicenseDir 'Go-DEPENDENCIES.txt') -Encoding UTF8
    Copy-Item -LiteralPath 'LICENSE' -Destination $Stage
    Copy-Item -LiteralPath 'docs/distribution.md' -Destination (Join-Path $Stage 'README.md')
    $BuildInfo = [ordered]@{
        version = $Version
        commit = "$Commit".Trim()
        modified = ($Changes.Count -gt 0)
        toolchain = "$Toolchain".Trim()
        target = 'windows/amd64'
        winfsp = $WinFsp
    }
    $BuildInfo | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $Stage 'build-info.json') -Encoding UTF8
    $Hash = (Get-FileHash -LiteralPath $Exe -Algorithm SHA256).Hash.ToLowerInvariant()
    "$Hash  dkdrive.exe" | Set-Content -LiteralPath (Join-Path $Stage 'SHA256SUMS.txt') -Encoding ASCII
    # Fixed file times prevent wall-clock time from changing ZIP entries.
    $Files = @(Get-ChildItem -LiteralPath $Stage | Sort-Object Name)
    foreach ($File in (Get-ChildItem -LiteralPath $Stage -Recurse -File)) { $File.LastWriteTime = [datetime]'2000-01-01T00:00:00' }
    $Archive = Join-Path $Dist "DK-Drive-$Version-windows-amd64.zip"
    Compress-Archive -LiteralPath $Files.FullName -DestinationPath $Archive -Force
    $ZipHash = (Get-FileHash -LiteralPath $Archive -Algorithm SHA256).Hash.ToLowerInvariant()
    "$ZipHash  $(Split-Path -Leaf $Archive)" | Set-Content -LiteralPath "$Archive.sha256" -Encoding ASCII
    Write-Host "Package: $Archive"
    Write-Host "SHA-256: $Archive.sha256"
}
finally {
    if ($Stage -and (Test-Path -LiteralPath $Stage)) { Remove-Item -LiteralPath $Stage -Recurse -Force }
    $env:GOOS = $OldGOOS
    $env:GOARCH = $OldGOARCH
    $env:CGO_ENABLED = $OldCGO
    Pop-Location
}
