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
| `content` | `pressure_language` | Spam | Druck- oder Drohformulierung (sofort handeln, Sperrung, letzte Warnung …) |
| `content` | `reward_bait` | Spam | Lockangebot (Gewinn, Geschenk, Bonus, „Sie gehören zu den …“) |
| `content` | `verification_request` | Spam | Aufforderung, Identität/Zugangsdaten zu bestätigen oder zu aktualisieren |
| `content` | `financial_pressure` | Spam | Finanzielle Druckformulierung (offene Zahlung, Mahnung, Inkasso) |
| `content` | `subject_anomaly` | Spam | Auffällige Zeichensetzung (`!!`) oder Blockschrift im Betreff |
| `sender_integrity` | `sender_digit_pattern` | Spam | Absenderdomain mit langen Ziffernfolgen (≥ 4) – maschinell erzeugt |
| `sender_integrity` | `machine_generated_domain` | Spam | Domain-Label wirkt automatisch zusammengesetzt (überlang + Ziffern oder niedriger Vokalanteil) |
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
  verhindert automatische Verschiebungen.
- Der statistische Lerner wird ausschließlich aus menschlich bestätigten
  Reviews trainiert; ein Modell darf erst ab 20 bestätigten Beispielen und nur
  mit Beispielen aus beiden Klassen Beiträge liefern.
