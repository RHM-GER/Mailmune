$ErrorActionPreference = "Stop"

$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$outputDirectory = Join-Path $projectRoot "apps\desktop\src-tauri\binaries"
$outputPath = Join-Path $outputDirectory "spam-agent-x86_64-pc-windows-msvc.exe"
$portableGo = Join-Path $projectRoot ".tools\go\bin\go.exe"
$goCache = Join-Path $projectRoot ".tools\gocache"
$goModuleCache = Join-Path $projectRoot ".tools\gomodcache"

if (Test-Path -LiteralPath $portableGo) {
    $goExecutable = $portableGo
} else {
    $goExecutable = (Get-Command go -ErrorAction Stop).Source
}

New-Item -ItemType Directory -Path $outputDirectory -Force | Out-Null
New-Item -ItemType Directory -Path $goCache -Force | Out-Null
New-Item -ItemType Directory -Path $goModuleCache -Force | Out-Null
$env:CGO_ENABLED = "0"
$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:GOCACHE = $goCache
$env:GOMODCACHE = $goModuleCache

Push-Location $projectRoot
try {
    & $goExecutable build -buildvcs=false -trimpath -ldflags "-s -w" -o $outputPath .\cmd\spam-agent
    if ($LASTEXITCODE -ne 0) {
        throw "Der Go-Sidecar konnte nicht gebaut werden."
    }
} finally {
    Pop-Location
}

Write-Host "Sidecar erstellt: $outputPath"
