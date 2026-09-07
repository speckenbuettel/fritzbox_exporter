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
