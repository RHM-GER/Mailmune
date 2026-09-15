$ErrorActionPreference = "Stop"

# Leftover processes from a previous dev run keep a lock on the sidecar that
# tauri-build copies to target\debug\spam-agent.exe. When that file is locked,
# copy_binaries fails with "Zugriff verweigert" (Os code 5, PermissionDenied)
# and the whole build aborts. Stop Mailmune's own dev binaries first so the
# fresh copy always succeeds. This only ever targets mailmune/spam-agent.
foreach ($staleName in @("mailmune", "spam-agent*")) {
    Get-Process -Name $staleName -ErrorAction SilentlyContinue |
        Stop-Process -Force -ErrorAction SilentlyContinue
}
Start-Sleep -Milliseconds 400

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
