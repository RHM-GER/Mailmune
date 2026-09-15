# Mailmune

Mailmune ist ein lokaler IMAP-Spamfilter mit menschlicher Abnahme. Die Anwendung liest ausschließlich Metadaten und begrenzte Textauszüge, ordnet Verdachtsfälle nachvollziehbar ein und löscht niemals E-Mails.

## Aktueller Stand

- Browserfähige React-/shadcn-App-Shell nach dem Figma-Entwurf
- Go-Agent mit lokaler, token-geschützter Loopback-API und SQLite (WAL, Foreign Keys, nummerierte vorwärtslaufende Migrationen)
- UID-basierte, ausschließlich lesende IMAP-Synchronisierung mit sicherer Re-Synchronisierung bei `UIDVALIDITY`-Wechsel
- **Dauerbetrieb**: IMAP-IDLE für Live-Erkennung neuer Mails plus 10-Minuten-UID-Abgleich als Sicherheitsnetz; Reconnect mit exponentiellem Backoff und Jitter
- **Wöchentlicher KI-Tiefscan**: zur konfigurierbaren Uhrzeit (je Postfach, lokale Zeit) werden alle Mails seit dem letzten Tiefscan erneut mit dem validierten lokalen Modell geprüft; verpasste Termine holt der Agent automatisch nach
- Hintergrundscans je Konto mit Fortschritt, Abbruch, Wiederaufnahme nach Neustart und SSE-Eventstream in die UI
- Dreistufige Klassifikation:
  1. deterministische Regelpipeline mit generischen Heuristiken (maschinell erzeugte Absenderdomains, Ziffernmuster, Druck-/Lock-/Bestätigungs-/Finanzsprache, Betreff-Anomalien) und stabilen Evidence-Codes ([docs/evidence-codes.md](docs/evidence-codes.md))
  2. lokaler Naive-Bayes-Lernfilter aus bestätigten Reviews, optional verstärkt durch eine importierte Offline-Baseline (opt-in, mit Herkunft/Lizenz)
  3. optionales lokales Ollama-Modell (nur Loopback, versionierter Prompt, strikte JSON-Validierung, Fähigkeitstest, Empfehlungsliste) – zählt höchstens als eine Signalgruppe und erhält als „RAG light" ausschließlich den datenschutzsicheren Profil-Kontext aus bestätigten Reviews (diskriminative Tokens und Absenderdomains, niemals Rohtext oder vollständige Adressen, strikt je Postfach)
- **Absturzfeste Move-Zustandsmaschine**: bestätigte Verdachtsfälle wandern per atomarem UID-MOVE in `AI_SPAM_FILTER`, Fehlalarme zurück in den Ursprungsordner; idempotent, mit UIDVALIDITY-Recheck und Reconcile nach Absturz; ohne MOVE-Unterstützung wird nie verschoben
- **Kalibrierungsmessung** aus bestätigten Reviews (Precision/Recall/FPR, Kalibrierungsbuckets); automatische Verschiebung ist erst ab 20 Reviews mit ≥ 99,5 % Präzision bei Score ≥ 0,98 freischaltbar
- Echte Dashboard-Statistiken (`GET /v1/stats`) statt Demo-Charts in der Desktop-App
- Windows-Schlüsselbund für IMAP-Passwörter (Secret-Service/Keychain über dieselbe Abstraktion möglich)
- Kontrollierter lokaler IMAP-Testserver (`internal/mailbox/imaptest`) für Integrations-, IDLE-, Move- und Crash-Recovery-Tests

Offen bleiben u. a.: Erstdurchlauf-Profilierung mit Vorschlagsworkflow, Erkennung externer Client-Bewegungen, Export/Import von Profil- und Lernpaketen, signierte Updates und Linux-Paketierung. Der vollständige Stand steht in [TODO.md](TODO.md).

Der ausführliche Implementierungsauftrag für die weitere Backend-Entwicklung steht in [BACKEND_HANDOFF.md](BACKEND_HANDOFF.md), das Backend-Audit in [docs/backend-audit.md](docs/backend-audit.md).

