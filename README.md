# Mailmune

Mailmune ist ein lokaler IMAP-Spamfilter mit menschlicher Abnahme. Die Anwendung liest im ersten Betriebsmodus ausschließlich Metadaten, ordnet Verdachtsfälle nachvollziehbar ein und löscht niemals E-Mails.

## Aktueller MVP

- Browserfähige React-/shadcn-App-Shell nach dem Figma-Entwurf
- Go-Agent mit lokaler, token-geschützter API und SQLite
- IMAP-Verbindungstest über TLS und lesender 90-Tage-/1.000-Nachrichten-Trockenlauf
- Regelbasierte Klassifikation und optionale lokale Ollama-Klassifikation
- Windows-Schlüsselbund für IMAP-Passwörter
- Tauri-Sidecar-, Tray-, Autostart-, Benachrichtigungs- und Updater-Grundgerüst

Automatisches Verschieben, IMAP-IDLE, Modellvalidierung und signierte Updates bleiben deaktiviert, bis die jeweiligen Sicherheitstests abgeschlossen sind. Der vollständige Stand steht in [TODO.md](TODO.md).

Der ausführliche Implementierungsauftrag für die weitere Backend-Entwicklung steht in [BACKEND_HANDOFF.md](BACKEND_HANDOFF.md).

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
.\.tools\go\bin\go.exe test ./...
.\scripts\build-sidecar.ps1
```

Für die native Windows-App werden zusätzlich Rust und die Microsoft-C++-Buildtools benötigt. Danach startet `pnpm desktop:dev` in `apps\desktop` zuerst den Sidecar-Build und anschließend Tauri.

## Sicherheitsgrenzen

- Kein API-Endpunkt und keine Oberfläche zum Löschen von E-Mails
- TLS-Zertifikatsprüfung ist verpflichtend
- Anhänge und externe Inhalte werden nicht geöffnet
- Vollständige Nachrichtentexte werden nicht dauerhaft gespeichert
- Ein LLM-Ergebnis allein löst niemals eine Verschiebung aus
- Neue Konten beginnen immer im Trockenlauf
- Der COPY-/STORE-/EXPUNGE-Fallback wird nicht verwendet; ohne IMAP MOVE wird nicht verschoben
