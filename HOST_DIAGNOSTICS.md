# Bounded host diagnostics

Opt in with `HOST_DIAGNOSTIC_DURATION=48h` or `-host-diagnostic-duration=48h`.
Default: `0` (off). Maximum: `72h` from process start. Restarting starts a new window and a new pseudonym key.

Fresh host enumerations are sampled at most once per minute. The diagnostic reads the host count and `X_AVM-DE_GetChangeCounter` before and after enumeration. Unsupported actions are reported as such. After error 713 it retries that index once without touching the production cache. Existing failure handling remains in effect even if this retry succeeds.

Each additional action has a one-second timeout and uses the existing collection deadline. Calls are skipped with less than two seconds remaining. Up to five additional actions per sampled enumeration can add up to five seconds; this opt-in instrumentation can itself affect timing. Timeout or skipped probes do not become query failures.

One diagnostic log line is emitted for a sampled anomaly, otherwise at most once per five minutes. Rows are capped at 64 per line. Row format: index:pseudonym:Active:InterfaceType:source. Unknown interface values are replaced by `other`. HMAC-SHA256 pseudonyms use a random in-memory key and MAC/IP/name inputs; no raw identifiers or diagnostic error bodies are logged. Pseudonyms can change when identifying fields change and are not unique physical-device IDs. Existing ordinary exporter logs remain unchanged.

Compare count_before/count_after, change_before/change_after and their status fields. A changing counter supports concurrent host changes but does not identify the cause; an unchanged counter does not prove a stable list. retry_status distinguishes ok, invalid_index_713, unsupported, request_failed and budget skips. Compare row pseudonyms across emitted summaries and anomalies, and correlate UTC timestamps with router WLAN events. This does not automatically correlate the event archive or prove band steering caused an error.
