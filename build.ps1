param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^\d+\.\d+\.\d+$')]
    [string]$Version
)

$ErrorActionPreference = "Stop"
$Root = $PSScriptRoot
$Dist = Join-Path $Root "dist"
$Binary = Join-Path $Dist "music-ops.exe"

function Invoke-Checked {
    param([string]$Command, [string[]]$Arguments)
    & $Command @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$Command $($Arguments -join ' ') failed with exit code $LASTEXITCODE"
    }
}

$major, $minor, $patch = $Version.Split(".")
$env:MUSIC_OPS_VERSION = $Version
$env:MUSIC_OPS_VERSION_MAJOR = $major
$env:MUSIC_OPS_VERSION_MINOR = $minor
$env:MUSIC_OPS_VERSION_PATCH = $patch

New-Item -ItemType Directory -Force -Path $Dist | Out-Null
Push-Location $Root
try {
    Invoke-Checked "go" @("generate", "./cmd/music-ops")
    Invoke-Checked "go" @("build", "-trimpath", "-ldflags", "-s -w -X main.version=$Version", "-o", $Binary, "./cmd/music-ops")
}
finally {
    Pop-Location
}

$hash = (Get-FileHash -Path $Binary -Algorithm SHA256).Hash.ToLowerInvariant()
Set-Content -Path (Join-Path $Dist "checksums.txt") -Value "$hash  music-ops.exe" -NoNewline
Write-Host "built $Binary ($Version)"
