# Mailmune – Projektstand und priorisierte Aufgaben

Ziel: zuerst ein sicherer, vollständig lokaler Trockenlauf mit einem echten IMAP-Konto. Danach folgen kontrollierte Verschiebungen, lernende Klassifikation, komfortable Einrichtung und Distribution. E-Mails werden niemals gelöscht.

## In Arbeit

### P0 – Nutzbarer Sicherheitskern

- [ ] Native Tauri-App auf Windows 11 vollständig kompilieren und Sidecar-Handshake prüfen (inkl. Verifikation des Rust-SSE-Weiterleiters beim ersten Build)
- [ ] Testkonto sicher speichern, Verbindung testen und ausschließlich lesenden Trockenlauf ausführen
- [ ] Sichere IMAP-MOVE-Transaktion mit Zielprüfung und Wiederherstellung implementieren
- [ ] Reviewaktionen mit Verschiebung erst nach erfolgreichen MOVE-, Wiederholungs- und Absturztests aktivieren
- [x] UI und Agent verbinden; Demo-Daten klar von echten Daten trennen (Desktop startet leer, Browser-Vorschau mit Demo)
- [ ] Dashboard-Charts und Benachrichtigungen mit echten Verlaufsdaten verbinden

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
- [ ] IMAP-IDLE plus zehnminütiger Abgleich und Standby-Reconnect
- [x] UID-basierte Synchronisierung mit sicherem Resync bei UIDVALIDITY-Wechsel und idempotenten Entscheidungen
- [ ] Credential Manager produktiv prüfen; Secret-Service- und Keychain-Adapter vorbereiten
- [x] Geheimnisse nur im Schlüsselbund, Profile/Lernmerkmale in SQLite, keine dauerhaften Nachrichtentexte
- [x] Nummerierte vorwärtslaufende Datenbankmigrationen
- [ ] Backup, Export und Import von Profil- und Lernwissen mit Schema-, Versions- und Konfliktprüfung
- [ ] Nachweisende Tests, dass keine Lösch-, Papierkorb- oder Aufbewahrungsfunktion existiert
- [ ] Externe Blacklists nur mit Herkunft, Lizenz, Signatur/Hash, Aktualitätsprüfung und Rollback evaluieren
- [ ] Blacklist-Updates ohne Telemetrie und unabhängig von App-Releases konzipieren

### P0 – Klassifikation und Lernsystem

- [x] Deterministischen Regelkern für Header, Korrespondenz, Domains, Links, Mailinglisten und Authentifizierung fertigstellen (Stufen-Pipeline mit stabilen Evidence-Codes, Allow-/Deny-Listen)
- [x] Statistischen Lernfilter aus bestätigten Spam- und Fehlalarm-Beispielen implementieren (lokaler Naive Bayes, Idempotenz, Giftschutz)
- [ ] Kalibrierungsmetriken (Precision, Recall, False-Positive-Rate) für den Lernfilter messen
- [ ] Export/Import/Reset des Lernfilters und Evaluierung gegen das dokumentierte MIT-Datenset (nur offline, ohne Auslieferung)
- [x] Ollama mit JSON-Schema, Prompt-Injection-Tests, Timeouts und niedriger Parallelität anbinden (nur Loopback, serialisiert, versionierter Prompt)
- [x] Fähigkeitstest als feste Proben-Suite umsetzen
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
