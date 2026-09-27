# Ereignisarchiv (optional)

Das Archiv speichert FRITZ!Box-Ereignisse aus SOAP `DeviceInfo/GetDeviceLog` und diagnostizierte SOAP-/Lua-/API-Abfragefehler mit bereinigten Details. Prometheus erhÃ¤lt weiterhin nur Metriken. Grafana 9.1.6 braucht keine Ã„nderung und kein Plugin. Powerline-Spektren werden nicht aktiviert.

## Portainer

Image: `senecaiii/fritzbox_exporter:v1.1.2` (Linux ARM64).

Pro Exporter ein eigenes persistentes Volume nach `/data` einbinden. Niemals zwei Exporter dieselbe SQLite-Datei schreiben lassen. Daten lokal speichern, nicht auf einer SMB-/NFS-Freigabe. Bei einem spÃ¤teren Umzug das Archiv bei gestopptem Container kopieren.

ZusÃ¤tzliche ENV:

| Variable | 5690 | 4040 |
|---|---|---|
| `ARCHIVE_FILE` | `/data/events.db` | `/data/events.db` (separates Volume!) |
| `ARCHIVE_TOKEN` | optional; falls gesetzt, mindestens 16 Zeichen | optional; falls gesetzt, mindestens 16 Zeichen |
| `ARCHIVE_TIMEZONE` | `Europe/Berlin` | `Asia/Singapore` |
| `ARCHIVE_INTERVAL` | `30s` | `30s` |
| `ARCHIVE_RETENTION` | `2160h` | `2160h` |
| `ARCHIVE_MAX_ROWS` | `50000` | `50000` |
| `ARCHIVE_MAX_MB` | `64` | `64` |

Die Zeitzone muss zur auf der jeweiligen FRITZ!Box angezeigten Ereigniszeit passen. Die 4040 erhÃ¤lt nicht automatisch die Zeitzone des Exporter-Servers. UngÃ¼ltige Archivkonfiguration stoppt den Start sichtbar. Ohne `ARCHIVE_FILE` bleiben Archiv und HTTP-Routen deaktiviert. Keine Ã„nderung der metrics-Dateien erforderlich; bestehende Zugangsdaten, Entry Point, Ports und Timeout-Einstellungen beibehalten. Laufende Abfragen und Archiv-Abfragen besitzen getrennte Zeitbudgets.

Nach dem Aktualisieren/Neuerstellen Ã¶ffnen:

- 5690: `http://exporter-host:9059/events`
- 4040: `http://exporter-host:9044/events`

Ohne `ARCHIVE_TOKEN` Ã¶ffnet sich die Ereignisansicht direkt; jeder Client mit Netzwerkzugriff auf diesen Exporter kann dann das Archiv lesen. Ist ein SchlÃ¼ssel gesetzt (mindestens 16 Zeichen), bleiben Ansicht und API geschÃ¼tzt. Der SchlÃ¼ssel bleibt nur im Arbeitsspeicher der Browserseite, nicht in URLs, Cookies oder LocalStorage. Bei HTTP wird er unverschlÃ¼sselt Ã¼bertragen.

## Anzeige und Speicherung

Die Ansicht bietet Zeitraum-, Quellen-, Verfahrens- und Textfilter sowie BlÃ¤ttern. Jede Instanz zeigt ihre Box. Die beiden Instanzen werden Ã¼ber ihre jeweilige Adresse geÃ¶ffnet; es gibt noch keine serverÃ¼bergreifende Sammelansicht.

Gespeichert werden Ereigniszeit in UTC, unverÃ¤nderte Router-Zeitangabe, erster/letzter Beobachtungszeitpunkt, Quelle und Meldung. Unlesbare Router-Zeitangaben bleiben als Text erhalten. Zeitfilter und Sortierung verwenden die letzte Beobachtung. Browserzeiten werden in der lokalen Browser-Zeitzone dargestellt.

Identische Router-Zeilen werden nach Neustarts nicht erneut eingefÃ¼gt, solange sie noch im Archiv vorhanden sind. Mehrfach vorhandene identische Zeilen bleiben nach ihrer Anzahl erhalten. Bereits von FRITZ!OS zusammengefasste Meldungen werden als Originaltext gespeichert; Ã¤ndert sich dieser Text oder die darin enthaltene Anzahl, wird die neue Fassung als eigener Datensatz gesichert. Verdeckte Einzelereignisse kÃ¶nnen nicht rekonstruiert werden.

