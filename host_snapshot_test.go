package main

import (
	"fmt"
	upnp "github.com/sberk42/fritzbox_exporter/fritzbox_upnp"
	"testing"
)

func TestHostSnapshotRefreshAndFailure(t *testing.T) {
	aa := &ActionArg{Name: "NewIndex", ProviderAction: "GetHostNumberOfEntries", Value: "HostNumberOfEntries"}
	count := 3
	fail := -1
	calls := 0
	call := func(action string, arg *upnp.ActionArgument) (upnp.Result, error) {
		calls++
		if arg == nil {
			return upnp.Result{"HostNumberOfEntries": fmt.Sprint(count)}, nil
		}
		if arg.Value == fail {
			return nil, fmt.Errorf("713 SpecifiedArrayIndexInvalid")
		}
		return upnp.Result{"HostName": fmt.Sprint(arg.Value)}, nil
	}
	rows, err := readHostSnapshot(call, aa)
	if err != nil || len(rows) != 3 {
		t.Fatal(rows, err)
	}
	count = 1
	rows, err = readHostSnapshot(call, aa)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	count = 3
	fail = 1
	calls = 0
	rows, err = readHostSnapshot(call, aa)
	if err == nil || rows != nil || calls != 3 {
		t.Fatalf("partial snapshot or continued enumeration: %v %v %d", rows, err, calls)
	}
}
