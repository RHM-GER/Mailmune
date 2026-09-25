# Startet die Offline-Evaluierung der Mailmune-Regeln gegen das lokale
# HuggingFace-Korpus (MIT) abgekoppelt im Hintergrund und schreibt das
# Ergebnis nach .tools/corpus/eval.log. Achtung: Flags MÜSSEN vor den
# Positionsargumenten stehen (Go-flag-Parser stoppt sonst).
$root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$tool = Join-Path $root ".tools\mltool.exe"
$corpus = Join-Path $root ".tools\corpus\df.csv"
$log = Join-Path $root ".tools\corpus\eval.log"
& $tool eval --max 40000 --thresholds 0.4,0.5,0.6,0.7,0.8,0.9 $corpus *> $log
"eval beendet mit Exit-Code $LASTEXITCODE" | Add-Content -Path $log