Ein wiederholter gleicher Abfragefehler aktualisiert Anzahl und letzte Beobachtung. Ã„ndert sich der Fehlertext, beginnt ein neuer Eintrag. Die nÃ¤chste erfolgreiche Auswertung erzeugt eine Wiederherstellungsmeldung. Erfolg kann wie bei den bisherigen Metriken aus einem noch gÃ¼ltigen Cache kommen. Allgemeine Prozess-Logs werden nicht vollstÃ¤ndig Ã¼bernommen; gespeichert werden Abfragediagnosen samt zugeordneten Fehlerdetails. Zugangspasswort, Session-Parameter und Query-Strings in Fehler-URLs werden entfernt. Routertexte und Netzwerkinformationen sind weiterhin privat.

SQLite schreibt Transaktionen dauerhaft; die Aufzeichnung lÃ¤uft auch ohne Prometheus-Scrapes. Abfragefehler werden bei den tatsÃ¤chlichen Metrikabfragen aufgezeichnet. Begrenzte Warteschlange und Schreibthread verhindern, dass langsamer Speicher die Metrikabfrage blockiert. Bei hartem Abbruch des Exporters kÃ¶nnen noch nicht geschriebene WarteschlangeneintrÃ¤ge fehlen. Normaler Container-Stop beendet laufende Arbeit und leert die Warteschlange; dafÃ¼r ausreichend Stop-Timeout vorsehen (z.B. 70 Sekunden).

Standard: 90 Tage seit letzter Beobachtung und hÃ¶chstens 50.000 EintrÃ¤ge. Alte EintrÃ¤ge werden bei SchreibvorgÃ¤ngen entfernt. SQLite kann freigegebene Seiten wiederverwenden; die DateigrÃ¶ÃŸe muss deshalb nicht sinken. Die Hauptdatenbank ist auf 64 MiB begrenzt, Journaldateien benÃ¶tigen zusÃ¤tzlich Platz. Ist die KapazitÃ¤t vor Erreichen der Aufbewahrungsgrenzen erschÃ¶pft, werden Schreibfehler sichtbar und der letzte erfolgreiche Sicherungszeitpunkt bleibt stehen; dann Grenze erhÃ¶hen oder Aufbewahrung reduzieren. Die Archivbegrenzung ist kein Versprechen lÃ¼ckenloser Aufzeichnung.

Der letzte erfolgreiche Sicherungszeitpunkt sowie Abruf-, Schreib- und Warteschlangenfehler erscheinen in der Ansicht und als vier zusÃ¤tzliche Prometheus-Metriken mit PrÃ¤fix `fritzbox_exporter_archive_`. ZÃ¤hler und Statuszeitpunkt beginnen beim Exporter-Start neu; die Ereignisse bleiben gespeichert. Eine beim Start noch laufende SOAP-Diensterkennung kann zunÃ¤chst als Abruffehler erscheinen und wird bei Erfolg geschlossen.

Bei Ausfall der 5690 bleibt das Archiv erreichbar, solange der Container-Host und dessen Netzwerkzugang funktionieren. Die Abfrage der entfernten 4040 benÃ¶tigt weiterhin das VPN. Zwischen zwei erfolgreichen Abfragen verlorene Router-Ereignisse kÃ¶nnen nicht nachtrÃ¤glich gerettet werden.

## Lesende Schnittstelle

`GET /api/events`; nur bei konfiguriertem SchlÃ¼ssel ist der Header `Authorization: Bearer <ARCHIVE_TOKEN>` erforderlich.

Optionale Parameter: `gateway`, `kind` (`router`, `query`, `recovery`), `backend` (`router`, `soap`, `lua`, `api`, `archive`), `q` (Textsuche), `from` und `to` (RFC3339-Zeit), `limit` (1â€“500, Standard 200), `offset` (Standard 0).

Antwort: JSON mit `events` und Archivstatus. Keine SQL-Abfragen, keine schreibenden HTTP-Endpunkte. Die begrenzte, parametrisierte Schnittstelle kann spÃ¤ter als Datenquelle eines neueren Grafana dienen.

## Backup / Umzug

Container stoppen, komplettes `/data`-Volume sichern, auf dem Ziel wieder als `/data` einbinden und gleiche ENV verwenden. Beim Kopieren einer laufenden Datenbank reicht die `.db`-Datei allein im WAL-Modus nicht aus; deshalb den Container fÃ¼r diese einfache Sicherungsmethode stoppen. Das aktuelle Docker-Image wird nur fÃ¼r ARM64 verÃ¶ffentlicht; fÃ¼r ein x86-Synology-System wÃ¤re zusÃ¤tzlich ein AMD64-Build erforderlich.

## Gestaltung der Webseite

`archive.html` enthält HTML, CSS und JavaScript. `archive_http.go` bettet die Datei
über `//go:embed` in die ausführbare Datei ein. Änderungen an Logo oder Gestaltung
erfordern derzeit einen neuen Build. Eine gemountete HTML-Datei ersetzt die
eingebettete Seite nicht; konfigurierbare Themes sind noch nicht implementiert.
Docker-Logs benötigen unabhängig vom Archiv eine eigene Größenbegrenzung/Rotation.