## Designvorschau starten

Voraussetzungen: Node.js 24 und pnpm.

```powershell
cd apps\desktop
pnpm install
pnpm dev
```

Danach ist die Vorschau unter `http://localhost:5173/` erreichbar. Sie verwendet Demo-Daten und verbindet kein Postfach.

## Desktop-App (Entwicklung)

Voraussetzungen: Node.js 24, pnpm, Rust (stable-msvc) und die Microsoft-C++-Buildtools.

```powershell
cd apps\desktop
pnpm desktop:dev
```

Der Go-Sidecar wird automatisch gebaut und gestartet; UI-Änderungen laden per Hot-Reload. Sauber beenden über Strg+C oder Tray → „Beenden" (das Fenster schließt sich sonst nur ins Tray).

## Tests und Sidecar

Die portable Go-Toolchain liegt während der Entwicklung unter `.tools` und wird nicht versioniert.

```powershell
$env:GOROOT = (Resolve-Path .tools\go).Path
$env:GOMODCACHE = (Resolve-Path .tools\gomodcache).Path
$env:GOCACHE = (Resolve-Path .tools\gocache).Path
$env:CGO_ENABLED = "0"
.\.tools\go\bin\go.exe test -buildvcs=false ./...
.\scripts\build-sidecar.ps1
```

## ML-Werkzeuge (offline, ohne Agent)

```powershell
# Lernfilter gegen ein gelabeltes CSV-Korpus benchmarken (label,text; 0=ham, 1/2=spam/phish)
.\.tools\go\bin\go.exe run -buildvcs=false .\cmd\mltool eval C:\pfad\zum\korpus.csv

# Optionale globale Baseline aus einem MIT-lizenzierten Korpus importieren (opt-in, mit Herkunft)
.\.tools\go\bin\go.exe run -buildvcs=false .\cmd\mltool import "$env:APPDATA\de.rhmedia.mailmune\mailmune.db" C:\pfad\zum\korpus.csv --source "HF locuoco/…-300000" --license MIT

# Read-only Diagnose: echte Scores aller Mails eines verbundenen Kontos anzeigen
.\.tools\go\bin\go.exe run -buildvcs=false .\cmd\scandebug "$env:APPDATA\de.rhmedia.mailmune\mailmune.db"
```

Die Baseline wirkt als begrenzter Prior (virtuell 150 Beispiele); das bestätigte Lernen des Nutzers kann sie immer überstimmen. Ein Reset des Konto-Lernens lässt die Baseline unangetastet und umgekehrt.

## Sicherheitsgrenzen

- Kein API-Endpunkt und keine Oberfläche zum Löschen von E-Mails
- TLS-Zertifikatsprüfung ist verpflichtend; Dial und Handshake sind zeitbegrenzt und abbrechbar
- Anhänge und externe Inhalte werden nicht geöffnet
- Vollständige Nachrichtentexte werden nicht dauerhaft gespeichert; Lernfeatures sind normierte Token und werden nach Training oder 180 Tagen entfernt
- Ein LLM-Ergebnis allein löst niemals eine Verschiebung aus; Ollama ist nur über Loopback erreichbar und muss den Fähigkeitstest bestehen
- Neue Konten beginnen immer im Trockenlauf; Automatik erfordert ausdrückliche Aktivierung **und** bestandene Kalibrierung (≥ 20 Reviews, ≥ 99,5 % Präzision bei 0,98)
- Der COPY-/STORE-/EXPUNGE-Fallback wird nicht verwendet; ohne atomares IMAP MOVE wird nicht verschoben
- Jede Bewegung ist idempotent und absturzfest; nach einem Absturz wird durch Lesen abgeglichen, nie blind wiederholt
- Zugangsdaten liegen ausschließlich im Betriebssystem-Schlüsselbund; API-Fehler tragen stabile Codes und redigierte Texte
