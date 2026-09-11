# Ereignisarchiv (optional)

Das Archiv speichert FRITZ!Box-Ereignisse aus SOAP `DeviceInfo/GetDeviceLog` und diagnostizierte SOAP-/Lua-/API-Abfragefehler mit bereinigten Details. Prometheus erhält weiterhin nur Metriken. Grafana 9.1.6 braucht keine Änderung und kein Plugin. Powerline-Spektren werden nicht aktiviert.

## Portainer

Image: `senecaiii/fritzbox_exporter:api-v0-test9` (Linux ARM64).

Pro Exporter ein eigenes persistentes Volume nach `/data` einbinden. Niemals zwei Exporter dieselbe SQLite-Datei schreiben lassen. Daten lokal speichern, nicht auf einer SMB-/NFS-Freigabe. Bei einem späteren Umzug das Archiv bei gestopptem Container kopieren.

Zusätzliche ENV:

| Variable | 5690 | 4040 |
|---|---|---|
| `ARCHIVE_FILE` | `/data/events.db` | `/data/events.db` (separates Volume!) |
| `ARCHIVE_TOKEN` | eigener zufälliger Schlüssel, mindestens 16 Zeichen | eigener zufälliger Schlüssel, mindestens 16 Zeichen |
| `ARCHIVE_TIMEZONE` | `Europe/Berlin` | `Asia/Singapore` |
| `ARCHIVE_INTERVAL` | `30s` | `30s` |
| `ARCHIVE_RETENTION` | `2160h` | `2160h` |
| `ARCHIVE_MAX_ROWS` | `50000` | `50000` |
| `ARCHIVE_MAX_MB` | `64` | `64` |

Die Zeitzone muss zur auf der jeweiligen FRITZ!Box angezeigten Ereigniszeit passen. Die 4040 erhält nicht automatisch die Zeitzone des Exporter-Servers. Ungültige Archivkonfiguration stoppt den Start sichtbar. Ohne `ARCHIVE_FILE` bleiben Archiv und HTTP-Routen deaktiviert. Keine Änderung der metrics-Dateien erforderlich; bestehende Zugangsdaten, Entry Point, Ports und Timeout-Einstellungen beibehalten. Laufende Abfragen und Archiv-Abfragen besitzen getrennte Zeitbudgets.

Nach dem Aktualisieren/Neuerstellen öffnen:

- 5690: `http://192.168.188.151:9059/events`
- 4040: `http://192.168.188.151:9044/events`

Ohne `ARCHIVE_TOKEN` �ffnet sich die Ereignisansicht direkt; jeder Client mit Netzwerkzugriff auf diesen Exporter kann dann das Archiv lesen. Ist ein Schl�ssel gesetzt (mindestens 16 Zeichen), bleiben Ansicht und API gesch�tzt. Der Schl�ssel bleibt nur im Arbeitsspeicher der Browserseite, nicht in URLs, Cookies oder LocalStorage. Bei HTTP wird er unverschl�sselt �bertragen.

## Anzeige und Speicherung

Die Ansicht bietet Zeitraum-, Quellen-, Verfahrens- und Textfilter sowie Blättern. Jede Instanz zeigt ihre Box. Die beiden Instanzen werden über ihre jeweilige Adresse geöffnet; es gibt noch keine serverübergreifende Sammelansicht.

Gespeichert werden Ereigniszeit in UTC, unveränderte Router-Zeitangabe, erster/letzter Beobachtungszeitpunkt, Quelle und Meldung. Unlesbare Router-Zeitangaben bleiben als Text erhalten. Zeitfilter und Sortierung verwenden die letzte Beobachtung. Browserzeiten werden in der lokalen Browser-Zeitzone dargestellt.

Identische Router-Zeilen werden nach Neustarts nicht erneut eingefügt, solange sie noch im Archiv vorhanden sind. Mehrfach vorhandene identische Zeilen bleiben nach ihrer Anzahl erhalten. Bereits von FRITZ!OS zusammengefasste Meldungen werden als Originaltext gespeichert; ändert sich dieser Text oder die darin enthaltene Anzahl, wird die neue Fassung als eigener Datensatz gesichert. Verdeckte Einzelereignisse können nicht rekonstruiert werden.

