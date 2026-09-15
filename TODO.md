# Mailmune – Projektstand und priorisierte Aufgaben

Ziel: zuerst ein sicherer, vollständig lokaler Trockenlauf mit einem echten IMAP-Konto. Danach folgen kontrollierte Verschiebungen, lernende Klassifikation, komfortable Einrichtung und Distribution. E-Mails werden niemals gelöscht.

## In Arbeit

### ⚠️ Testmodus (temporär – vor Release zurückbauen)

- [ ] `debugScanAllMessages` in `internal/service/scanner.go` wieder auf `false` setzen (aktuell `true`): speichert zu Debug-Zwecken ALLE gescannten Nachrichten als Entscheidung, auch unter der 60-%-Kandidatenschwelle, damit Nicht-Erkennungen in der Zuordnung inspectiert werden können.
- [ ] Den entfernten Kandidaten-Filter `item.score >= 0.6 &&` in `apps/desktop/src/App.tsx` (ReviewPage, `filtered`) wieder einfügen; siehe `TESTMODUS`-Kommentar dort.
- [ ] Hinweis: Beide Änderungen gehören zusammen. Danach werden wieder nur Kandidaten (≥ 60 %) gespeichert und angezeigt; der Test `TestScanLifecycleIdempotencyAndResume` setzt die Variable bereits selbst auf `false`.

### P0 – Nutzbarer Sicherheitskern

- [ ] Native Tauri-App auf Windows 11 vollständig kompilieren und Sidecar-Handshake prüfen (Rust-SSE-Weiterleiter ist per cargo check verifiziert; erster voller Desktop-Build steht aus)
- [ ] Testkonto sicher speichern, Verbindung testen und ausschließlich lesenden Trockenlauf ausführen
- [x] Sichere IMAP-MOVE-Transaktion mit Zielprüfung und Wiederherstellung implementieren (Zustandsmaschine, UIDVALIDITY-Recheck, Reconcile, Restore)
- [x] Reviewaktionen mit Verschiebung erst nach erfolgreichen MOVE-, Wiederholungs- und Absturztests aktivieren (Automatik zusätzlich durch Kalibrierungsgate gesperrt)
- [x] UI und Agent verbinden; Demo-Daten klar von echten Daten trennen (Desktop startet leer, Browser-Vorschau mit Demo)
- [x] Dashboard-Charts mit echten Verlaufsdaten verbinden
- [x] Benachrichtigungsseite mit echten Ereignissen verbinden (Scan-Abschluss/-Fehler, Wochenprüfung, geplante Prüffehler, offene Prüffälle; lokal persistiert, Demo-Einträge nur in der Browser-Vorschau)

### P1 – UI-Konsolidierung

- [x] Navigation gegen Figma-Node `14405:2376` prüfen: 44-px-Zeilen, keine Lücke zwischen normalen Nav-Einträgen, getrennte Account-Zone
- [x] Zähler-Pill auf 4 px Radius und 30 px Mindestbreite setzen
- [x] Trennlinie unter dem Navigationsheader ergänzen und Abstand über „Postfach“ bereinigen
- [x] Account-Switcher unten mit Abstand unter „Einstellungen“ ergänzen
- [x] STRATO-Platzhalteravatar in Orange ergänzen
- [ ] Provider-Avatare lokal zuordnen: STRATO, GMX, WEB.DE, Gmail; unbekannte Anbieter neutral grau
- [x] Card-Grautöne sowie Schriftgrößen von Suche, Filter, Dropdowns und View-Switch vereinheitlichen

## Offen

### P0 – IMAP, Sicherheit und Datenhaltung

