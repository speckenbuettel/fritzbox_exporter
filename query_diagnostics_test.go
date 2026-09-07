package main

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	lua "github.com/sberk42/fritzbox_exporter/fritzbox_lua"
	upnp "github.com/sberk42/fritzbox_exporter/fritzbox_upnp"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func diagnostic(t *testing.T, r *prometheus.Registry, name, collector string) float64 {
	t.Helper()
	families, err := r.Gather()
	if err != nil {
		t.Fatal(err)
	}
	return diagnosticValue(t, families, name, collector)
}
func diagnosticValue(t *testing.T, families []*dto.MetricFamily, name, collector string) float64 {
	t.Helper()
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.Metric {
			for _, l := range m.Label {
				if l.GetName() == "collector" && l.GetValue() == collector {
					if m.Counter != nil {
						return m.Counter.GetValue()
					}
					return m.Gauge.GetValue()
				}
			}
		}
	}
	t.Fatalf("missing %s %s", name, collector)
	return 0
}
func TestAPIQueryRecovery(t *testing.T) {
	body := `{"items":[]}`
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
	defer s.Close()
	c := testAPI(t, s.URL)
	c.session.SID = "valid"
	for _, m := range c.metrics {
		m.AllowEmpty = true
	}
	r := prometheus.NewRegistry()
	r.MustRegister(c)
	if v := diagnostic(t, r, "fritzbox_exporter_query_success", "api"); v != 1 {
		t.Fatal(v)
	}
	if v := diagnostic(t, r, "fritzbox_exporter_query_results", "api"); v != 0 {
		t.Fatal(v)
	}
	body = `{}`
	c.cache = map[string]apiCacheEntry{}
	if v := diagnostic(t, r, "fritzbox_exporter_query_success", "api"); v != 0 {
		t.Fatal(v)
	}
	body = `{"items":[{"value":12,"state":"ready","name":"vpn"}]}`
	c.cache = map[string]apiCacheEntry{}
	f, err := r.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if v := diagnosticValue(t, f, "fritzbox_exporter_query_success", "api"); v != 1 {
		t.Fatal(v)
	}
	if v := diagnosticValue(t, f, "fritzbox_exporter_query_errors_total", "api"); v != 1 {
		t.Fatal(v)
	}
	for _, family := range f {
		if family.GetName() == "fritzbox_exporter_query_error" {
			t.Error("stale error after recovery")
		}
	}
}
func TestSOAPAndLuaQueryDiagnostics(t *testing.T) {
	savedMetrics, savedLua, savedUpnpCache, savedLuaCache := metrics, luaMetrics, upnpCache, luaCache
	defer func() {
		metrics, luaMetrics, upnpCache, luaCache = savedMetrics, savedLua, savedUpnpCache, savedLuaCache
	}()
	metrics = []*Metric{{Service: "missing", Action: "GetInfo", Result: "Value", PromDesc: JSONPromDesc{FqName: "soap_value"}, Desc: prometheus.NewDesc("soap_value", "Value", nil, nil), MetricType: prometheus.GaugeValue, CacheEntryTTL: 30}}
	lm := &LuaMetric{Path: "data.lua", Params: "page=usbOv", AllowEmpty: true, CacheEntryTTL: 30, PromDesc: JSONPromDesc{FqName: "lua_value"}, Desc: prometheus.NewDesc("lua_value", "Value", nil, nil), MetricType: prometheus.GaugeValue, LuaMetricDef: lua.LuaMetricValueDefinition{Path: "devices.*", Key: "value"}}
	luaMetrics = []*LuaMetric{lm}
	upnpCache = map[string]*upnpCacheEntry{}
	data := map[string]interface{}{"devices": []interface{}{}}
	luaCache = map[string]*luaCacheEntry{"data.lua_page=usbOv": {Timestamp: time.Now().Unix(), Result: &data}}
	c := &FritzboxCollector{Root: &upnp.Root{}, LuaSession: &lua.LuaSession{SID: "valid"}}
	r := prometheus.NewPedanticRegistry()
	r.MustRegister(c)
	families, err := r.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if v := diagnosticValue(t, families, "fritzbox_exporter_query_success", "soap"); v != 0 {
		t.Fatal(v)
	}
	if v := diagnosticValue(t, families, "fritzbox_exporter_query_success", "lua"); v != 1 {
		t.Fatal(v)
	}
	result := upnp.Result{"Value": float64(5)}
	upnpCache["missing|GetInfo"] = &upnpCacheEntry{Timestamp: time.Now().Unix(), Result: &result}
	if v := diagnostic(t, r, "fritzbox_exporter_query_success", "soap"); v != 1 {
		t.Fatal(v)
	}
	data = map[string]interface{}{}
	luaCache["data.lua_page=usbOv"] = &luaCacheEntry{Timestamp: time.Now().Unix(), Result: &data}
	families, err = r.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if v := diagnosticValue(t, families, "fritzbox_exporter_query_success", "lua"); v != 0 {
		t.Fatal(v)
	}
	if c.LuaSession.SID != "valid" {
		t.Error("extraction failure cleared SID")
	}
	api := testAPI(t, "http://localhost")
	api.metrics = nil
	r.MustRegister(api) // shared diagnostic descriptors must coexist
}
func TestLuaSourceDoesNotIncludeCredentials(t *testing.T) {
	s := luaQuerySource(&LuaMetric{Path: "data.lua", Params: "page=usbOv&sid=secret&password=hidden"})
	if strings.Contains(s, "secret") || strings.Contains(s, "hidden") || s != "data.lua?page=usbOv" {
		t.Fatal(s)
	}
}
