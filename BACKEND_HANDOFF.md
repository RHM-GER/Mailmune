# Mailmune – Backend-Handoff und Implementierungsauftrag

## Rolle und Ziel

Du übernimmst die Backend-Entwicklung von **Mailmune**, einer lokalen Desktopanwendung zum sicheren Klassifizieren und Verschieben von Spam in beliebigen IMAP-Postfächern. Das vorhandene React-/Tauri-Frontend ist ein weit entwickelter interaktiver Prototyp. Deine Hauptaufgabe ist nicht das Redesign der Oberfläche, sondern der belastbare Go-Agent, die lokale Datenhaltung, IMAP-Synchronisierung, Klassifikation, Lernlogik und die versionierte Verbindung zum Frontend.

Arbeite autonom, effizient und evidenzbasiert. Inspiziere zuerst den vorhandenen Code und `TODO.md`, bevor du Architektur änderst. Implementiere vertikale, testbare Funktionsschnitte statt großer unverbundener Gerüste. Dokumentiere wichtige Entscheidungen knapp im Repository.

## Unverhandelbare Produktregeln

1. **Mailmune löscht niemals E-Mails.** Es gibt keine Lösch-, Papierkorb-, Auto-Expunge- oder Retention-Funktion für Nachrichten.
2. Verdachtsfälle werden höchstens in den konfigurierten Ordner `AI_SPAM_FILTER` verschoben. Fehlalarme gehen in den gespeicherten Ursprungsordner zurück.
3. Eine IMAP-Bewegung darf niemals die letzte vorhandene Kopie gefährden. Bevorzugt wird atomisches `MOVE`. Ein unsicherer `COPY + STORE + EXPUNGE`-Fallback bleibt verboten.
4. TLS mit vollständiger Zertifikatsprüfung ist verpflichtend. Kein Klartext-IMAP und kein „Zertifikat ignorieren“.
5. Anhänge werden nicht geöffnet. Externe Bilder, URLs und sonstige Ressourcen werden nie abgerufen.
6. Vollständige Nachrichtentexte werden nicht dauerhaft gespeichert. Temporär gelesener Text ist nach Klassifikation zu verwerfen.
7. Ein LLM-Ergebnis allein darf niemals eine automatische Verschiebung auslösen.
8. Bekannte Korrespondenzpartner werden ohne zusätzliche starke Belege nicht automatisch verschoben.
9. Neue Konten beginnen im lesenden Trockenlauf. Automatische Aktionen benötigen eine ausdrückliche Aktivierung.
10. Zugangsdaten gehören ausschließlich in den Betriebssystem-Schlüsselbund, niemals in SQLite, Logs oder Konfigurationsdateien.
11. Keine Cloud-KI, keine Telemetrie und kein zentraler Maildienst. Ollama läuft ausschließlich lokal.
12. Jeder schreibende Ablauf muss idempotent, absturzfest und nachvollziehbar sein.

## Vorhandener Stand

### Desktop

- `apps/desktop`: React, TypeScript, Tailwind CSS, shadcn/ui und Tauri v2.
- Interaktive Seiten für Übersicht, Zuordnung, Benachrichtigungen und Einstellungen.
- Tauri startet den Go-Agenten als Sidecar und liest dessen einmaligen Handshake.
- Kommunikation ausschließlich über `127.0.0.1`, zufälligen Port und ein pro Start erzeugtes Sitzungstoken.
- Tray, Autostart-, Benachrichtigungs- und Updater-Grundgerüste sind vorhanden.
- Viele UI-Daten sind noch Demo-Daten. Ersetze sie schrittweise über die vorhandene Agent-Schnittstelle, ohne das Design unnötig umzubauen.

### Go-Agent

- Einstieg: `cmd/spam-agent/main.go`
- Domänenmodelle: `internal/domain`
- Lokale API: `internal/api`
- SQLite: `internal/store`
- IMAP: `internal/mailbox`
- Regeln und Aktionsrichtlinie: `internal/classifier`
- Ollama-Provider: `internal/provider`
- Schlüsselbund: `internal/secrets`
- Anwendungslogik: `internal/service`
- Versionierter Vertrag: `contracts/v1/types.schema.json`

Bereits vorhanden sind ein TLS-IMAP-Verbindungstest, ein begrenzter lesender Metadaten-Scan, ein sicher verweigerter MOVE-Fallback, SQLite-Grundtabellen, eine token-geschützte Loopback-API, einfache Regeln und erste Tests. Behandle dies als Ausgangspunkt, nicht als fertige Implementierung.

