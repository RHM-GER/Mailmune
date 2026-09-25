# Startet die Offline-Evaluierung der Mailmune-Regeln gegen das lokale
# HuggingFace-Korpus (MIT) abgekoppelt im Hintergrund und schreibt das
# Ergebnis nach .tools/corpus/eval.log. Aufruf:
#   powershell -NoProfile -ExecutionPolicy Bypass -File scripts\run-eval.ps1
$root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$tool = Join-Path $root ".tools\mltool.exe"
$corpus = Join-Path $root ".tools\corpus\df.csv"
$log = Join-Path $root ".tools\corpus\eval.log"
& $tool eval $corpus *> $log
"eval beendet mit Exit-Code $LASTEXITCODE" | Add-Content -Path $log
