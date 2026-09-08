package main

import (
	"encoding/json"
	"github.com/prometheus/client_golang/prometheus"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDashboardAPIConfiguration(t *testing.T) {
	fixtures := map[string]interface{}{
		"generic/cpu":       map[string]interface{}{"StatTemperature": "52,51", "StatCPU": "15,14", "StatRAMStrictlyUsed": "39,38", "StatRAMCacheUsed": "40,41", "StatRAMPhysFree": "21,21"},
		"generic/vpn":       map[string]interface{}{"connection": []interface{}{map[string]interface{}{"name": "IPsec", "access_type": "3", "state": "ready", "activated": "1"}, map[string]interface{}{"name": "WireGuard", "access_type": "4", "state": "not active", "activated": "1"}}},
		"generic/power":     map[string]interface{}{"rate_sumact": "38", "rate_systemact": "71", "rate_wlanact": "50", "rate_abact": "0", "rate_usbhostact": "0"},
		"generic/eth_ports": map[string]interface{}{"eth": []interface{}{map[string]interface{}{"label": "LAN:1", "carrier": "1"}}},
		"generic/dect":      map[string]interface{}{"enabled": "0"},
		"storage":           map[string]interface{}{"internalStorage": map[string]interface{}{"capacity": 1000, "usedSpace": 100}, "externalStorages": []interface{}{}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		obj, ok := fixtures[r.URL.Path[len("/api/v0/"):]]
		if !ok {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(obj)
	}))
	defer server.Close()
	c, e := newAPICollector("examples/dashboard/5690/metrics-api.json", server.URL, "user", "pass", "v2", true, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	c.session.SID = "test"
	registry := prometheus.NewPedanticRegistry()
	registry.MustRegister(c)
	check := func(wantStorage int) {
		t.Helper()
		families, e := registry.Gather()
		if e != nil {
			t.Fatal(e)
		}
		for _, family := range families {
			if family.GetName() == "fritzbox_exporter_query_error" {
				t.Errorf("configuration errors: %v", family)
			}
			if family.GetName() == "gateway_data_storage_total" && len(family.Metric) != wantStorage {
				t.Errorf("storage samples = %d", len(family.Metric))
			}
			if family.GetName() == "gateway_vpn_wireguard" && (len(family.Metric) != 1 || family.Metric[0].Gauge.GetValue() != 0) {
				t.Error("VPN type filtering failed")
			}
		}
	}
	check(1)
	fixtures["storage"].(map[string]interface{})["externalStorages"] = []interface{}{
		map[string]interface{}{"UID": "disk1", "partitions": []interface{}{map[string]interface{}{"label": "data", "capacity": 2000, "usedSpace": 200}, map[string]interface{}{"label": "backup", "capacity": 3000, "usedSpace": 300}}},
		map[string]interface{}{"UID": "disk2", "partitions": []interface{}{map[string]interface{}{"label": "data", "capacity": 4000, "usedSpace": 400}}},
	}
	c.cache = map[string]apiCacheEntry{}
	check(4)
}
