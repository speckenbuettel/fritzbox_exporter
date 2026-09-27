# Changes from sberk42/fritzbox_exporter

Scope: this fork through **v1.1.2**, plus the diagnostic **v1.1.3-test**, compared with locally tracked upstream commit
`50ebd8ff4588208b46fe7caed312903e18f96d3b`. Later upstream changes are outside this comparison.

## Inherited features

SOAP/TR-064 collection, configurable metrics, environment credentials, HTTPS,
Lua WebGUI collection, discovery/test modes, host/DECT metrics and the original
Grafana dashboard already existed in sberk42's fork.

## API v0 for newer FRITZ!OS

The tested 5690 with FRITZ!OS 8.40-136122 BETA exposes several values through
`/api/v0/` rather than the previous `data.lua` responses. The optional API collector
adds WebGUI session authentication and GET-only JSON collection, configured
separately through `API_METRICS_FILE`.

The 5690 profile includes API definitions for CPU, RAM, energy, LAN ports, VPN,
DECT and storage. Extraction supports filters, nested collections, parent labels
and numeric sample selection. See [API.md](API.md).

This supplements SOAP and Lua. The tested 4040 with FRITZ!OS 8.03 still uses
SOAP/Lua. There is no automatic fallback or universal FRITZ!OS version cutoff;
endpoint schemas and semantics require verification on the target router.

## Further additions

| Area | Change |
| --- | --- |
| Diagnostics | Per-definition success, result count, error category/count and last success for SOAP, Lua and API |
| Empty collections | Explicit `allowEmpty` for valid empty Lua/API lists; malformed or missing fields remain errors |
| Timeouts | Per-request limits and total backend budgets; overlapping scrapes are rejected rather than queued |
| Hosts | Cached partial enumeration as in upstream; count-change logging in v1.1.2 and duplicate/cache provenance in v1.1.3-test. The intermittent 4040 issue remains open. |
| Lua errors | Non-JSON response diagnostics without dumping the response body |
| Powerline | Optional per-link SNR spectrum extraction with validated scaling; explicit definitions required, potentially many series |
| Archive | SQLite router events and query errors, independent polling, bounded storage, browser viewer and read-only API |
| Authentication | Session recovery and synchronized SOAP authentication state for archive/metric concurrency |
| Packaging | Builds the checked-out fork, ARM64 image tests, semantic release tags and fork OCI metadata |

The [profiles](examples/dashboard/README.md) now derive from anonymised September
2026 backups of the 5690 and 4040 installations, including fibre and Powerline
definitions. Legacy model-specific examples and discovery snapshots were removed.
Spectrum collection remains optional because of its series count and storage cost.

## v1.1.0 recovery

API sessions recover after router restarts, including HTTP 400 responses for
expired sessions. For HTTP 400 the SID is checked before reauthentication;
genuine request errors remain visible. Authentication recovery permits one retry
within the collection budget. Transport failures invalidate the API session and
cache so subsequent requests can log in again. Login handling clears stale state.

A provider reconnect needs no new login while the local session remains valid.
Router/VPN outages can still cause failed scrapes while the router is unreachable.

## Migration

Use `senecaiii/fritzbox_exporter:v1.1.2` for ARM64. Previous experimental tags used
`api-v0-testN`; the release workflow does not update `latest`. Set the metric file
paths explicitly; the root API file is empty. `SESSIONAPI=v2` selects the login
protocol, not the `/api/v0/` version.

Docker listens internally on `0.0.0.0:9042`; select host ports through port mapping.
Archive tokens are optional, and each instance needs its own database/volume.
Review collection budgets with scrape timeouts; SOAP/Lua share one budget and API
runs concurrently. See [DIAGNOSTICS.md](DIAGNOSTICS.md) and [ARCHIVE.md](ARCHIVE.md).

Update dashboard queries and alert rules for renamed counters:

| Former name | Current name |
| --- | --- |
| `fritzbox_exporter_collectErrors` | `fritzbox_exporter_collect_errors_total` |
| `fritzbox_exporter_luaCollectErrors` | `fritzbox_exporter_lua_collect_errors_total` |

The API counter is `fritzbox_exporter_api_collect_errors_total`. Old names are
not emitted as aliases; historical series retain their original names.

Tests cover extraction, authentication/recovery, timeouts, diagnostics and SQLite.
Release CI includes ARM64 container/archive smoke tests. Model profile validation
is limited to the documented firmware; other hardware needs separate verification.
