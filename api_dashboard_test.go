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
		"generic/connections": map[string]interface{}{"connection": []interface{}{map[string]interface{}{"is_active_internet_connection": "1", "media_type": "Fiber", "ip6_connstatus": "connected", "ip6_addr": "2001:db8::1", "ip6_prefix": "2001:db8::/64"}}},
		"generic/plc":         map[string]interface{}{"device": []interface{}{map[string]interface{}{"isLocal": "0", "mac": "02:00:00:00:00:01", "usr": "example-adapter", "model": "example-model", "phyRateRX": 300, "phyRateTX": 200}}},
		"generic/cpu":         map[string]interface{}{"StatTemperature": "52,51", "StatCPU": "15,14", "StatRAMStrictlyUsed": "39,38", "StatRAMCacheUsed": "40,41", "StatRAMPhysFree": "21,21"},
		"generic/vpn":         map[string]interface{}{"connection": []interface{}{map[string]interface{}{"name": "IPsec", "access_type": "3", "state": "ready", "activated": "1", "display_local_net": "example-local", "display_remote_net": "example-remote", "remote_ip": ""}, map[string]interface{}{"name": "WireGuard", "access_type": "4", "state": "not active", "activated": "1", "display_local_net": "example-local", "display_remote_net": "example-remote", "remote_ip": ""}}},
		"generic/power":       map[string]interface{}{"rate_sumact": "38", "rate_systemact": "71", "rate_wlanact": "50", "rate_abact": "0", "rate_usbhostact": "0"},
		"generic/eth_ports":   map[string]interface{}{"eth": []interface{}{map[string]interface{}{"label": "LAN:1", "carrier": "1"}}},
		"generic/dect":        map[string]interface{}{"enabled": "0"},
		"storage":             map[string]interface{}{"internalStorage": map[string]interface{}{"capacity": 1000, "usedSpace": 100}, "externalStorages": []interface{}{}},
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
		seenStorage, seenVPN := false, false
		for _, family := range families {
			if family.GetName() == "fritzbox_exporter_query_error" {
				t.Errorf("configuration errors: %v", family)
			}
			if family.GetName() == "gateway_system_storage_total_bytes" {
				seenStorage = true
				if len(family.Metric) != wantStorage {
					t.Errorf("storage samples = %d", len(family.Metric))
				}
			}
			if family.GetName() == "gateway_vpn_wireguard_status" {
				seenVPN = true
				if len(family.Metric) != 1 || family.Metric[0].Gauge.GetValue() != 0 {
					t.Error("VPN type filtering failed")
				}
			}
		}
		if !seenStorage || !seenVPN {
			t.Fatal("expected profile metrics missing")
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
