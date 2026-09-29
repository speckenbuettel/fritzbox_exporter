package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	// An identical row is skipped without failing the query.
	put("GetGenericHostEntry|NewIndex|2", upnp.Result{"Active": uint64(0), "HostName": "1"})
	check(2, 1)
	// A different value under the same labels is still a conflict.
	put("GetGenericHostEntry|NewIndex|2", upnp.Result{"Active": uint64(1), "HostName": "1"})
	check(2, 0)
	put("GetGenericHostEntry|NewIndex|2", upnp.Result{"Active": uint64(1), "HostName": "2"})
	upnpCache[service+"|GetGenericHostEntry|NewIndex|1"].Timestamp = time.Now().Add(-2 * time.Minute).Unix()
	check(2, 0)
	put("GetHostNumberOfEntries", upnp.Result{"HostNumberOfEntries": uint64(0)})
	check(0, 1)
}

func TestHostInvalidIndexRefreshesNextScrape(t *testing.T) {
	oldM, oldL, oldC := metrics, luaMetrics, upnpCache
	defer func() { metrics, luaMetrics, upnpCache = oldM, oldL, oldC }()
	const service = "urn:dslforum-org:service:Hosts:1"
	counts, hosts := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/igddesc.xml", "/tr64desc.xml":
			fmt.Fprintf(w, `<root><device><serviceList><service><serviceType>%s</serviceType><controlURL>/hosts</controlURL><SCPDURL>/scpd</SCPDURL></service></serviceList></device></root>`, service)
		case "/scpd":
			io.WriteString(w, `<scpd><actionList><action><name>GetHostNumberOfEntries</name><argumentList><argument><name>NewHostNumberOfEntries</name><direction>out</direction><relatedStateVariable>HostNumberOfEntries</relatedStateVariable></argument></argumentList></action><action><name>GetGenericHostEntry</name><argumentList><argument><name>NewActive</name><direction>out</direction><relatedStateVariable>Active</relatedStateVariable></argument><argument><name>NewHostName</name><direction>out</direction><relatedStateVariable>HostName</relatedStateVariable></argument></argumentList></action></actionList><serviceStateTable><stateVariable><name>HostNumberOfEntries</name><dataType>ui4</dataType></stateVariable><stateVariable><name>Active</name><dataType>ui4</dataType></stateVariable><stateVariable><name>HostName</name><dataType>string</dataType></stateVariable></serviceStateTable></scpd>`)
		case "/hosts":
			action := "GetGenericHostEntry"
			payload := ""
			if strings.Contains(r.Header.Get("SOAPAction"), "GetHostNumberOfEntries") {
				counts++
				n := 1
				if counts == 1 {
					n = 3
				}
				action = "GetHostNumberOfEntries"
				payload = fmt.Sprintf("<NewHostNumberOfEntries>%d</NewHostNumberOfEntries>", n)
			} else {
				hosts++
				b, _ := io.ReadAll(r.Body)
				if !strings.Contains(string(b), "<NewIndex>0</NewIndex>") {
					w.WriteHeader(500)
					io.WriteString(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><faultcode>s:Client</faultcode><faultstring>UPnPError</faultstring><detail><UPnPError><errorCode>713</errorCode><errorDescription>SpecifiedArrayIndexInvalid</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`)
					return
				}
				payload = "<NewActive>1</NewActive><NewHostName>host</NewHostName>"
			}
			fmt.Fprintf(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:%sResponse xmlns:u="%s">%s</u:%sResponse></s:Body></s:Envelope>`, action, service, payload, action)
		}
	}))
	defer srv.Close()
	root, err := upnp.LoadServices(srv.URL, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	m := &Metric{Service: service, Action: "GetGenericHostEntry", ActionArgument: &ActionArg{Name: "NewIndex", IsIndex: true, ProviderAction: "GetHostNumberOfEntries", Value: "HostNumberOfEntries"}, Result: "Active", CacheEntryTTL: 60, MetricType: prometheus.GaugeValue, PromDesc: JSONPromDesc{FqName: "gateway_hosts", VarLabels: []string{"gateway", "HostName"}}, Desc: prometheus.NewDesc("gateway_hosts", "Hosts", []string{"gateway", "hostname"}, nil)}
	metrics = []*Metric{m}
	luaMetrics = nil
	upnpCache = map[string]*upnpCacheEntry{}
	c := &FritzboxCollector{Root: root, Gateway: "test"}
	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(c)
	f, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if diagnosticValue(t, f, "fritzbox_exporter_query_success", "soap") != 0 || hosts != 2 || counts != 1 || len(upnpCache) != 0 {
		t.Fatalf("first scrape: hosts=%d counts=%d cache=%v", hosts, counts, upnpCache)
	}
	f, err = reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if diagnosticValue(t, f, "fritzbox_exporter_query_success", "soap") != 1 || hosts != 3 || counts != 2 {
		t.Fatalf("recovery: hosts=%d counts=%d", hosts, counts)
	}
}
