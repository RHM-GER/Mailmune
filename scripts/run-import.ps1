# Importiert das lokale HuggingFace-Korpus (MIT) als globale Lern-Baseline in
# die Agent-Datenbank. Läuft abgekoppelt; Ergebnis in .tools/corpus/import.log.
# Hinweis: Die Baseline wirkt nur als begrenzter Prior; bestätigtes Lernen des
# Nutzers kann sie überstimmen. Rohdaten verlassen das Gerät nicht.
$root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$tool = Join-Path $root ".tools\mltool.exe"
$db = Join-Path $env:APPDATA "de.rhmedia.mailmune\mailmune.db"
$corpus = Join-Path $root ".tools\corpus\df.csv"
$log = Join-Path $root ".tools\corpus\import.log"
& $tool import --source "https://huggingface.co/datasets/locuoco/the-biggest-spam-ham-phish-email-dataset-300000" --license "MIT" $db $corpus *> $log
"import beendet mit Exit-Code $LASTEXITCODE" | Add-Content -Path $log
