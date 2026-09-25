# Mailmune – Evidence-Codes (v1)

Jede Entscheidung trägt nachvollziehbare Evidence-Einträge mit stabilem
`code`. Codes dürfen ihre Bedeutung nicht ändern; neue Signale bekommen neue
Codes. Gruppen fassen unabhängige Signalquellen zusammen – automatische
Aktionen verlangen mindestens zwei unabhängige Gruppen.

## Gruppen und Codes

| Gruppe | Code | Richtung | Bedeutung |
|---|---|---|---|
| `relationship` | `known_correspondent` | Ham | Bekannter Korrespondenzpartner (z. B. aus dem eigenen Postausgang, nur lesend ermittelt) |
| `relationship` | `allow_sender` | Ham | Absender steht auf der Vertrauensliste des Profils |
| `relationship` | `allow_domain` | Ham | Domain steht auf der Vertrauensliste des Profils |
| `rules` | `deny_sender` | Spam | Absender steht auf der Sperrliste des Profils |
| `rules` | `deny_domain` | Spam | Domain steht auf der Sperrliste des Profils |
| `rules` | `deny_keyword` | Spam | Gesperrtes Schlüsselwort in Betreff oder Text |
| `authentication` | `auth_pass` | Ham | Plausible SPF/DKIM/DMARC-Pass-Hinweise (untrusted Input, nur schwaches Signal) |
| `authentication` | `auth_fail` | Spam | SPF/DKIM/DMARC-Fail-Hinweise (untrusted Input) |
| `authentication` | `brand_aligned_domain` | Ham | Bekannte Marke sendet von ihrer eigenen Domain mit passend ausgerichteter Envelope-Adresse (Return-Path) und bestandener Authentifizierung – DMARC-ähnliches Alignment als Vertrauenssignal. Markendomains kommen aus der kleinen eingebauten Liste plus der CC0-Wikidata-Exportliste (`internal/classifier/data/legit_domains.txt`, ~10.900 Domains) |
| `content` | `pressure_language` | Spam | Druck- oder Drohformulierung (sofort handeln, Sperrung, letzte Warnung …) |
| `content` | `reward_bait` | Spam | Lockangebot (Gewinn, Geschenk, Bonus, „Sie gehören zu den …“) |
| `content` | `verification_request` | Spam | Aufforderung, Identität/Zugangsdaten zu bestätigen oder zu aktualisieren |
| `content` | `financial_pressure` | Spam | Finanzielle Druckformulierung (offene Zahlung, Mahnung, Inkasso) |
| `content` | `subject_anomaly` | Spam | Auffällige Zeichensetzung (`!!`) oder Blockschrift im Betreff |
| `content` | `subject_emoji` | Spam | Emojis/Piktogramme im Betreff – unüblich für seriöse/formelle Nachrichten (schwaches Signal, hebt den Score nur in Kombination) |
| `content` | `server_marked_spam` | Spam | Der Eingangs-Server hat die Mail bereits als Spam markiert (Betreff-Marker wie `*** Spam ***`, `[Spam]`, `Spam:`) |
| `content` | `blackmail_threat` | Spam | Erpressung/Sextortion: Drohung mit Veröffentlichung von Video/Fotos oder Kontakt zur Familie |
| `sender_integrity` | `sender_digit_pattern` | Spam | Absenderdomain mit langen Ziffernfolgen (≥ 4) – maschinell erzeugt |
| `sender_integrity` | `machine_generated_domain` | Spam | Domain-Label wirkt automatisch zusammengesetzt (überlang + Ziffern oder niedriger Vokalanteil). Reine Rechtsform-Segmente (GmbH, AG, …) zählen nicht mit, damit echte Firmendomains wie `mittelstand-gmbh.de` nicht getroffen werden |
| `sender_integrity` | `spoofed_sender` | Spam | Eingehende Mail gibt die eigene Kontodomain als Absender an – sehr wahrscheinlich gefälscht (Spoofing, z. B. Sextortion). AUSGENOMMEN: eigene System-/Website-Mails mit passender Envelope und ohne Auth-Fehlschlag (siehe `own_domain_aligned`) |
| `relationship` | `own_domain_aligned` | Ham | Absender von der eigenen Domain mit Return-Path auf eigener Domain/Subdomain und keinem Auth-Fehlschlag = eigene Infrastruktur (WordPress, Shop, Anlagen). Fehlende Envelope zählt bewusst NICHT als intern |
| `sender_integrity` | `sender_mismatch` | Spam | Return-Path (Envelope) weicht vom From-Header ab – Spoofing-Hinweis. Nicht gewertet wird DMARC-ähnliches Alignment: Subdomains voneinander und Domains derselben bekannten Marke (z. B. google.com/googlemail.com) gelten als passend; Mailinglisten sind ausgenommen |
| `sender_integrity` | `brand_impersonation` | Spam | Absenderdomain enthält eine bekannte Marke (PayPal, ADAC, Telekom …), ohne deren eigene Domain zu sein. Neben der eingebauten Liste werden ~1.400 Markentokens aus den CC0-Wikidata-Exporten geprüft (`internal/classifier/data/brand_tokens.txt`, generiert mit `cmd/brandlist`); Domains der Positivliste werden vorher ausgenommen |
| `content` | `spam_vertical_content` | Spam | Inhalt passt zu einer generischen Massen-Spam-Kampagnenkategorie (Diät/Gesundheit, Krypto-Investment, Potenz, Krankenkassen-Lockangebote, Wallet-KYC, Kaltakquise mit Förder-Versprechen). Absenderunabhängiges Kampagnen-Vokabular, höchste Kategorie zählt einmal |
| `profile` | `profile_mismatch` | Spam | Kampagnentreffer UND keinerlei inhaltliche Überschneidung mit dem hinterlegten Postfachprofil (≥ 3 aussagekräftige Profilwörter nötig). Feuert nie ohne Kampagnentreffer – ungewöhnliche, aber legitime Post bleibt unangetastet |
| `profile` | `profile_topic_match` | Ham | Inhalt trifft ≥ 2 KI-kompilierte Erwartungsthemen dieses Postfachs (`profile_models`, aus dem Profiltext generiert, in den Postfach-Einstellungen einsehbar/deaktivierbar) |
| `profile` | `profile_offtopic_campaign` | Spam | Inhalt trifft eine KI-kompilierte, profilspezifische Fremdkampagne (≥ 2 Term-Treffer oder 1 sehr spezifischer langer Term). Literal-Substring-Matching, strikt validiert und begrenzt – niemals Regex/Code |
| `content` | `fake_endorsement` | Spam | Bewerbung mit erfundenem Prominenten-/Experten-/TV-Endorsement („Empfohlen von Dr. …“, „bekannt aus dem Fernsehen“, „wie im TV gesehen“) – generisches Betrugsmuster |
| `sender_integrity` | `tv_show_domain_abuse` | Spam | Absenderdomain missbraucht den Namen einer bekannten TV-Show (Die Höhle der Löwen, Shark Tank, …) – offizielle Sender versenden nie von solchen Domains |
| `links` | `url_shortener` | Spam | Verkürzte Links (bit.ly, tinyurl, …) |
| `links` | `suspicious_links` | Spam | Ungewöhnlich viele Links |
| `mailing_list` | `list_unsubscribe` | Ham | Reguläre Mailinglisten-Kopfzeile vorhanden |
| `attachment` | `dangerous_attachment_type` | Spam | Riskanter Anhangstyp (.exe, .js, .scr, .iso) – nur Metadaten, Anhänge werden nie geöffnet |
| `statistical` | `statistical_spam` | Spam | Lokaler Lernfilter erkennt ein bestätigtes Spam-Muster |
| `statistical` | `statistical_ham` | Ham | Lokaler Lernfilter erkennt ein bestätigtes Ham-Muster |
| `model` | `local_model_spam` / `local_model_ham` / `local_model_uncertain` | beides | Urteil des lokal validierten Ollama-Modells; zählt höchstens als eine Signalgruppe und löst allein nie eine Aktion aus |

## Garantien

- `Authentication-Results` wird nur als nicht vertrauenswürdiges Eingangssignal
  verwendet und niemals als Beweis.
- Ein starkes Vertrauenssignal (`StrongTrustSignal`) begrenzt den Score und
  verhindert automatische Verschiebungen. Bei einer KI-Prüfung blockiert es
  außerdem jede Score-Anhebung durch ein Spam-Votum des Modells: Eine
  authentifizierte, envelope-ausgerichtete Marken- oder explizit vertraute
  Mail bleibt auf dem Stand der deterministischen Regeln (typisch < 10 %),
  auch wenn das Modell „spam“ sagt. Das Votum bleibt als Evidence sichtbar.
  Explizite Sperrlisten (Deny-Regeln) stechen implizites Vertrauen weiterhin aus.
- Der statistische Lerner wird ausschließlich aus menschlich bestätigten
  Reviews trainiert; ein Modell darf erst ab 20 bestätigten Beispielen und nur
  mit Beispielen aus beiden Klassen Beiträge liefern.
