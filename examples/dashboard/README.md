# Dashboard configurations

These configurations were checked read-only against a FRITZ!Box 5690 with
8.40-136122 BETA and a FRITZ!Box 4040 with 8.03. Use the files for the matching
model/firmware, not a universal combined configuration. The router configurations
are PPPoE over direct fibre (5690) and Ethernet WAN (4040).

The files preserve the existing `gateway_*` metric names and dashboard labels.
Source priority is SOAP, then API, then legacy Lua. The 5690 has an empty Lua
configuration; the 4040 has an empty API configuration. Keep the three files
separate per container. API transforms require api-v0-test3 or newer.

5690 changes: WAN IP/MAC/error/status/uptime through WANPPPConnection instead of
the unavailable WANIPConnection action; WAN interface labels replace Cable;
CPU, RAM, energy, physical LAN port status, VPN, DECT base and storage via API.
The first CPU/RAM series entry is the latest sample, confirmed against the
reversed Lua series and WebGUI client code. RAM remains percentages, not bytes.
Energy CPU percentage is energy consumption, not CPU utilization.

VPN gateway_vpn_bridge includes LAN-LAN IPsec (access_type=3).
gateway_vpn_wireguard includes WireGuard (access_type=4). The filter is required
because the API returns both protocols in one collection. The API-prefixed VPN
metrics from earlier test images remain available for compatibility.

4040 changes: retain proven Lua paths, allow empty VPN/USB lists, add optional
USB metrics, declare WLAN packet counters as CounterValue. The 4040 has no
internal storage or DECT hardware. No synthetic zero hardware readings are added.
Its legacy USB definitions retain the existing first-partition extraction; the
empty case is live tested, attached-device results need a hardware test.

The 5690 exports its internal storage plus all external partitions, using the
storage UID and partition label to distinguish devices. The empty USB case was
checked live; nonempty and multi-device cases are covered by synthetic tests.
Actual attached USB devices still need a hardware test.

## Limits

The 5690 firmware returns permanent zero WLAN packet counters. These definitions
are omitted to avoid presenting false zero traffic. The inspected API and WLAN
device list expose link speeds, not equivalent traffic counters. The existing
WLAN Traffic panel therefore remains unavailable for that box.

Access type remains the actual SOAP value `Other` on the 5690; this is the
standardized service's response for its fibre connection, not a Cable label.
The DECT count is additionally available as `gateway_dect_count`; use that in
the DECT Phones panel to show an explicit zero when no phones are registered.
DOCSIS metrics are outside the scope of these two non-cable boxes.

Prometheus target labels (`device`, `model`, etc.) must continue to be added by
the existing scrape jobs. The exporter itself only supplies its gateway label.

Keep the existing Portainer entrypoint `/app/fritzbox_exporter` and empty CMD.
Set METRICS_FILE, LUA_METRICS_FILE, API_METRICS_FILE to the three files under the
appropriate mounted subdirectory, e.g. `/config/5690/metrics-api.json`.
For the 4040 API_METRICS_FILE may be omitted or point to its empty API file.
No production container is changed by installing these examples in the repository.
