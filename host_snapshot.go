package main

import (
	"fmt"
	upnp "github.com/sberk42/fritzbox_exporter/fritzbox_upnp"
	"strconv"
)

// A failed or changing list is never published as a complete set of hosts.
func readHostSnapshot(call func(string, *upnp.ActionArgument) (upnp.Result, error), aa *ActionArg) ([]upnp.Result, error) {
	countResult, err := call(aa.ProviderAction, nil)
	if err != nil {
		return nil, err
	}
	count, err := strconv.Atoi(fmt.Sprint(countResult[aa.Value]))
	if err != nil || count < 0 || count > 65536 {
		return nil, fmt.Errorf("invalid host count")
	}
	rows := make([]upnp.Result, 0, count)
	for i := 0; i < count; i++ {
		row, err := call("GetGenericHostEntry", &upnp.ActionArgument{Name: aa.Name, Value: i})
		if err != nil {
			return nil, fmt.Errorf("GetGenericHostEntry index %d of %d: %w", i, count, err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}