## Arbeitsweise

1. Lies `README.md`, `TODO.md`, diesen Handoff und alle Dateien unter `internal`, `cmd/spam-agent`, `contracts` und `apps/desktop/src/lib`.
2. Führe vorhandene Go- und TypeScript-Tests aus. Halte den ersten reproduzierbaren Status fest.
3. Erstelle eine kleine Bedrohungsanalyse für IMAP-Schreibvorgänge, lokale API, Schlüsselbund, SQLite, MIME und Prompt Injection.
4. Teile die Umsetzung in kleine Pull-Request-fähige Schritte. Jeder Schritt enthält Tests und aktualisierte Verträge.
5. Vermeide neue Abhängigkeiten, wenn die Standardbibliothek oder eine bereits vorhandene Bibliothek ausreicht.
6. Pinne Abhängigkeiten reproduzierbar. Prüfe Lizenz, Wartungsstatus, letzte Releases, Security Advisories und Plattformunterstützung.
7. Kopiere keinen Code aus `dominicgisler/imap-spam-cleaner`; das GPLv3-Projekt dient nur als technische Referenz.
8. Übernimm keine Blacklist oder Trainingsdatenbank ohne dokumentierte Herkunft, Lizenz, Aktualisierungsweg, Datenschutzprüfung und reproduzierbare Tests.

## Pflichtrecherche vor neuen Bibliotheken und Datenquellen

Recherchiere aktuelle, aktiv gepflegte Lösungen und dokumentiere die Auswahl in `docs/research/`. Nutze bevorzugt offizielle Dokumentation, Repositories und Primärquellen.

Prüfe insbesondere:

- den aktuellen stabilen Stand von `emersion/go-imap/v2`, einschließlich IDLE, UIDVALIDITY, MOVE, QRESYNC/CONDSTORE und Reconnect-Verhalten;
- MIME-Parsing mit harten Größen-, Tiefen- und Zeitlimits;
- lokale statistische Spamfilter, die eingebettet, offline und lizenzkompatibel sind;
- etablierte öffentliche DNS-/URI-Blocklisten und offline auslieferbare Listen mit klarer Lizenz;
- optional Rspamd als externe HTTP-Provider-Schnittstelle, nicht als Windows-Pflichtdienst;
- Ollama-API, strukturierte JSON-Ausgabe, Modellverfügbarkeit und ressourcenschonende lokale Modelle;
- Windows Credential Manager, Linux Secret Service und macOS Keychain über die bestehende SecretStore-Abstraktion;
- SQLite-Migrationen, Verschlüsselungsgrenzen und sichere Dateiberechtigungen;
- sichere Aktualisierung signierter Tauri-Artefakte.

Bewerte Kandidaten tabellarisch nach Lizenz, Aktivität, Sicherheitsmodell, Speicherbedarf, Plattformen, API-Stabilität und Integrationsaufwand. „Viele GitHub-Stars“ ist kein ausreichendes Auswahlkriterium.

## Zielarchitektur

### Laufzeit

- Ein Agentprozess pro Installation.
- Mehrere unabhängige IMAP-Konten pro Installation.
- Pro Konto genau ein Scheduler und eine serialisierte Aktionswarteschlange.
- IMAP-IDLE für zeitnahe Erkennung, sofern der Server es unterstützt.
- Ergänzender UID-Abgleich standardmäßig alle zehn Minuten, damit Standby, Netzwechsel und IDLE-Abbrüche keine Nachrichten verlieren.
- Begrenzte Wiederverbindung mit Exponential Backoff und Jitter.
- Keine unbeschränkten Goroutines, keine parallelen Ollama-Aufrufe und keine unbeschränkten Queues.
- Der Agent muss ohne Ollama deutlich unter 300 MB Leerlauf-RAM bleiben.

### Kontozustand

Persistiere mindestens:

- Konto-ID und nicht geheime IMAP-Konfiguration;
- Ordnerzuordnung;
- `UIDVALIDITY`, letzte sicher beobachtete UID und letzter erfolgreicher Abgleich pro Ordner;
- Profil, Regeln, Schwellen und Sicherheitsmodus;
- Entscheidungs- und Aktionsstatus;
- idempotente Operationsschlüssel;
- Modellname, Modellversion und bestandener Fähigkeitstest;
- letzte Fehler in redigierter Form;
- aggregierte Statistiken.

Passwörter werden nur über `SecretRef` referenziert.

### Zustandsmaschine einer Nachricht

