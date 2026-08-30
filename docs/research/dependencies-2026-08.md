# Recherche: Abhängigkeiten und Datenquellen (2026-08-30)

Stichtag der Prüfung: 30.08.2026. Primärquellen: GitHub-Releases, Go-Modulproxy
(`proxy.golang.org`), pkg.go.dev, Lizenzdateien der Module.

## Gepinnte Go-Abhängigkeiten

| Bibliothek | Gepinnt | Neueste Version am Stichtag | Lizenz | Wartung | Plattformen | Bewertung |
|---|---|---|---|---|---|---|
| `github.com/emersion/go-imap/v2` | v2.0.0-beta.8 | v2.0.0-beta.8 (Tag am 16.12.2025, Release 07.02.2026) | MIT | aktiv (Push 02.07.2026, Default-Branch `v2`) | Windows/Linux/macOS, rein Go | **behalten**: aktueller Release, UID/MOVE/IDLE-Unterstützung, enthält `imapserver` für kontrollierte Integrationstests |
| `modernc.org/sqlite` | v1.57.0 | v1.57.0 (19.08.2026, Go-Proxy `@latest`) | BSD-3-Clause | aktiv, eingebettetes SQLite 3.53.3 | Windows/Linux/macOS ohne CGO | **behalten**: CGO-frei, damit einfaches Cross-Build des Sidecars; WAL/Foreign Keys/Busy-Timeout verfügbar |
| `github.com/zalando/go-keyring` | v0.2.8 | v0.2.8 (23.03.2026) | MIT | aktiv (Hardening-Release v0.2.8) | Windows (Credential Manager), Linux (Secret Service), macOS (Keychain) | **behalten**: genau die benötigte OS-Schlüsselbund-Abstraktion |
| `github.com/google/uuid` | v1.6.0 | v1.6.0 | BSD-3-Clause | stabil, Standardbibliothek-Ersatz | alle | **behalten** |

Bekannte Security Advisories: zum Stichtag keine offenen Advisories mit
Auswirkung auf die gepinnten Versionen (GitHub Advisory Database, Abfrage über
die Repository-/Release-API; `go mod` enthält keine als verwundbar markierten
Versionen).

## Entscheidung zu IMAP-Tests

`go-imap/v2` liefert das Server-Paket `imapserver` (Session-Interface,
`FetchWriter`, `MoveWriter` mit `COPYUID`-Antwort). Damit bauen wir einen
kontrollierten lokalen IMAP-Testserver **innerhalb derselben bereits
gepinnten MIT-lizenzierten Abhängigkeit** – keine neue Bibliothek, keine
GPL-Übernahme. Das in neueren Vorabständen enthaltene `imapmemserver` ist in
beta.8 noch nicht verfügbar; die Testsession wird daher selbst implementiert.

## Entscheidung zum statistischen Lernfilter

- Kandidaten geprüft: SpamAway (GPLv3, nur Lernreferenz laut `TODO.md`),
  diverse Bayes-Bibliotheken (überwiegend unmaintained oder Cloud-Anbindung).
- Entscheidung: **eigener, kleiner multinomialer Naive-Bayes-Lerner** in
  `internal/learning` (ca. 300 Zeilen), weil:
  - keine neue Abhängigkeit (Handoff: Standardbibliothek bevorzugen),
  - vollständig offline, deterministisch und reproduzierbar testbar,
  - Trainingsdaten bleiben lokal und stammen nur aus bestätigten Reviews,
  - Lizenzrisiken und Datenherkunftsprobleme externer Modelle entfallen.

## Externe Blacklists / Trainingsdaten

Werden weiterhin **nicht** verwendet. Voraussetzung für eine spätere Nutzung
bleibt: dokumentierte Herkunft, Lizenz, Signatur/Hash, Aktualisierungsweg und
Datenschutzprüfung (`TODO.md` P0).

## Ausblick (noch nicht eingebaut)

- **Ollama-API**: lokale Anbindung über `http://127.0.0.1:11434` mit
  `format`-Schema und strikter Antwortvalidierung ist bereits implementiert;
  Fähigkeitstest folgt in Phase 1 (Schnittstelle stabil, lokale Modelle wie
  qwen3:4b bleiben Empfehlung).
- **Rspamd**: optionaler externer HTTP-Provider, nicht Windows-Pflichtdienst;
  weiterhin nicht priorisiert.

## Externes Evaluierungs-Datenset (Hugging Face)

- Quelle: `locuoco/the-biggest-spam-ham-phish-email-dataset-300000`
  (https://huggingface.co/datasets/locuoco/the-biggest-spam-ham-phish-email-dataset-300000).
- Lizenz laut Dataset-Karte: **MIT**; Herkunft dokumentiert als Übernahme des
  gleichnamigen Kaggle-Datensets (`akshatsharma2`, MIT). Stichtagsprüfung am
  31.08.2026.
- Inhalt: ca. 365.000 Zeilen (Label 0=Ham, 1=Phish, 2=Spam), reine Textkörper
  ohne Header/Absender, englisch, ~631 MB.
- Bewertung: Als **Offline-Evaluierungsbasis** für Feature-Extraktion und
  Lernfilter-Metrik (Precision/Recall/FPR) geeignet. Grenzen: keine
  Header-Informationen (Authentication, Listen, Absenderstrukturen lassen sich
  damit nicht prüfen), englischsprachig, deutsche Spam-Muster unterrepräsentiert.
- Auflagen für die Nutzung: Das Dataset wird **nicht** ins Repository
  übernommen und nicht als Trainingsdatensatz ausgeliefert. Ein späterer
  Import in den lokalen Lernfilter läuft ausschließlich über den noch zu
  spezifizierenden, versionierten Profil-/Lerntransfer mit Herkunft, Schema,
  Version und Konfliktanzeige (siehe BACKEND_HANDOFF Datenbank-Abschnitt).
  Vor einem produktiven Einsatz ist die Herkunft der Unter-Datensätze
  (Enron u. a.) datenschutzrechtlich zu bewerten.
