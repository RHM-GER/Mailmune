# Mailmune

Mailmune ist ein lokaler IMAP-Spamfilter mit menschlicher Abnahme. Die Anwendung liest im ersten Betriebsmodus ausschließlich Metadaten, ordnet Verdachtsfälle nachvollziehbar ein und löscht niemals E-Mails.

## Aktueller MVP

- Browserfähige React-/shadcn-App-Shell nach dem Figma-Entwurf
- Go-Agent mit lokaler, token-geschützter Loopback-API und SQLite (WAL, Foreign Keys, nummerierte vorwärtslaufende Migrationen)
- UID-basierte, ausschließlich lesende IMAP-Synchronisierung mit sicherer Re-Synchronisierung bei `UIDVALIDITY`-Wechsel
- Hintergrundscans je Konto mit Fortschritt, Abbruch und Wiederaufnahme nach Neustart; neue Konten starten immer im Trockenlauf
- SSE-Eventstream (`GET /v1/events`) für Scan-Fortschritt und Kontostatus, in der Desktop-App als Tauri-Events weitergereicht
- Dreistufige Klassifikation: deterministische Regelpipeline mit stabilen Evidence-Codes (siehe [docs/evidence-codes.md](docs/evidence-codes.md)), lokaler Naive-Bayes-Lernfilter aus bestätigten Reviews, optional lokal validiertes Ollama-Modell (nur Loopback, streng validiert, Fähigkeitstest)
- Windows-Schlüsselbund für IMAP-Passwörter (Secret-Service/Keychain über dieselbe Abstraktion möglich)
- Tauri-Sidecar-, Tray-, Autostart-, Benachrichtigungs- und Updater-Grundgerüst
- Kontrollierter lokaler IMAP-Testserver (`internal/mailbox/imaptest`) für Integrations- und Crash-Recovery-Tests

Automatisches Verschieben (Move-Zustandsmaschine), IMAP-IDLE mit periodischem Abgleich, Kalibrierungsmetriken und signierte Updates bleiben deaktiviert bzw. offen, bis die jeweiligen Sicherheitstests abgeschlossen sind. Der vollständige Stand steht in [TODO.md](TODO.md).

Der ausführliche Implementierungsauftrag für die weitere Backend-Entwicklung steht in [BACKEND_HANDOFF.md](BACKEND_HANDOFF.md), das aktuelle Backend-Audit in [docs/backend-audit.md](docs/backend-audit.md).

## Designvorschau starten

Voraussetzungen: Node.js 24 und pnpm.

```powershell
cd apps\desktop
pnpm install
pnpm dev
```

Danach ist die Vorschau unter `http://localhost:5173/` erreichbar. Sie verwendet Demo-Daten und verbindet kein Postfach.

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

Für die native Windows-App werden zusätzlich Rust und die Microsoft-C++-Buildtools benötigt. Danach startet `pnpm desktop:dev` in `apps\desktop` zuerst den Sidecar-Build und anschließend Tauri. Hinweis: Die Rust-Änderungen am SSE-Weiterleiter (`src-tauri/src/lib.rs`, `futures-util`/reqwest-`stream`) wurden ohne lokale Rust-Toolchain erstellt und müssen beim ersten `cargo check`/`pnpm desktop:dev` verifiziert werden.

### Lokaler Test des Agents ohne Desktop-App

```powershell
.\.tools\go\bin\go.exe run -buildvcs=false .\cmd\spam-agent --data-dir .\data-test
```

Der Agent gibt beim Start eine JSON-Zeile mit `endpoint` und `token` aus. Danach z. B.:

```powershell
# Konto anlegen (Passwort geht nur in den Schlüsselbund)
curl.exe -X POST "$ENDPOINT/v1/accounts" -H "Authorization: Bearer $TOKEN" -d "@konto.json"
# Verbindung testen, Scan starten, Fortschritt lesen, Ereignisse abonnieren
curl.exe -X POST "$ENDPOINT/v1/accounts/<id>/test" -H "Authorization: Bearer $TOKEN"
curl.exe -X POST "$ENDPOINT/v1/accounts/<id>/scans" -H "Authorization: Bearer $TOKEN" -d "{}"
curl.exe "$ENDPOINT/v1/accounts/<id>/scans" -H "Authorization: Bearer $TOKEN"
curl.exe -N "$ENDPOINT/v1/events" -H "Authorization: Bearer $TOKEN"
```

Der Scan läuft im Hintergrund und setzt nach Abbruch oder Absturz beim nächsten Start an der zuletzt persistierten UID auf. Entscheidungen werden über Idempotenz-Schlüssel dedupliziert.

## Sicherheitsgrenzen

- Kein API-Endpunkt und keine Oberfläche zum Löschen von E-Mails
- TLS-Zertifikatsprüfung ist verpflichtend; Dial und Handshake sind zeitbegrenzt und abbrechbar
- Anhänge und externe Inhalte werden nicht geöffnet
- Vollständige Nachrichtentexte werden nicht dauerhaft gespeichert; Lernfeatures sind normierte Token und werden nach Training oder 180 Tagen entfernt
- Ein LLM-Ergebnis allein löst niemals eine Verschiebung aus; Ollama ist nur über Loopback erreichbar
- Neue Konten beginnen immer im Trockenlauf; der COPY-/STORE-/EXPUNGE-Fallback wird nicht verwendet, ohne IMAP MOVE wird nicht verschoben
- Zugangsdaten liegen ausschließlich im Betriebssystem-Schlüsselbund; API-Fehler tragen stabile Codes und redigierte Texte
