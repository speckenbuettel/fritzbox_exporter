# Query diagnostics

SOAP, Lua and API collectors automatically expose diagnostics for every enabled
metric definition. No new environment variable is required. Existing aggregate
error counters and log messages remain available.

| Metric | Meaning |
| --- | --- |
| `fritzbox_exporter_query_success` | Last evaluation succeeded (1) or failed (0). |
| `fritzbox_exporter_query_results` | Number of samples emitted by that definition. |
| `fritzbox_exporter_query_errors_total` | Failed evaluations since exporter start, at most once per definition per scrape. |
| `fritzbox_exporter_query_last_success_timestamp_seconds` | Last successful evaluation; 0 until first success. |
| `fritzbox_exporter_query_error` | Value 1 for a current failure, with a `reason` label; disappears after recovery. |

All five metrics have `collector` (`soap`, `lua`, `api`), `query` (one-based
position in that collector's configuration), `source` (SOAP service/action,
Lua path/page, or API path) and `metric` (configured Prometheus name) labels.
Reordering definitions changes query identities. Prometheus adds its normal
target labels such as `instance`, `job` and `device`.

Failure reasons are `discovery` (SOAP services unavailable; this also blocks
legacy Lua collection), `request` (request/login/HTTP failure, including invalid
API JSON), `json` (invalid Lua JSON), `extract` (unavailable/unconvertible value),
`duplicate` (duplicate output labels) and `metric` (metric construction failure).
Only the first failure category in an evaluation is emitted. Raw error messages,
session IDs and arbitrary query parameters are not included in diagnostic labels.

These are evaluation diagnostics, not a count of network requests: definitions
can share a request or use cached data. Successful cached evaluations update the
last-success timestamp too. An indexed SOAP definition can emit some valid
samples while failing overall. A stopped or unstartable exporter cannot report
its own diagnostics; monitor Prometheus `up` as well.

## Optional devices and empty results

Add `"allowEmpty": true` to an individual Lua/API metric definition whose
wildcard collection may legitimately be empty, for example USB partitions:

```json
{
  "path": "storage",
  "resultPath": "externalStorages.*.partitions.*",
  "resultKey": "capacity",
  "allowEmpty": true,
  "promType": "GaugeValue",
  "promDesc": {
    "fqName": "fritzbox_api_usb_capacity_bytes",
    "help": "USB partition capacity in bytes.",
    "varLabels": ["gateway", "label"]
  }
}
```

This is an illustrative schema; verify the target firmware response first.
`{"externalStorages":[]}` produces success=1 and results=0. A missing
`externalStorages` key, null, a wrong type or a nonempty malformed partition
remains an error. Thus a removed Lua field is not hidden as an absent USB device.
All rows in an allowEmpty definition are validated. Existing definitions without
allowEmpty retain their extraction behavior and treat entirely empty results as
errors. SOAP indexed queries already accept a returned item count of zero.

## Grafana / Prometheus

Current failing definitions (instant query, table panel):

```promql
fritzbox_exporter_query_error == 1
```

Number of failing definitions per router:

```promql
sum by (job, instance) (fritzbox_exporter_query_success == bool 0)
```

Failures over the last hour:

```promql
increase(fritzbox_exporter_query_errors_total[1h])
```

Valid empty results:

```promql
(fritzbox_exporter_query_results == 0)
and (fritzbox_exporter_query_success == 1)
```

Exporter unavailable:

```promql
up{job=~"fritzbox-.*"} == 0
```

Use the diagnostic table to audit SOAP/Lua/API definitions after firmware
updates. A successful extraction still requires a semantic check against the
router UI (units, interface, expected values); diagnostics alone cannot prove
that a metric describes the intended quantity.

## Collection counter names

Collection error counters use snake_case and the _total suffix:
- fritzbox_exporter_collect_errors_total (SOAP)
- fritzbox_exporter_lua_collect_errors_total
- fritzbox_exporter_api_collect_errors_total

The former names fritzbox_exporter_collectErrors and fritzbox_exporter_luaCollectErrors were removed. Update dashboard queries and alert rules when upgrading. Counter behavior is unchanged.


## Bounded collections (api-v0-test5)

SOAP_TIMEOUT and LUA_TIMEOUT default to 10s per HTTP request, including response bodies. API_TIMEOUT remains unchanged. COLLECTION_TIMEOUT defaults to 25s. SOAP and Lua share one total budget; the concurrently gathered API collector has the same budget. Values must be positive.

For the remote 4040 with a Prometheus scrape timeout of 55s, set COLLECTION_TIMEOUT=45s, SOAP_TIMEOUT=10s and LUA_TIMEOUT=10s. Keep the total budget below the Prometheus scrape timeout. For the 5690's 30s scrape timeout, the default 25s budget applies. A budget that is too short produces explicit failures rather than silently queueing more work; adjust the scrape timeout and collection budget together if required.

When the budget expires, active HTTP requests are cancelled and remaining definitions are marked with query_error reason="timeout". Individual request failures continue to use reason="request". Existing successful samples from earlier in a partial collection may still be returned; inspect query_success. Collection timeouts are counted by fritzbox_exporter_collection_timeouts_total{backend="soap_lua"|"api"}. A simultaneous /metrics request gets HTTP 503 immediately and increments fritzbox_exporter_scrapes_rejected_total. There is no queue. Prometheus marks a rejected scrape as up=0.

A disconnected Prometheus client does not directly cancel the collector: its independent finite budget remains in force. This applies to metrics collection; service discovery at startup is separate. Counters may be observed on the following scrape because registry collectors are gathered concurrently.
