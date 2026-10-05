package main

import (
	"context"
	upnp "github.com/sberk42/fritzbox_exporter/fritzbox_upnp"
	"strings"
	"testing"
	"time"
)

func TestHostTraceBoundsAndPrivacy(t *testing.T) {
	fc := &FritzboxCollector{}
	row := upnp.Result{"MACAddress": "private-mac", "IPAddress": "private-ip", "HostName": "private-name", "Active": uint64(1), "InterfaceType": "private-type"}
	id := fc.hostTrace.identity(row)
	if id != fc.hostTrace.identity(row) {
		t.Fatal("unstable identity")
	}
	fc.hostTrace.key[0] = 1
	if id == fc.hostTrace.identity(row) {
		t.Fatal("key not used")
	}
	trace := &hostTrace{seen: map[string]string{}}
	for i := 0; i < 100; i++ {
		fc.traceHostRow(trace, i, row, "network")
	}
	if len(trace.rows) != 64 || trace.total != 100 || !trace.anomaly {
		t.Fatal("bounds")
	}
	if strings.Contains(strings.Join(trace.rows, " "), "private") {
		t.Fatal("leak")
	}
	row["Active"] = uint64(0)
	if fc.hostTrace.identity(row) != strings.Split(trace.rows[0], ":")[1] {
		t.Fatal("state changed identity")
	}
}
func TestHostTraceDisabledExpiredAndBudget(t *testing.T) {
	old, start := *flagHostTrace, hostTraceStart
	defer func() { *flagHostTrace = old; hostTraceStart = start }()
	fc := &FritzboxCollector{}
	m := &Metric{Action: "GetGenericHostEntry"}
	*flagHostTrace = 0
	if fc.beginHostTrace(m, 0, 0) != nil {
		t.Fatal("disabled")
	}
	*flagHostTrace = time.Hour
	hostTraceStart = time.Now().Add(-2 * time.Hour)
	if fc.beginHostTrace(m, 0, 0) != nil {
		t.Fatal("expired")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	fc.collectionContext = ctx
	if _, status := fc.traceCall(m, "unused", nil); status != "budget_insufficient" {
		t.Fatal(status)
	}
	cancel()
	if _, status := fc.traceCall(m, "unused", nil); status != "budget_exhausted" {
		t.Fatal(status)
	}
}