Verwende eine explizite Zustandsmaschine, beispielsweise:

`observed -> classified -> review_pending | move_planned -> moving -> moved -> confirmed | rejected -> restoring -> restored`

Zusätzlich sind `deferred`, `failed_retryable` und `failed_terminal` zulässig. Jeder Übergang braucht Vorbedingungen, Zeitstempel und einen idempotenten Schlüssel. Wiederholungen nach Absturz dürfen keine doppelte Bewegung erzeugen.

## IMAP-Implementierung

### Lesen

1. Nach UID und nicht nach Sequenznummer arbeiten.
2. `UIDVALIDITY` bei jeder Auswahl prüfen. Wechsel löst eine sichere Re-Synchronisierung aus, keine blinde Fortsetzung.
3. Zuerst Envelope, ausgewählte Header, Größe und MIME-Struktur laden.
4. Harte Limits definieren: Nachrichtengröße, Headergröße, MIME-Tiefe, Teileanzahl, Textmenge und Verarbeitungszeit.
5. Nur bei Bedarf begrenzten Text für statistische/LLM-Klassifikation laden.
6. HTML offline und deterministisch in Klartext umwandeln. Keine URL auflösen oder abrufen.
7. `Authentication-Results` nur als nicht vertrauenswürdiges Eingangssignal behandeln. Vertraue nur Ergebnissen, deren Einfügepunkt und Providerkontext plausibel validiert werden können.
8. „Gesendet“ im Profil-Trockenlauf ausschließlich lesend verwenden, um bekannte Korrespondenzpartner vorzuschlagen.

### Schreiben

- Zielordner kontrolliert anlegen, wenn er fehlt und der Nutzer dies bestätigt hat.
- Vor jeder Bewegung aktuellen Zustand und UIDVALIDITY erneut prüfen.
- Atomisches UID MOVE verwenden.
- Serverantwort und Ziel-UID speichern, soweit verfügbar.
- Bei unklarer Serverantwort durch lesenden Abgleich ermitteln, wo die Nachricht liegt; nicht blind wiederholen.
- Ohne sichere MOVE-Unterstützung keine automatische Bewegung. Der Fall bleibt in der Prüfliste.
- Rückverschiebung verwendet denselben abgesicherten Ablauf.
- Externe Bewegungen durch Thunderbird/Outlook erkennen und als Feedback interpretieren, ohne Schleifen zu erzeugen.

## Klassifikationspipeline

Die Pipeline muss deterministische Signale bevorzugen und Unsicherheit sichtbar machen.

### Stufe 1: harte lokale Regeln

- explizite Allow-/Deny-Regeln für Absender, Domain und Schlüsselwörter;
- bekannte ausgehende Korrespondenz;
- bestätigte Geschäftspartner und Newsletter;
- Mailinglistenmerkmale;
- Antwortbeziehungen und Message-ID-/References-Zusammenhang;
- Domainähnlichkeit, Unicode-Homoglyphen und verdächtige TLD-/URL-Muster;
- Absender-/Reply-To-Abweichungen;
- Authentifizierungsindizien;
- ungewöhnliche Anhangsmetadaten;
- wiederkehrende bestätigte Muster.

### Stufe 2: lokaler statistischer Filter

Baue einen nachvollziehbaren, versionierten Online-Lerner für bestätigtes Spam/Ham. Anforderungen:

- Training nur aus menschlich bestätigten Aktionen oder eindeutig importierten Regeln;
- getrennte Modelle pro Profil plus optional anonymisierbare globale Merkmale;
- Schutz gegen Datenvergiftung und massenhaft identische Beispiele;
- reproduzierbare Feature-Extraktion;
- Export/Import mit Schema- und Versionsprüfung;
- vollständiges Zurücksetzen und Neuaufbauen aus bestätigtem Feedback;
- Messung von Precision, Recall, False-Positive-Rate und Kalibrierung.

Bewerte Naive Bayes, logistische Regression oder einen ähnlich kleinen Online-Ansatz anhand realer Tests. Verwende keinen undurchsichtigen Ansatz nur wegen einer vorhandenen Library.

### Stufe 3: lokales Ollama-Modell

Nur unklare Fälle dürfen an Ollama gehen. Sende minimal notwendige, begrenzte Daten. Systemanweisung und Ausgabeformat sind fest versioniert.

Erwartete Antwort:

```json
{
  "label": "spam|ham|uncertain",
  "score": 0.0,
  "reasonCodes": ["FIXED_CODE"]
}
```

