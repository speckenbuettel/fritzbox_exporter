# Router configuration examples

These six JSON files are derived from the supplied September 2026 configuration
backups. They contain extraction rules, not router response snapshots; no router
addresses, passwords, session IDs, MAC addresses or private device names are
required in the files. Set credentials and gateway URLs through environment
variables. Private fields named in `varLabels` are extracted at runtime: the
resulting `/metrics` output may still contain private network information.

| Model | Firmware reference | SOAP definitions | Lua definitions | API definitions |
| --- | --- | ---: | ---: | ---: |
| 5690 (not Pro), direct GPON | 8.40; initially validated on 8.40-136122 BETA | 40 | 7 | 26 |
| 4040, Ethernet WAN | 8.03 | 34 | 15 | 0 |

The exact current 5690 build was not independently rechecked for this update.
Copy all three files from one model into your mounted configuration directory:

```text
METRICS_FILE=/config/metrics.json
LUA_METRICS_FILE=/config/metrics-lua.json
API_METRICS_FILE=/config/metrics-api.json
```

For the 4040, API_METRICS_FILE can also be omitted. Do not combine the profiles.
They preserve the installation's metric names and labels; the inherited Grafana
dashboard must be adapted. See the main [README](../../README.md) for deployment.

## 5690

SOAP supplies device, PPP connection, traffic, fibre counters, WLAN state, hosts,
DECT and Powerline inventory. API supplies CPU, RAM, energy, Ethernet port state,
VPN, storage, access type, IPv6 state and Powerline PHY rates. Lua supplies fibre
optical diagnostics, physical connection state and the latest downstream chart
sample. The Lua file is no longer empty.

Both optical power directions come from `fiberFiber` via Lua in dBm. The fibre
chart value from `inetOv` uses `values` with index `-1`, already in bytes/second;
multiply by 8 for bits/second and do not apply `rate()`. It is the latest short
chart sample, not an average over the entire scrape interval.

WAN and Fibre traffic counters can describe overlapping Internet traffic; do not
sum them. Use Fibre for this profile's Internet traffic. Physical GPON rates are
not the subscribed tariff or measured throughput. `MinutesInShowtime` describes
physical synchronisation and need not reset on an IP/PPP reconnect.

Powerline API `isLocal=0` selects remote adapters; this is an API flag, not a
hostname suffix. PHY rates describe link capacity, not traffic. The adapter's
`model` label can collide with a Prometheus target label of the same name and
become `exported_model`. IPsec and WireGuard are selected by `access_type` 3/4;
API address labels use `remote_ip`. Disconnected peers may have empty addresses.
WLAN packet counters are omitted because the tested firmware returned zero.

## 4040

SOAP supplies device/WAN/LAN/WLAN counters and host inventory; Lua supplies CPU,
RAM, energy, LAN state, USB and VPN data. Use WAN for Internet traffic. The API
configuration is intentionally empty. Lua IPsec uses `remoteIP`, WireGuard uses
`remoteIp`; response field names are case-sensitive. USB definitions currently
select the first partition of each device. Identically named USB devices may
need additional identifying labels.

Intermittent host index 713 and duplicate-host diagnostics are still under
investigation. v1.1.2 preserves partial host results and reports count changes;
v1.1.3-test additionally records duplicate indexes and cache provenance. Neither
is claimed to fix the underlying cause. Remote connectivity and collection
budgets must also be considered; do not suppress errors merely to hide them.

## Localisation and maintenance

The label-renaming rules target German WebGUI responses. Review them for other
languages. API/Lua schemas are not stable contracts across firmware versions.
Optional empty collections use `allowEmpty` where configured; missing fields are
not equivalent to valid empty lists. Review definitions after firmware updates.
The generic root defaults are retained for compatibility, but the supported
example profiles in this directory are the recommended starting point.
