package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	upnp "github.com/sberk42/fritzbox_exporter/fritzbox_upnp"
)

// Exercise the real collector: a failed middle index must not discard hosts
// before or after it, and configured TTLs must still control cached reads.
func TestHostEnumerationCachePartialFailureAndRecovery(t *testing.T) {
	oldMetrics, oldLua, oldCache := metrics, luaMetrics, upnpCache
	defer func() { metrics, luaMetrics, upnpCache = oldMetrics, oldLua, oldCache }()
	const service = "urn:dslforum-org:service:Hosts:1"
	metrics = []*Metric{{Service: service, Action: "GetGenericHostEntry",
		ActionArgument: &ActionArg{Name: "NewIndex", IsIndex: true, ProviderAction: "GetHostNumberOfEntries", Value: "HostNumberOfEntries"},
		Result:         "Active", CacheEntryTTL: 60, MetricType: prometheus.GaugeValue,
		PromDesc: JSONPromDesc{FqName: "gateway_hosts", VarLabels: []string{"gateway", "HostName"}},
		Desc:     prometheus.NewDesc("gateway_hosts", "Hosts", []string{"gateway", "hostname"}, nil),
	}}
	luaMetrics = nil
	upnpCache = map[string]*upnpCacheEntry{}
	put := func(key string, result upnp.Result) {
		upnpCache[service+"|"+key] = &upnpCacheEntry{Timestamp: time.Now().Unix(), Result: &result}
	}
	put("GetHostNumberOfEntries", upnp.Result{"HostNumberOfEntries": uint64(3)})
	for _, i := range []int{0, 2} {
		put(fmt.Sprintf("GetGenericHostEntry|NewIndex|%d", i), upnp.Result{"Active": uint64(1), "HostName": fmt.Sprint(i)})
	}
	// No service is available: an uncached index fails, while fresh cached entries
	// remain usable. This also detects accidental cache bypasses.
	c := &FritzboxCollector{Root: &upnp.Root{}, Gateway: "test"}
	r := prometheus.NewPedanticRegistry()
	r.MustRegister(c)
	check := func(wantHosts int, wantSuccess float64) {
		t.Helper()
		families, err := r.Gather()
		if err != nil {
			t.Fatal(err)
		}
		hosts := 0
		for _, f := range families {
			if f.GetName() == "gateway_hosts" {
				hosts = len(f.Metric)
			}
		}
		if hosts != wantHosts {
			t.Fatalf("got %d hosts, want %d", hosts, wantHosts)
		}
		if got := diagnosticValue(t, families, "fritzbox_exporter_query_success", "soap"); got != wantSuccess {
			t.Fatalf("success=%v, want %v", got, wantSuccess)
		}
		if got := diagnosticValue(t, families, "fritzbox_exporter_query_results", "soap"); got != float64(wantHosts) {
			t.Fatalf("results=%v", got)
		}
	}
	check(2, 0)
	put("GetGenericHostEntry|NewIndex|1", upnp.Result{"Active": uint64(0), "HostName": "1"})
	check(3, 1)
	upnpCache[service+"|GetGenericHostEntry|NewIndex|1"].Timestamp = time.Now().Add(-2 * time.Minute).Unix()
	check(2, 0)
	put("GetHostNumberOfEntries", upnp.Result{"HostNumberOfEntries": uint64(0)})
	check(0, 1)
}
