package main

import (
	"context"
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
