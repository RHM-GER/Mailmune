# Mailmune – Backend-Audit (2026-08-30)

Basis: Commit `c210336` (`main`), Branch `qwen/backend-foundation`.
Reproduzierbare Basislinie: `go test ./...` grün, `pnpm typecheck` grün.

## Was funktioniert bereits

- **Lokale API**: Loopback-Listener auf `127.0.0.1:0`, Sitzungstoken mit
  Konstantzeit-Vergleich, Host-Prüfung, Origin-Sperre, body-limitierte
  JSON-Dekodierung. Durch `internal/api/server_test.go` abgesichert.
- **IMAP lesen**: TLS-Verbindungstest und lesender Metadaten-Scan
  (Envelope, ausgewählte Header als `BODY.PEEK[HEADER.FIELDS]` mit
  64-KiB-Partial, `RFC822.SIZE`, erweiterte `BODYSTRUCTURE`). Auswahl
  ausschließlich `ReadOnly`, keine Flag-Änderungen.
- **MOVE-Verweigerung**: `MoveAtomic` prüft die `MOVE`-Fähigkeit und lehnt den
  `COPY + STORE + EXPUNGE`-Fallback der Bibliothek ab (`ErrMoveUnsupported`).
  Die No-Delete-Garantie bleibt damit gewahrt.
- **Aktionssicherheit**: `classifier.Decide` verlangt zwei unabhängige
  Signalgruppen, `>= 0,98` im sicheren Modus, keine Automatik bei
  Vertrauenssignalen; ein reines LLM-Ergebnis verschiebt nie (getestet).
- **Datenhaltung**: SQLite mit WAL, Foreign Keys, Busy-Timeout; Reviews über
  Idempotenz-Schlüssel in `review_operations` gegen Doppelanwendung geschützt;
  180-Tage-Redaktion von Absender/Betreff vorhanden.
- **Schlüsselbund**: `zalando/go-keyring` (Windows Credential Manager;
  Secret-Service/Keychain-Fallback der Bibliothek), Referenzen als `SecretRef`.
- **Ollama**: streng validiertes JSON-Antwortformat, Loopback-Standard,
  Text-Begrenzung, `format`-Schema im Request.

## Was nur Grundgerüst ist

- **Keine versionierten Migrationen**: nur `CREATE TABLE IF NOT EXISTS`.
- **Kein UID-Sync-Zustand**: `UIDVALIDITY` und letzte UID werden nirgends
  persistiert; jeder Scan beginnt von vorn (letzte 1.000 Nachrichten).
- **Scan läuft synchron im HTTP-Handler** (10-Minuten-Timeout): kein
  Fortschritt, kein Abbruch, keine Wiederaufnahme, keine Hintergrundplanung.
- **`GET /v1/events`** sendet nur Heartbeats; kein echter Eventstream.
- **Kein IDLE, kein periodischer Abgleich, kein Reconnect/Backoff.**
- **UI**: Übersicht, Zuordnung und Einstellungen fragen zwar echte Daten ab,
  starten aber mit Demo-Daten; Scans, Fortschritt und Ereignisse sind nicht
  angebunden.
- **Keine Tests** für `store`, `service`, `mailbox`, `provider`, `secrets`.

## Sicherheitsrisiken und Architekturprobleme

1. **Trockenlauf nicht erzwungen**: `SaveAccount` übernimmt `dryRun` unverändert
   aus dem Request. Neue Konten könnten so mit `dryRun=false` angelegt werden –
   Verstoß gegen Produktregel 9. **Wird behoben: neue Konten starten immer im
   Trockenlauf.**
2. **Schreibende Endpunkte ohne Idempotency Keys** (`POST /v1/accounts`,
   Scan-Start) – Vertragspflicht aus dem Handoff.
3. **Fehlerantworten geben rohe Fehlertexte weiter** (stabile Fehlercodes und
   Redaktion fehlen). Passwörter gelangen zwar nicht in Fehler, aber die
   Antwortstruktur ist nicht versioniert.