- JSON strikt validieren; freie Texte ignorieren.
- Mailinhalt ist untrusted data und niemals eine Anweisung.
- Fähigkeitstest umfasst Deutsch, Englisch, kaputtes JSON, Prompt Injection, lange Texte und widersprüchliche Signale.
- Ein nicht validiertes Modell liefert höchstens UI-Vorschläge.
- Modellwechsel darf gespeicherte statistische Lernmerkmale nicht zerstören.
- Modellaufrufe seriell, abbrechbar und mit Timeout ausführen.

### Aktionsrichtlinie

- Unter `0,60`: nicht in der App-Prüfliste halten, sofern keine explizite Regel greift.
- „Alles bestätigen“: keine automatische Bewegung.
- Standard: automatisch erst ab kalibriertem Score `>= 0,98`, mindestens zwei unabhängigen Signalgruppen und ohne starkes Vertrauenssignal.
- Autonom: konfigurierbare Schwelle `0,75–0,98`; weiterhin mindestens zwei unabhängige Signalgruppen und kein starkes Vertrauenssignal.
- Ein LLM-Signal zählt höchstens als eine Signalgruppe.
- Die UI-Darstellung eines Scores ist keine Garantie für statistische Kalibrierung; kalibriere ihn anhand bestätigter lokaler Daten.

## Profilierung und Erstdurchlauf

Implementiere einen lesenden Erstdurchlauf über höchstens 90 Tage oder 1.000 Nachrichten aus Posteingang und Gesendet:

- Fortschritt, geschätzte Restzeit und Abbruch unterstützen;
- keine Flags, Ordner oder Nachrichten verändern;
- Kontakte, Domains, Newsletter und Nachrichtentypen nur vorschlagen;
- Nutzer muss Vorschläge bestätigen;
- vollständige Texte nicht speichern;
- Resultat als strukturierte, versionierte `MailboxProfile`-Daten halten, nicht als frei wachsenden Prompt.

## Datenbank und Migrationen

1. Definiere nummerierte, vorwärts laufende SQLite-Migrationen.
2. Aktiviere Foreign Keys und sinnvolle Busy-Timeouts.
3. Verwende Transaktionen für Zustandsübergänge und Outbox/Action Queue.
4. Redigiere Prüfdetails nach 180 Tagen; bewahre nur aggregierte Summen und notwendige Lernmerkmale.
5. Speichere keine Passwörter, Sitzungstokens oder vollständigen Mailtexte.
6. Dokumentiere Backup-, Export- und Wiederherstellungsformat.
7. Implementiere zwei klar getrennte Transferformate:
   - anonymisierte allgemeine Lernmerkmale ohne personenbezogene Inhalte;
   - vollständiges Profilpaket mit Regeln und profillokalem Wissen, nur nach expliziter Bestätigung.
8. Import muss Herkunft, Schema, Version und Konflikte vor Anwendung anzeigen.

## Lokale API und Verträge

- Versioniere alle Typen in `contracts/v1` und generiere oder validiere Go-/TypeScript-Typen dagegen.
- Nur Loopback-Verbindungen akzeptieren.
- Sitzungstoken mit konstanter Zeit vergleichen.
- Host- und Origin-Prüfung beibehalten.
- Schreibende Endpunkte benötigen Idempotency Keys.
- SSE oder einen vergleichbaren lokalen Eventstream für Status, Scanfortschritt, neue Entscheidungen und Verbindungszustand implementieren.
- Fehler erhalten stabile Codes plus redigierte, nutzerfreundliche Texte.
- Keine Geheimnisse, Rohtexte oder vollständigen Header in API-Fehlern oder Logs.

Benötigte Endpunktgruppen:

- Konten: anlegen, testen, aktualisieren, aktivieren/deaktivieren;
- Profile und Regeln;
- Scan starten, abbrechen und Status lesen;
- Entscheidungen suchen, filtern und paginieren;
- Review-Aktionen als Batch;
- Verbindungs-/Modellstatus;
- Ollama-Modelle erkennen und Fähigkeitstest ausführen;
- Berichte und aggregierte Statistiken;
- sichere Profiltransfers;
- Eventstream.

Es darf keinen Endpunkt zum Löschen von E-Mails geben.

## Tests

### Unit- und Fuzztests

- MIME-Grenzen, kaputte Header und Unicode;
- Domainnormalisierung und Homoglyphen;
- Prompt-Injection-Beispiele;
- Regelpriorität und unabhängige Signalgruppen;
- Score-Kalibrierung;
- Zustandsmaschine und idempotente Wiederholung;
- Migrationen und Retention;
- Secret-Redaktion in Fehlern und Logs.

