package main

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	upnp "github.com/sberk42/fritzbox_exporter/fritzbox_upnp"
	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
)

func TestHostCountDiagnosticPreservesCacheOnExpiredBudget(t *testing.T) {
	oldCache := upnpCache
	defer func() { upnpCache = oldCache }()
	result := upnp.Result{"HostNumberOfEntries": uint64(33)}
	entry := &upnpCacheEntry{Timestamp: 123, Result: &result}
	const service = "urn:dslforum-org:service:Hosts:1"
	upnpCache = map[string]*upnpCacheEntry{service + "|GetHostNumberOfEntries": entry}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fc := &FritzboxCollector{Gateway: "test", collectionContext: ctx}
	m := &Metric{Service: service, Action: "GetGenericHostEntry", CacheEntryTTL: 60,
		ActionArgument: &ActionArg{ProviderAction: "GetHostNumberOfEntries", Value: "HostNumberOfEntries"}}
	logger := logrus.StandardLogger()
	oldHooks := logger.ReplaceHooks(make(logrus.LevelHooks))
	defer logger.ReplaceHooks(oldHooks)
	hook := logtest.NewGlobal()
	fc.logHostCountCheck(m, 31, 33, 42, time.Now())
	event := hook.LastEntry()
	if event == nil || event.Data["first_invalid_index"] != 31 || event.Data["count"] != 33 || event.Data["recheck_error"] != context.Canceled.Error() {
		t.Fatalf("unexpected diagnostic: %+v", event)
	}
	if _, ok := event.Data["fresh_count"]; ok {
		t.Fatal("must not report cached count as fresh")
	}
	if upnpCache[service+"|GetHostNumberOfEntries"] != entry || entry.Timestamp != 123 || (*entry.Result)["HostNumberOfEntries"] != uint64(33) {
		t.Fatal("diagnostic changed cache")
	}
}

func TestDuplicateHostDiagnosticSources(t *testing.T) {
	old := upnpCache
	defer func() { upnpCache = old }()
	const service = "hosts"
	row := upnp.Result{"HostName": "Host", "MACAddress": "AA", "Active": uint64(1)}
	upnpCache = map[string]*upnpCacheEntry{service + "|GetGenericHostEntry|NewIndex|0": {Timestamp: 100, Result: &row}}
	m := &Metric{Service: service, Action: "GetGenericHostEntry", CacheEntryTTL: 60, PromDesc: JSONPromDesc{FqName: "gateway_hosts", VarLabels: []string{"gateway", "HostName", "MACAddress"}}}
	a := &upnp.ActionArgument{Name: "NewIndex", Value: 0}
	first := hostReadBeforeCall(m, a, 160)
	if first.Source != "cache" || first.Age != 60 {
		t.Fatalf("boundary: %+v", first)
	}
	expired := hostReadBeforeCall(m, a, 161)
	if expired.Source != "network" || expired.Age != 61 {
		t.Fatalf("expired: %+v", expired)
	}
	a.Value = 1
	second := hostReadBeforeCall(m, a, 160)
	if second.Source != "network" || second.Age != -1 {
		t.Fatalf("missing: %+v", second)
	}
	logger := logrus.StandardLogger()
	oldHooks := logger.ReplaceHooks(make(logrus.LevelHooks))
	defer logger.ReplaceHooks(oldHooks)
	hook := logtest.NewGlobal()
	fc := &FritzboxCollector{Gateway: "test"}
	seen := make(map[string]hostReadDiagnostic)
	fc.logDuplicateHost(m, row, 0, 32, 0, first, seen)
	if hook.LastEntry() != nil {
		t.Fatal("first row logged as duplicate")
	}
	normalized := upnp.Result{"HostName": "host", "MACAddress": "aa", "Active": uint64(0)}
	fc.logDuplicateHost(m, normalized, 1, 32, 0, second, seen)
	event := hook.LastEntry()
	if event == nil || event.Data["first_index"] != 0 || event.Data["duplicate_index"] != 1 || event.Data["first_source"] != "cache" || event.Data["duplicate_source"] != "network" {
		t.Fatalf("unexpected: %+v", event)
	}
	if len(seen) != 1 || upnpCache[service+"|GetGenericHostEntry|NewIndex|0"].Timestamp != 100 {
		t.Fatal("diagnostic mutated state")
	}
}

func TestHostResultDifferences(t *testing.T) {
	a := upnp.Result{"HostName": "private-host", "Active": uint64(1)}
	if got := hostResultDifferences(a, a); len(got) != 0 {
		t.Fatal(got)
	}
	b := upnp.Result{"HostName": "PRIVATE-HOST", "Active": uint64(0), "LeaseTimeRemaining": uint64(7)}
	want := []string{"Active", "HostName", "LeaseTimeRemaining"}
	if got := hostResultDifferences(a, b); !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}

func TestHostComparisonDoesNotLogValues(t *testing.T) {
	logger := logrus.StandardLogger()
	oldHooks := logger.ReplaceHooks(make(logrus.LevelHooks))
	defer logger.ReplaceHooks(oldHooks)
	hook := logtest.NewGlobal()
	fc := &FritzboxCollector{Gateway: "test"}
	m := &Metric{PromDesc: JSONPromDesc{VarLabels: []string{"gateway", "HostName", "MACAddress", "InterfaceType"}}}
	seen := make(map[string]hostReadDiagnostic)
	a := upnp.Result{"HostName": "private-host", "MACAddress": "private-mac", "Active": uint64(1)}
	fc.logDuplicateHost(m, a, 0, 2, 0, hostReadDiagnostic{}, seen)
	b := upnp.Result{"HostName": "PRIVATE-HOST", "MACAddress": "private-mac", "Active": uint64(0)}
	fc.logDuplicateHost(m, b, 1, 2, 0, hostReadDiagnostic{}, seen)
	event := hook.LastEntry()
	if event.Data["soap_results_equal"] != false {
		t.Fatal(event.Data)
	}
	if !reflect.DeepEqual(event.Data["missing_label_fields"], []string{"InterfaceType"}) {
		t.Fatal(event.Data)
	}
	line, _ := event.String()
	if strings.Contains(strings.ToLower(line), "private-") {
		t.Fatal("host values leaked")
	}
}