- [ ] Echter STRATO-Trockenlauf mit mindestens 100 manuell geprüften Entscheidungen
- [x] IMAP-IDLE plus zehnminütiger Abgleich und Reconnect mit Backoff/Jitter
- [x] Wöchentlicher KI-Tiefscan zur konfigurierbaren Uhrzeit (je Postfach, verpasste Termine holt der Agent nach; Fenster reicht bis zum letzten Tiefscan, manuell per API auslösbar)
- [x] UID-basierte Synchronisierung mit sicherem Resync bei UIDVALIDITY-Wechsel und idempotenten Entscheidungen
- [ ] Credential Manager produktiv prüfen; Secret-Service- und Keychain-Adapter vorbereiten
- [x] Geheimnisse nur im Schlüsselbund, Profile/Lernmerkmale in SQLite, keine dauerhaften Nachrichtentexte
- [x] Echte Dashboard-Statistiken mit Eingangsdatum-Bezug; „Eingang“ zählt jede angekommene Mail auch im Produktionsmodus (datenschutzarmer Arrival-Log: nur Tag + Message-ID-Hash, dedupliziert, 730 Tage Aufbewahrung)
- [x] Nummerierte vorwärtslaufende Datenbankmigrationen
- [ ] Backup und Export von Profil- und Lernwissen mit Schema-, Versions- und Konfliktprüfung (Import der Lern-Baseline existiert bereits)
- [x] Nachweisende Tests, dass keine Lösch-, Papierkorb- oder Aufbewahrungsfunktion existiert (Methoden-Audit des IMAP-Clients plus Move-Erhaltungs- und Purge-Sicherheitstests)
- [ ] Externe Blacklists nur mit Herkunft, Lizenz, Signatur/Hash, Aktualitätsprüfung und Rollback evaluieren
- [ ] Marken-Domain-Liste von KOR-Labs/logo-trust (`domain_names.json`, BIMI/Mark-Certificate-basiert) als Basis für den Ausbau von `brandTokens` (brand_impersonation + brand_aligned_domain) evaluieren: https://github.com/KOR-Labs/logo-trust
  - ⚠️ Lizenz ist **CC-BY-SA-4.0** (nicht MIT, wie vermutet): ShareAlike-Pflicht bei Übernahme/Veröffentlichung eines abgeleiteten Datensatzes plus Namensnennung; kommerzielle Nutzung erlaubt, aber Copyleft-Folgewirkungen für eine ausgelieferte Kopie der Liste müssen vor Übernahme juristisch bewertet werden
  - Alternativen Weg prüfen: Liste nur als Recherche-/Inspirationsquelle nutzen und eine eigene, kleine Liste allgemein bekannter Marken führen (Fakten wie „Marke X gehört Domain Y“ sind urheberrechtlich dünn), statt den Datensatz zu kopieren
  - Repo-Aktivität ist gering (4 Commits, 0 Stars): Aktualität und Wartung vor jeder Übernahme prüfen; nur mit dokumentierter Herkunft und Version einbauen