### IMAP-Integration

Baue einen kontrollierten lokalen IMAP-Testserver oder testbare Adapter/Fakes für:

- IDLE und Reconnect;
- UIDVALIDITY-Wechsel;
- MOVE mit und ohne zurückgegebene Ziel-UID;
- Verbindungsabbruch vor, während und nach MOVE;
- externe Bewegungen;
- Ordneranlage;
- mehrere Konten;
- Standby-/Netzwechsel-Simulation;
- Server ohne MOVE, bei dem niemals automatisch verschoben wird.

### Sicherheit

- Nachweis, dass keine Lösch-/Trash-/Expunge-Funktion existiert;
- lokale API nicht von fremden Origins oder Netzwerkinterfaces erreichbar;
- keine Secrets in SQLite/Logs/API;
- TLS-Fehler werden nicht umgangen;
- Größen-/Zeitlimits verhindern Ressourcenerschöpfung;
- Agent-/UI-Neustart erzeugt keine Doppelaktionen.

### Echter Anbieter

Erst nach bestandenen automatisierten Tests:

1. STRATO-Verbindung ausschließlich lesend testen.
2. Mindestens 100 Entscheidungen manuell prüfen.
3. Bewegungen zunächst nur mit kopierten Testnachrichten in einem eigenen Testordner prüfen.
4. Zwei Wochen Schattenbetrieb empfehlen.
5. Automatische Bewegung erst nach ausdrücklicher Zustimmung aktivieren.
6. Zielpräzision für automatische Bewegungen: mindestens 99,5 % im lokal bestätigten Datensatz.

## Priorisierte Umsetzung

### Phase 1 – belastbarer lokaler Kern

- Migrationen und Repository-Abstraktionen härten.
- Verträge vervollständigen.
- Scheduler, Kontolifecycle und Scanfortschritt implementieren.
- UI-Demoentscheidungen durch echte paginierte Agentdaten ersetzen.
- Secret- und Log-Redaktion testen.

### Phase 2 – sichere Synchronisierung

- UID-basierte Synchronisierung, UIDVALIDITY und Reconnect.
- IMAP-IDLE plus Zehn-Minuten-Abgleich.
- atomische Move-/Restore-Zustandsmaschine.
- externe Clientbewegungen erkennen.
- Integrations- und Crash-Recovery-Tests.

### Phase 3 – Klassifikation und Lernen

- Feature-Pipeline und Rules Engine.
- statistischen Online-Lerner evaluieren und implementieren.
- Ollama-Fähigkeitstest und strikt validierte Ausgabe.
- kalibrierte Aktionsrichtlinie und Feedbackschleife.
- anonymisierten und vollständigen Profiltransfer spezifizieren.

### Phase 4 – Profilierung und Betrieb

- Erstdurchlauf und Vorschlagsworkflow.
- Berichte, Benachrichtigungen und Wochenprüfung.
- Ressourcenmessungen auf einem 16-GB-Windows-11-Laptop.
- Linux-Paketierung vorbereiten; macOS erst danach.
- signierte, bestätigungspflichtige Updates vorbereiten.

## Definition of Done für jeden vertikalen Schnitt

- Verhalten ist über eine reale Agent-API aus der UI erreichbar.
- Go-Tests, Integrationsfälle und TypeScript-Typecheck sind grün.
- Verträge und Migrationen sind versioniert.
- Fehlerzustände und Wiederaufnahme nach Neustart sind getestet.
- Keine Sicherheitsinvariante wurde aufgeweicht.
- Logs enthalten keine Geheimnisse oder vollständigen Nachrichtentexte.
- `README.md` und `TODO.md` spiegeln den tatsächlichen Stand.
- Keine versteckte Cloudabhängigkeit, Telemetrie oder Mail-Löschung wurde eingeführt.

## Startauftrag

Beginne mit einem kurzen Auditbericht des vorhandenen Backends. Liste konkrete Lücken, Risiken und überholte Annahmen auf. Erstelle danach einen umsetzbaren Plan für Phase 1 und implementiere unmittelbar den ersten kleinen vertikalen Schnitt. Frage nur dann nach, wenn eine Entscheidung Produktumfang, Datenschutz oder die No-Delete-Garantie wesentlich verändert. Bei gewöhnlichen technischen Detailentscheidungen wähle die sicherste, einfachste und gut testbare Variante und dokumentiere sie.
