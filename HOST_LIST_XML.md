# Experimental XML host collector

Enable only for testing with `HOST_LIST_XML=true` (or `-host-list-xml`). Default is false. Disable bounded index diagnostics (`HOST_DIAGNOSTIC_DURATION=0`) for this experiment; indexed tracing does not run for XML queries.

The existing indexed Hosts/GetGenericHostEntry metric uses X_AVM-DE_GetHostListPath and downloads its XML list instead. No automatic fallback or background polling is added. Metric names, values and remaining labels are unchanged. The metric's cacheEntryTTL controls the whole list cache; reads happen on Prometheus scrapes. The separate GetHostNumberOfEntries metric remains a SOAP request and need not agree atomically with a separately fetched list.

## Configuration change

In metrics.json, remove `AddressSource` from gateway_hosts/promDesc/varLabels: the tested 4040 XML list does not supply it. Do not invent a Static or DHCP value. Keep the service, action, actionArgument, result and TTL unchanged. Other configured fields absent from XML (for example LeaseTimeRemaining) must likewise not be requested; the collector rejects the query if a row lacks a configured field. metrics-api.json and metrics-lua.json do not change.

Removing a label creates new Prometheus series; historical series still have addresssource. Queries filtering/grouping on that label need adjustment. Back up the original configuration to restore SOAP mode later.

Path tokens are not logged. List downloads enforce the router origin, reject redirects, limit responses to 4 MiB and share the collection deadline and SOAP HTTP timeout. Successful lists alone are cached; failed refreshes are not emitted as stale success. Exact duplicate rows are skipped, while conflicting metric labels remain errors. This feature is experimental and is not a default switch for other routers.