4. **`decisions`-Upsert mit `ON CONFLICT DO NOTHING`**: erneute Scans
   aktualisieren weder Ordner noch Score; externe Verschiebungen durch andere
   Clients bleiben unsichtbar.
5. **Ein einziger blockierender Scan** pro Prozess; kein serialisierter
   Scheduler pro Konto.
6. **Ollama-Endpunkt nicht auf Loopback beschränkt**: `baseURL` ist zwar
   konfigurierbar, aber nicht validiert.
7. **Keine Zustandsmaschine**: Entscheidungen kennen nur Endzustände;
   `move_planned/moving/restoring` fehlen (Phase 2).

## Zwingende TODOs für den ersten real nutzbaren Test

Aus `TODO.md` (P0) abgeleitet, in dieser Reihenfolge:

1. Versionierte Migrationen und Repository-Härtung.
2. Persistenter UID-Sync-Zustand (`UIDVALIDITY`, letzte UID je Ordner) mit
   sicherer Re-Synchronisierung bei `UIDVALIDITY`-Wechsel.
3. Scan als Hintergrundlauf mit Fortschritt, Abbruch, Wiederaufnahme nach
   Neustart.
4. Eventstream (SSE) für Scan-Fortschritt und neue Entscheidungen.
5. UI-Demo-Daten durch echte Agent-Daten ersetzen und klar trennen.
6. Trockenlauf für neue Konten serverseitig erzwingen.
7. Tests für Neustart, `UIDVALIDITY`, Idempotenz.

## Bibliotheken und Datenquellen (Recherche)

Details in [`research/dependencies-2026-08.md`](research/dependencies-2026-08.md).
Kurzergebnis: Alle vier vorhandenen Abhängigkeiten sind aktuellste Releases,
MIT/BSD-lizenziert und aktiv gepflegt. `emersion/go-imap/v2` enthält das
Paket `imapserver`, mit dem sich ein kontrollierter lokaler IMAP-Testserver
ohne neue Abhängigkeit bauen lässt. Der statistische Lernfilter wird als
kleiner Naive-Bayes-Lerner ohne externe Bibliothek implementiert. Externe
Blacklists werden weiterhin nicht verwendet.

## Zwischenstand (Branch `qwen/backend-foundation`)

Behoben bzw. umgesetzt:

- Versionierte, vorwärtslaufende Migrationen (`schema_migrations`).
- UID-Sync-Zustand und Scan-Läufe persistiert; `UIDVALIDITY`-Wechsel löst
  sicheren Resync aus.
- Scans laufen im Hintergrund je Konto serialisiert, mit Fortschritt,
  Abbruch und Wiederaufnahme; `running`-Leichen werden beim Start als
  `interrupted` markiert.
- Trockenlauf wird serverseitig für neue Konten erzwungen.
- SSE-Eventstream liefert `scan.*`- und `account.*`-Ereignisse; Fehler
  tragen stabile Codes.
- `defer client.Logout().Wait()`-Defekt (sofortiger LOGOUT-Versand) in allen
  IMAP-Pfaden behoben; Fetch-Deadlock durch Zwei-Phasen-Abruf behoben;
  Dial/Handshake sind zeitbegrenzt und abbrechbar.
- Regelpipeline mit dokumentierten Evidence-Codes, Deny-Listen und lokalem
  Lernfilter; Ollama nur Loopback, serialisiert, mit Fähigkeitstest.

Weiterhin offen (siehe TODO.md):

- Move-Zustandsmaschine (`move_planned/moving/...`) mit Zielprüfung und
  Rückverschiebung; bis dahin verschiebt nur die bestehende, MOVE-gebundene
  Pfadlogik im freigegebenen Nicht-Trockenlauf.
- IMAP-IDLE, periodischer Abgleich, Standby-Reconnect.
- Kalibrierungsmetriken und Export/Import des Lernwissens.
- Rust-SSE-Weiterleiter ist ohne lokale Rust-Toolchain erstellt und muss
  beim ersten Desktop-Build verifiziert werden.
