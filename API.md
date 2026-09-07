# FRITZ!OS API v0 collector

The API collector is optional and independent of `metrics.json` (SOAP) and
`metrics-lua.json` (legacy WebGUI). Enable it explicitly:

```sh
./fritzbox_exporter -api-metrics-file /config/metrics-api.json
```

In Docker/Portainer the equivalent environment variable is
`API_METRICS_FILE=/config/metrics-api.json`. Existing `USERNAME`, `PASSWORD`,
`GATEWAY_URL`, `GATEWAY_LUAURL`, and `LISTEN_ADDRESS` settings remain supported.
`GATEWAY_APIURL` optionally overrides the WebGUI origin, including a port.
`API_TIMEOUT=10s` bounds each HTTP request, including login; a scrape can contain
multiple requests. `SESSIONAPI=v2` selects the existing PBKDF2 login mechanism.
`VERIFYTLS=true` enables certificate verification. The API collector has its own
session and respects these timeout/TLS settings during both login and API reads.
`-nolua` disables only legacy Lua; it does not disable the API collector.
Without `-api-metrics-file`, no API requests or API logins are made.

The shipped `metrics-api.json` is intentionally empty: endpoint schemas must be
checked on the target model and firmware before enabling definitions. This
illustrative definition assumes a response of
`{"connection":[{"name":"Singapore","state":"ready"}]}` from
`/api/v0/generic/vpn`. It is not yet a verified production VPN configuration:

```json
{
  "labelRenames": [],
  "metrics": [
    {
      "path": "generic/vpn",
      "params": "",
      "resultPath": "connection.*",
      "resultKey": "state",
      "okValue": "ready",
      "promType": "GaugeValue",
      "cacheEntryTTL": 30,
      "promDesc": {
        "fqName": "fritzbox_api_vpn_up",
        "help": "Whether the VPN connection is ready.",
        "varLabels": ["gateway", "name"],
        "fixedLabels": {}
      }
    }
  ]
}
```

Configuration uses the same extraction fields as Lua: dot-separated paths,
`*` for object/array members, numeric array indexes, numeric strings, `okValue`
for a 1/0 comparison, labels and label renaming. Use `GaugeValue`, `CounterValue`
or `UntypedValue`. The response root must be a JSON object. Missing values are
reported as collection errors and omitted, never converted into a false zero.
Use distinct metric names when SOAP/Lua still export equivalent metrics; different
collectors must not emit the same name/label combination.

`path` is relative to `/api/v0/`, without a leading slash or method prefix.
`params` is an optional URL-encoded query string. Only GET is supported.
Authentication uses `Authorization: AVM-SID <sid>` and `Client-Name: WebGUI`.
401/403 clears the session and triggers at most one login/retry per endpoint.
Other HTTP errors, invalid JSON and unavailable fields do not trigger a login.
Redirects are rejected. API responses are limited to 8 MiB. Sessions, passwords
and response bodies are not logged by the API collector.

A successful response is cached for at least 30 seconds. Definitions sharing an
endpoint/query use the shortest configured TTL and a single request per scrape.
Failures are memoized only within that scrape. Concurrent API scrapes are
serialized to protect session and cache. The counter
`fritzbox_exporter_api_collect_errors_total` records request, extraction and
metric emission failures.

## Build and verification

The Dockerfile now builds the checked-out source, including fork changes. It
keeps upstream Go 1.25.5 because the current dependencies require a recent Go
release. The runtime entrypoint executes the binary directly; environment
variables are handled by the existing flag library. Explicit Portainer command
and entrypoint overrides should be reviewed when switching images.

```sh
go test ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o fritzbox_exporter .
docker build -t YOUR_DOCKERHUB_NAME/fritzbox_exporter:api-v0-1 .
```

Tests use mock HTTP/TLS servers for header authentication, v2 challenge login,
session renewal, JSON extraction, caching, concurrent scrapes, invalid paths,
HTTP/JSON errors, redirects and timeouts. Live verification against the 5690,
final endpoint definitions and a Docker runtime test remain necessary before
replacing a production container. The 4040 can continue using SOAP and Lua.

## Query health

Per-definition health metrics and optional empty collections for Lua/API are
documented in [DIAGNOSTICS.md](DIAGNOSTICS.md).