Ein wiederholter gleicher Abfragefehler aktualisiert Anzahl und letzte Beobachtung. Ändert sich der Fehlertext, beginnt ein neuer Eintrag. Die nächste erfolgreiche Auswertung erzeugt eine Wiederherstellungsmeldung. Erfolg kann wie bei den bisherigen Metriken aus einem noch gültigen Cache kommen. Allgemeine Prozess-Logs werden nicht vollständig übernommen; gespeichert werden Abfragediagnosen samt zugeordneten Fehlerdetails. Zugangspasswort, Session-Parameter und Query-Strings in Fehler-URLs werden entfernt. Routertexte und Netzwerkinformationen sind weiterhin privat.

SQLite schreibt Transaktionen dauerhaft; die Aufzeichnung läuft auch ohne Prometheus-Scrapes. Abfragefehler werden bei den tatsächlichen Metrikabfragen aufgezeichnet. Begrenzte Warteschlange und Schreibthread verhindern, dass langsamer Speicher die Metrikabfrage blockiert. Bei hartem Abbruch des Exporters können noch nicht geschriebene Warteschlangeneinträge fehlen. Normaler Container-Stop beendet laufende Arbeit und leert die Warteschlange; dafür ausreichend Stop-Timeout vorsehen (z.B. 70 Sekunden).

Standard: 90 Tage seit letzter Beobachtung und höchstens 50.000 Einträge. Alte Einträge werden bei Schreibvorgängen entfernt. SQLite kann freigegebene Seiten wiederverwenden; die Dateigröße muss deshalb nicht sinken. Die Hauptdatenbank ist auf 64 MiB begrenzt, Journaldateien benötigen zusätzlich Platz. Ist die Kapazität vor Erreichen der Aufbewahrungsgrenzen erschöpft, werden Schreibfehler sichtbar und der letzte erfolgreiche Sicherungszeitpunkt bleibt stehen; dann Grenze erhöhen oder Aufbewahrung reduzieren. Die Archivbegrenzung ist kein Versprechen lückenloser Aufzeichnung.

Der letzte erfolgreiche Sicherungszeitpunkt sowie Abruf-, Schreib- und Warteschlangenfehler erscheinen in der Ansicht und als vier zusätzliche Prometheus-Metriken mit Präfix `fritzbox_exporter_archive_`. Zähler und Statuszeitpunkt beginnen beim Exporter-Start neu; die Ereignisse bleiben gespeichert. Eine beim Start noch laufende SOAP-Diensterkennung kann zunächst als Abruffehler erscheinen und wird bei Erfolg geschlossen.

Bei Ausfall der 5690 bleibt das Archiv erreichbar, solange der TWS und dessen Netzwerkzugang funktionieren. Die Abfrage der entfernten 4040 benötigt weiterhin das VPN. Zwischen zwei erfolgreichen Abfragen verlorene Router-Ereignisse können nicht nachträglich gerettet werden.

## Lesende Schnittstelle

`GET /api/events`; nur bei konfiguriertem Schl�ssel ist der Header `Authorization: Bearer <ARCHIVE_TOKEN>` erforderlich.

Optionale Parameter: `gateway`, `kind` (`router`, `query`, `recovery`), `backend` (`router`, `soap`, `lua`, `api`, `archive`), `q` (Textsuche), `from` und `to` (RFC3339-Zeit), `limit` (1–500, Standard 200), `offset` (Standard 0).

Antwort: JSON mit `events` und Archivstatus. Keine SQL-Abfragen, keine schreibenden HTTP-Endpunkte. Die begrenzte, parametrisierte Schnittstelle kann später als Datenquelle eines neueren Grafana dienen.

## Backup / Umzug

Container stoppen, komplettes `/data`-Volume sichern, auf dem Ziel wieder als `/data` einbinden und gleiche ENV verwenden. Beim Kopieren einer laufenden Datenbank reicht die `.db`-Datei allein im WAL-Modus nicht aus; deshalb den Container für diese einfache Sicherungsmethode stoppen. Das aktuelle Docker-Image wird nur für ARM64 veröffentlicht; für ein x86-Synology-System wäre zusätzlich ein AMD64-Build erforderlich.