- [ ] Wikidata-Exporte als Basis einer großen legitimen Markendomain-Liste evaluieren (✅ Lizenz **CC0 1.0** – kommerziell frei, keine Namensnennung, kein ShareAlike; die sauberste der verfügbaren Quellen)
  - Vom Nutzer bereitgestellte SPARQL-Exporte (liegen lokal unter `C:\Users\User\Desktop\`, nicht im Repo):
    - `query_World_First_5000.json` – 5.000 Unternehmen weltweit (`company`, `companyLabel`, `website`)
    - `query_GER_First_10000.json` – 10.000 deutsche Unternehmen/Marken (`item`, `itemLabel`, `website`; Typen: Unternehmen Q783794, Business Q4830453, Marke Q431289; Land Q183; offizielle Website P856; mit deutschem Wikipedia-Artikel)
    - Quelle/Query: https://query.wikidata.org (SPARQL im TODO-Kontext des Nutzers dokumentiert); Abrufdatum und Query bei Übernahme in die Herkunfts-Doku schreiben
  - Geplanter Verwendungszweck: `brandTokens` von der kleinen handgepflegten Liste auf eine breite, versionierte Positivliste legitimer Markendomains ausbauen – für `brand_impersonation` (Lookalikes bekannter Marken) und `brand_aligned_domain` (echte Markenmail mit alignierter Envelope + bestandener Auth)
  - Sicherheitsregel dabei: Listeneintrag allein ist NIEMALS ein Ham-Beweis (Absender-Header sind fälschbar); Vertrauen entsteht nur aus der Kombination Domain-in-Liste + Envelope-Alignment + SPF/DKIM-Pass, genau wie heute schon in `stageTrust`
  - Aufbereitungsschritte vor dem Einbau: URL → registrierbare Domain (eTLD+1) normalisieren, Duplikate entfernen, veraltete/defekte P856-Einträge stichprobenartig prüfen, kompakte verarbeitete Liste (statt Roh-JSON) mit Version + Abrufdatum + Query-Herkunft erzeugen; Größe/RAM-Impact der dann ~10–15k Domains im Klassifikator messen (Trie/Map, kein Regex)
- [ ] Blacklist-Updates ohne Telemetrie und unabhängig von App-Releases konzipieren
- [ ] Externe Client-Bewegungen (Thunderbird/Outlook) als Feedback erkennen, ohne Schleifen

### P0 – Klassifikation und Lernsystem

- [x] Deterministischen Regelkern für Header, Korrespondenz, Domains, Links, Mailinglisten und Authentifizierung fertigstellen (Stufen-Pipeline mit stabilen Evidence-Codes, Allow-/Deny-Listen)
- [x] Statistischen Lernfilter aus bestätigten Spam- und Fehlalarm-Beispielen implementieren (lokaler Naive Bayes, Idempotenz, Giftschutz)
- [x] Kalibrierungsmetriken (Precision, Recall, False-Positive-Rate) für den Lernfilter messen (Endpunkt + Automatik-Gate)
- [x] Opt-in-Import einer externen Lern-Baseline und Offline-Evaluierung gegen das dokumentierte MIT-Datenset (mltool, ohne Auslieferung im Repo)
- [x] Zurücksetzen des Lernwissens aus der UI (je Postfach; Entscheidungen, E-Mails und importierte Baseline bleiben erhalten)
- [ ] Export des Lernwissens aus der UI (anonymisiert, mit Herkunft/Version)
- [x] Ollama mit JSON-Schema, Prompt-Injection-Tests, Timeouts und niedriger Parallelität anbinden (nur Loopback, serialisiert, versionierter Prompt)
- [x] Lokales Modell mit datenschutzsicherem Profil-Kontext versorgen („RAG light": diskriminative Tokens und Absenderdomains aus bestätigten Reviews, niemals Rohtext oder vollständige Adressen, strikt je Postfach)
- [x] DMARC-ähnliches Envelope-Alignment: authentifizierte Markenmails (eigene Domain + passender Return-Path) erhalten ein starkes Vertrauenssignal; Subdomains und Marken-Geschwister zählen nicht als sender_mismatch; KI-Voten können starke Vertrauenssignale nicht in die Prüfliste heben
- [x] Fähigkeitstest als feste Proben-Suite umsetzen (persistierte Validierung je Konto, Empfehlungsliste in der UI)
- [ ] Reproduzierbaren Modellvergleich für 16-GB-Laptops umsetzen
- [ ] Wissen vom Modell entkoppeln: Profil, Regeln, Statistik und Beispiele bleiben modellunabhängig
- [ ] Modellwechsel mit Re-Benchmark, Schattenbetrieb und Rollback gestalten
- [ ] Scores unter 60 % nicht in die normale Prüfliste aufnehmen; nur anonymisiert zur Kalibrierung zählen
- [ ] Automatische Spam-Schwelle und Benachrichtigungsschwelle von 75 bis 99 % konfigurierbar machen
- [ ] SpamAway isoliert prüfen: Naive Bayes, Paketstruktur und Tests als Lernreferenz; Code/Dataset erst nach Lizenz- und Herkunftsprüfung verwenden

### P1 – Ersteinrichtung und Profile

- [ ] Leere Erststartseite: zuerst Sprache und Theme, danach geführte Postfachkonfiguration
- [ ] Erststart und „Postfach hinzufügen“ aus einer gemeinsamen wiederverwendbaren Flow-Definition bauen
- [ ] Schritte: App-Einstellungen, IMAP, Postfachname, Geschäftsprofil, Whitelist, Nachrichtentypen, Filterverhalten, Schwellen, Zielordner und Prüfintervalle
- [ ] Nach dem Anlegen neue Profilseite öffnen und das neue Postfach automatisch auswählen
- [ ] Erstdurchlauf mit Start, Abbruch, Fortschrittsbalken, Live-Restzeit und Abschlussbericht
- [ ] Erstdurchlauf in Einstellungen erneut startbar; Umfang als gesamter Bestand oder Zeitraum konfigurierbar
- [ ] Sicherer Standard bleibt höchstens 90 Tage/1.000 Nachrichten
- [ ] Profiländerungsverlauf und Regelvorschläge mit menschlicher Bestätigung
- [ ] Lern- und Profildaten zwischen Postfächern übertragen
  - [ ] Datenschutzgeprüften Export ausschließlich anonymisierter, allgemeiner Lernmerkmale anbieten
  - [ ] Vollständiges Profilpaket mit Regeln, Präferenzen und postfachspezifischen Lernmerkmalen separat exportieren/importieren
  - [ ] Vollständige Profile direkt über ein durchsuchbares Zielpostfach-Dropdown übertragen können
  - [ ] Vor Import Inhalt, Herkunft, Umfang und überschreibende Änderungen anzeigen und ausdrücklich bestätigen lassen
  - [ ] Nachrichtentexte, Zugangsdaten und unmittelbar personenbezogene Inhalte vom anonymisierten Export technisch ausschließen

### P1 – App-Einstellungen und Persistenz

- [x] „App-Einstellungen“ für Sprache und Theme klar von Postfachprofilen trennen
- [x] Suchbares Sprach-Dropdown mit Deutsch und vorbereitetem Englisch-Fallback
- [ ] Zentrales i18n-Modul für UI, Fehler und Standardbenachrichtigungen; Englisch als Fallback
- [ ] Sprachwahl beeinflusst App/KI-Anweisungen, niemals Originalinhalt der E-Mails
- [ ] Vollständige Theme-Logik umsetzen: System (Standard), Dunkel und Hell
  - [ ] Zentrale semantische Farb-Tokens statt fest codierter Dark-Theme-Farben verwenden
  - [ ] Echtes Light Theme für sämtliche Seiten, Dialoge, Tabellen, Charts, Tooltips, Toasts, Scroll-Fades und Interaktionszustände gestalten
  - [ ] System-Theme live auf Betriebssystemwechsel reagieren lassen
  - [ ] Theme-Auswahl persistent speichern und bereits vor dem ersten Rendern anwenden, damit kein falsches Theme aufblitzt
  - [ ] Kontrast, Fokuszustände, deaktivierte Zustände und Corporate-Orange in beiden Themes prüfen
- [ ] Account-Switcher mit Plus-Button; Profilansichten beziehen sich auf das aktive Postfach
- [ ] Letzte Seite, aktives Postfach, Filter, Sortierung, Zeitraum, Sprache, Theme und Einstellungen persistent speichern
- [ ] Standardfilter nicht als aktive Abweichung markieren
- [ ] „Postfächer“ kontextgerecht in „Postfach“ oder „Postfächer verwalten“ umbenennen

### P1 – Filter und Tabelle

- [x] Kurze waagerechte Verbinder ohne Lücke zwischen Filterbutton und dynamischen Dropdowns ergänzen
- [x] Aktive Nicht-Standardfilter vollständig invertiert weiß hervorheben
- [x] Filtericon bei Abweichungen in Reseticon wechseln; Icon setzt nur aktive Filter zurück
- [x] Status/Score sowie Betreff/Datum in Header und Tabelleninhalt tauschen
- [ ] Nachrichtendetails mit vollständigen Reason-Codes ergänzen
- [ ] Such-, Filter- und Tabellenzustand je Postfach persistent speichern

### P1 – Section-Indicator

- [x] Wiederverwendbaren rechten Section-Indicator nach Figma-Frame `App_Wrap Einstellunen--Dialog--Edit` bauen
- [x] Aktiven Abschnitt durch längsten Strich anzeigen
- [x] Hover zeigt Abschnittsnamen; Klick scrollt weich zum Abschnitt
- [x] Übersicht/Einstellungen verwenden semantische Abschnitte; bei Benachrichtigungen entspricht jeder Strich einem Eintrag
- [x] Höchstens zwölf Striche gleichzeitig anzeigen; sichtbares Fenster folgt der aktuellen Position
- [x] IntersectionObserver, Tastaturbedienung und reduzierte Bewegung berücksichtigen
- [x] Nur auf vertikal scrollbaren Seiten anzeigen; Zuordnung behält eigene Scroll-Indikatoren

### P1 – KI-Modellverwaltung

- [ ] Global genau ein aktives KI-Modell; Postfachprofile bleiben unabhängig
- [ ] Modell-Dialog für Ollama-Endpunkt, Modellauswahl, Verbindungstest, Fähigkeitstest und Ressourcenprofil
- [ ] Separater Anleitungsdialog für Ollama, Hardwareklassen und empfohlene Modelle
- [ ] Empfehlungen versionieren; funktionierendes Produktivmodell niemals automatisch wechseln
- [ ] Vor Löschen/Wechsel warnen; Wissen bleibt erhalten, Kalibrierung und Vergleichstest laufen erneut

### P1 – Feedback und Status

- [ ] Toast-System im Card-Stil: Info Cyan, Erfolg Grün, Warnung Orange, Fehler Rot; nur Icon/Indicator farbig
- [ ] Bei „Autonom“ warnen: ab gewählter Schwelle wird in den Spamordner verschoben, niemals gelöscht
- [ ] Native Benachrichtigungen mit „Als Spam bestätigen“ und „Fehlalarm“, ohne Löschaktion
- [ ] Erstdurchlauf, Scans und Modelltests mit einheitlichen Fortschritts- und Abbruchzuständen

### P2 – Distribution und Qualität

- [ ] Signierter Stable-/Beta-Updater und Windows-Installer ohne Administratorrechte
- [ ] Authenticode, SmartScreen und reproduzierbare Builds prüfen
- [ ] Linux/Secret Service; macOS/Keychain nach stabiler Windows-/Linux-Version
- [ ] Unter 300 MB Leerlauf-RAM ohne Ollama; sequenzielle 4B-Inferenz auf 16 GB RAM
- [ ] Accessibility, Tastatur, kleine Fenster, lange Texte, Fehler- und Leerezustände testen
- [ ] Visuelle Endabnahme gegen relevante Figma-Nodes

## Erledigt

### Technik

- [x] Eigenständiges Repository ohne Übernahme des GPLv3-Projekts `imap-spam-cleaner`
- [x] Versionierte API-Verträge und dreistufige Sicherheitsrichtlinie begonnen
- [x] Go-Agent-Grundgerüst mit SQLite, Schlüsselbund, TLS-IMAP-Test und lesendem Trockenlauf
- [x] Lokale API mit Sitzungstoken, Host-Prüfung und Origin-Sperre
- [x] Löschendpunkte ausgeschlossen und unsicherer COPY-/EXPUNGE-Fallback gesperrt
- [x] Hintergrundscans mit Fortschritt, Abbruch, Wiederaufnahme und SSE-Eventstream
- [x] Kontrollierter lokaler IMAP-Testserver für Integrations- und Absturztests

### Design

- [x] Tauri-/React-/shadcn-App-Shell und Figma-basierte Navigation
- [x] Dashboard, Benachrichtigungen, Einstellungen und Zuordnung als interaktiver Prototyp
- [x] Tabelle mit Suche, Ansichtswechsel, Zeitraum, Sortierung, Mehrfachauswahl und bidirektionalem Scrollen
- [x] Scroll-Fades, Richtungsindikatoren und animierte Sammelaktionen
- [x] Kontoassistent als Dialogprototyp und Demo-Cards für Postfach/KI-Modell

## Definition „brauchbar“ für den ersten echten Test

- [ ] Windows-App startet nativ und Agent verbindet sich zuverlässig
- [ ] Echtes Konto wird sicher im Schlüsselbund gespeichert
- [ ] Trockenlauf liest ausschließlich und zeigt nachvollziehbare Entscheidungen
- [ ] Mindestens 100 Entscheidungen wurden manuell geprüft
- [ ] Keine automatische Verschiebung vor Kalibrierung und ausdrücklicher Aktivierung
