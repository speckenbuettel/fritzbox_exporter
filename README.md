# FRITZ!Box exporter for Prometheus

Monitor a FRITZ!Box using SOAP/TR-064, WebGUI Lua endpoints and the newer
FRITZ!OS API v0. This fork of [sberk42/fritzbox_exporter](https://github.com/sberk42/fritzbox_exporter)
adds API collection, persistent router events and detailed collection diagnostics.

## Improvements over upstream

| Feature | What this fork adds |
| --- | --- |
| **API metrics for newer FRITZ!OS** | Optional authenticated `/api/v0/` JSON collector for values that some newer WebGUI versions no longer return through the previous Lua paths; filtering, label mapping and sample selection. |
| **Persistent event archive** | Router events, query failures and recoveries in bounded SQLite storage, with a browser viewer at `/events` and a read-only JSON API. |
| **Errors exposed as metrics** | Per-definition success, error counters, error category, result count and last success across SOAP, Lua and API. |
| **Bounded collection** | Request timeouts, total collection budgets and rejection of overlapping scrapes. |
| **Session recovery** | API recovery after expired sessions or router restarts; synchronized SOAP authentication for concurrent archive and metric requests. |
| **Packaging** | Versioned ARM64 images built from this fork, with race tests, static checks and container/SQLite smoke tests. |

SOAP, Lua collection and the original Grafana dashboard are inherited features.
See [Changes from upstream](CHANGES_FROM_UPSTREAM.md) for scope and migration notes.

## Example configurations and firmware

The examples are derived from two running installations, reviewed for private
addresses, device identifiers and credentials. Credentials are supplied separately.
They are firmware-specific examples, not a guarantee of support on every router.

| Model | Firmware reference | Sources | Files |
| --- | --- | --- | --- |
| FRITZ!Box **5690** (not Pro), direct GPON fibre | FRITZ!OS **8.40**; initial validation on **8.40-136122 BETA** | SOAP + API + Lua | [5690 profile](examples/dashboard/5690) |
| FRITZ!Box **4040**, Ethernet WAN to a modem | FRITZ!OS **8.03** | SOAP + Lua; empty API file | [4040 profile](examples/dashboard/4040) |

The profiles reflect the supplied configuration backups from September 2026.
The exact current 8.40 build was not re-read as part of this documentation update.
Older model examples and discovery snapshots have been removed; Git history
retains them. Root-level metric files remain generic defaults for compatibility.
Use all three files from the chosen profile together, following the
[profile notes and known limitations](examples/dashboard/README.md).

The API is needed because some newer firmware exposes CPU, RAM, energy, VPN and
other WebGUI data through API v0 instead of the former `data.lua` response fields.
This is **not a universal firmware cutoff**: the 4040 still supplies these through
Lua, and the 5690 still uses Lua for optical diagnostics and the downstream chart.
There is no automatic fallback between sources. See [API.md](API.md).

## Run with Docker or Portainer

Stable image: **`senecaiii/fritzbox_exporter:v1.1.2`** — **Linux ARM64**.
The optional **`v1.1.3-test`** image adds host-duplicate diagnostics for the ongoing
4040 investigation. It is not a fix for that issue. `latest` is not updated by
this fork's release workflow. Other architectures require a separate build.

Create a dedicated FRITZ!Box user with permission for the selected data and enable
the applicable TR-064/UPnP access. Copy the chosen profile's three files into
`./config`. Set `FRITZBOX_USERNAME` and `FRITZBOX_PASSWORD` outside version control.

```yaml
services:
  fritzbox-exporter:
    image: senecaiii/fritzbox_exporter:v1.1.2
    restart: unless-stopped
    stop_grace_period: 70s
    ports:
      - "9042:9042"
    environment:
      USERNAME: "${FRITZBOX_USERNAME}"
      PASSWORD: "${FRITZBOX_PASSWORD}"
      GATEWAY_URL: "http://fritz.box:49000"
      GATEWAY_LUAURL: "http://fritz.box"
      METRICS_FILE: /config/metrics.json
      LUA_METRICS_FILE: /config/metrics-lua.json
      API_METRICS_FILE: /config/metrics-api.json
      ARCHIVE_FILE: /data/events.db
      ARCHIVE_TIMEZONE: Europe/Berlin
      ARCHIVE_RETENTION: 2160h
      ARCHIVE_MAX_ROWS: "50000"
      ARCHIVE_MAX_MB: "64"
    volumes:
      - ./config:/config:ro
      - archive-data:/data
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"
volumes:
  archive-data:
```

Replace `fritz.box` with a router address resolvable from inside the container.
Set the timezone to that used in the router's event timestamps. For multiple
routers use separate containers, configuration directories, published ports and
archive volumes. In Portainer set the same environment variables and mounts.
The container listens on `0.0.0.0:9042`; `LISTEN_ADDRESS` is not the Docker host IP.
The standalone binary defaults to `127.0.0.1:9042`.

Configure Prometheus to scrape `/metrics`, for example every 60 seconds. Keep
`scrape_timeout` above `COLLECTION_TIMEOUT`, but within `scrape_interval`:

```yaml
scrape_configs:
  - job_name: fritzbox
    scrape_interval: 60s
    scrape_timeout: 55s
    static_configs:
      - targets: ["exporter-host:9042"]
        labels:
          device: "router-example"
```

The `device` label is added by Prometheus, not the exporter. Dashboards must use
the same labels. Never commit actual credentials, router responses or event
archives to a public repository.

## Collector settings

| Environment variable | Default / purpose |
| --- | --- |
| `USERNAME`, `PASSWORD` | Router credentials; set explicitly. |
| `GATEWAY_URL` | `http://fritz.box:49000`, SOAP origin. |
| `GATEWAY_LUAURL` | `http://fritz.box`, WebGUI origin. |
| `GATEWAY_APIURL` | Uses the WebGUI origin unless overridden. |
| `METRICS_FILE` | `metrics.json`, SOAP definitions. |
| `LUA_METRICS_FILE` | `metrics-lua.json`, Lua definitions. |
| `API_METRICS_FILE` | Unset: API disabled. The root API example is empty. |
| `SESSIONAPI` | `v2`, WebGUI login protocol, unrelated to API v0. |
| `VERIFYTLS` | `false`; set `true` to verify TLS certificates. |
| `SOAP_TIMEOUT`, `LUA_TIMEOUT`, `API_TIMEOUT` | `10s` per HTTP request. |
| `COLLECTION_TIMEOUT` | `25s`; SOAP/Lua share a budget, API has a separate concurrent budget. |

Use `-h` for all flags. `-nolua` disables Lua, not API collection. A remote router
may require a larger collection budget, with corresponding Prometheus timeout
headroom. A timeout is not proof that a particular metric is unsupported.

## Event archive and `/events`

Enable `ARCHIVE_FILE` and mount persistent local storage. The archive polls SOAP
`DeviceInfo/GetDeviceLog` independently of Prometheus; query errors and recoveries
are recorded when metrics are collected. `/events` provides filters, pagination,
occurrence counts and storage/poll status. `/api/events` provides read-only JSON.
It requires no Grafana plugin and does not expose SQLite directly.

| Environment variable | Default | Meaning |
| --- | --- | --- |
| `ARCHIVE_FILE` | Empty | Disabled unless a database path is provided, e.g. `/data/events.db`. |
| `ARCHIVE_TOKEN` | Empty | Optional read-access token; at least 16 characters if set. |
| `ARCHIVE_TIMEZONE` | `Europe/Berlin` | Timezone of the router's event timestamps. |
| `ARCHIVE_INTERVAL` | `30s` | Router event polling interval. |
| `ARCHIVE_RETENTION` | `2160h` | 90 days since last observation; Go duration syntax, e.g. `720h` for 30 days. |
| `ARCHIVE_MAX_ROWS` | `50000` | Retain the newest rows by last observation; minimum 100. |
| `ARCHIVE_MAX_MB` | `64` | Main SQLite database limit in MiB; minimum 8. Journal overhead is additional. |

Old rows are removed during writes. Repeated identical query failures are grouped;
changed messages create new rows. Router messages whose embedded occurrence count
changes are also separate records. SQLite reuses freed pages, so deletion need
not shrink the database file. Hitting the size limit can cause write failures;
it does not guarantee automatic eviction before every insert. WAL checkpoints
limit journal growth, but the main-file limit is not a directory quota.

Without `ARCHIVE_TOKEN`, anyone who can reach the exporter can read the archive.
Use trusted network access or an authenticated HTTPS reverse proxy; router event
text can contain private network details. Archive retention is independent of
Docker log rotation (configured separately in the Compose example above).

Use a separate local database per exporter; do not share a file between instances
or place it on SMB/NFS. Stop the container and back up the entire volume for a
simple consistent backup. Events lost from the router before polling cannot be
reconstructed. See [ARCHIVE.md](ARCHIVE.md) for details and API parameters.

## Error diagnostics in Prometheus and Grafana

| Metric | Meaning |
| --- | --- |
| `fritzbox_exporter_query_success` | Last evaluation: 1 successful, 0 failed; cached/valid empty results may succeed. |
| `fritzbox_exporter_query_error` | Current failure with `reason`; emitted as 1 only for failed definitions, absent after recovery. |
| `fritzbox_exporter_query_errors_total` | Failed evaluations; at most one increment per definition per collection. |
| `fritzbox_exporter_query_results` | Samples emitted by a definition in the last evaluation. |
| `fritzbox_exporter_query_last_success_timestamp_seconds` | Last successful evaluation, including cached results. |
| `fritzbox_exporter_collection_timeouts_total` | Exhausted collection budgets, by backend. |
| `fritzbox_exporter_scrapes_rejected_total` | Overlapping `/metrics` requests rejected with HTTP 503. |

Aggregate counters are `fritzbox_exporter_collect_errors_total` (SOAP),
`fritzbox_exporter_lua_collect_errors_total` and
`fritzbox_exporter_api_collect_errors_total`. They need not match the per-definition
counts. Prometheus `up=1` only confirms a successful scrape, not successful router
queries or an active Internet connection.

Diagnostics identify `collector` (`soap`, `lua`, `api`), `query` (configuration
position), `source` and `metric`. If a target also supplies `collector`, Prometheus
normally renames the exporter's label to `exported_collector`. Only the first
failure reason per evaluation is retained; detailed errors are in logs/archive,
not high-cardinality metric labels.

Current failed definitions:

```promql
sum(1 - fritzbox_exporter_query_success{device="$device"})
```

Failure table for the previous hour (use Grafana's Instant query mode):

```promql
increase(fritzbox_exporter_query_errors_total{device="$device"}[1h]) > 0
```

`increase()` extrapolates to the window boundaries, so fractional counts are
normal. Current-error tables return no data when there are no errors. See
[DIAGNOSTICS.md](DIAGNOSTICS.md) for all meanings and example queries.

The inherited [Grafana dashboard](grafana/README.md) is a starting point and needs
adjustment for these profiles. Set Grafana's Prometheus scrape interval to the
actual scrape interval; use `$__rate_interval` for counter rates. A value already
expressed as a rate is a gauge and should not be passed to `rate()` again.

## Customize the event viewer

[`archive.html`](archive.html) contains the HTML, CSS and JavaScript for `/events`.
[`archive_http.go`](archive_http.go) embeds it with Go's `//go:embed` at build time.
To change colours, typography or add a logo, edit that file and rebuild the binary
or image. Mounting a replacement HTML file into the existing container does not
change the embedded page. There is currently no theme/logo environment variable
or configurable template directory. A self-contained inline SVG logo can be
included in the HTML without adding a new asset endpoint.

For a shared visual identity across projects, a future theme could expose common
colour tokens, a logo, product title and footer while retaining bundled defaults.
This is a design direction, not an implemented configuration feature.

## Build and license

```sh
git clone https://github.com/speckenbuettel/fritzbox_exporter.git
cd fritzbox_exporter
go build -o fritzbox_exporter .
go test ./...
go vet ./...
docker build -t fritzbox_exporter:local .
```

Docker builds the checked-out source with Go 1.25.5, including SQLite without CGO.
The Go module path remains `github.com/sberk42/fritzbox_exporter`; installing that
upstream module with `@latest` does not install this fork.

This fork remains under the [Apache License 2.0](LICENSE), inherited from upstream.
Original copyright and license notices are retained. This is a modified fork;
[CHANGES_FROM_UPSTREAM.md](CHANGES_FROM_UPSTREAM.md) records its main additions.
Apache 2.0 already permits commercial use, modification and redistribution and
includes an express patent grant. Replacing LICENSE with MIT alone would not
remove the obligations for inherited Apache-licensed code. See
[Apache 2.0, section 4](https://www.apache.org/licenses/LICENSE-2.0.html#redistribution).
