package main

import (
	"fmt"
	lua "github.com/sberk42/fritzbox_exporter/fritzbox_lua"
	"math"
	"net/url"
	"strconv"
	"strings"
)

// extractLuaDefinition preserves legacy extraction unless explicitly enabled.
func extractLuaDefinition(renames *[]lua.LabelRename, data map[string]interface{}, m *LuaMetric) ([]lua.LuaMetricValue, error) {
	if !m.PowerlineSpectrum {
		return lua.GetMetricsWithEmpty(renames, data, m.LuaMetricDef, m.AllowEmpty)
	}
	status, err := apiValue(data, "data.status")
	if err != nil || status != "SUCCESS" {
		return nil, fmt.Errorf("powerline spectrum unavailable")
	}
	root, err := apiValue(data, m.ResultPath)
	if err != nil {
		return nil, err
	}
	params, err := url.ParseQuery(m.Params)
	if err != nil {
		return nil, err
	}
	labels := map[string]string{}
	for _, side := range []string{"source", "target"} {
		v, e := apiValue(root, side+".macAddress")
		if e != nil {
			return nil, e
		}
		mac, ok := v.(string)
		if !ok || mac == "" {
			return nil, fmt.Errorf("missing spectrum adapter identity")
		}
		if expected := params.Get(side + "MacAddress"); expected == "" || !strings.EqualFold(mac, expected) {
			return nil, fmt.Errorf("spectrum adapter does not match requested link")
		}
		labels[side+"macaddress"] = mac
	}
	number := func(path string) (float64, error) {
		v, e := apiValue(root, path)
		if e != nil {
			return 0, e
		}
		n, ok := v.(float64)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, fmt.Errorf("invalid spectrum number: %s", path)
		}
		return n, nil
	}
	start, err := number("startFrequency")
	if err != nil {
		return nil, err
	}
	step, err := number("spacingFrequency")
	if err != nil {
		return nil, err
	}
	granularity, err := number("granularity")
	if err != nil {
		return nil, err
	}
	if start < 0 || step <= 0 || granularity <= 0 {
		return nil, fmt.Errorf("invalid spectrum scale")
	}
	v, err := apiValue(root, m.ResultKey)
	if err != nil {
		return nil, err
	}
	values, ok := v.([]interface{})
	if !ok || len(values) > 4096 {
		return nil, fmt.Errorf("invalid spectrum array")
	}
	if len(values) == 0 && !m.AllowEmpty {
		return nil, fmt.Errorf("empty spectrum")
	}
	result := make([]lua.LuaMetricValue, 0, len(values))
	for i, v := range values {
		n, ok := v.(float64)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, fmt.Errorf("invalid spectrum sample at %d", i)
		}
		freq := (start + step*float64(i)) / 1e6
		value := n / granularity / 4 // FRITZ!OS divides samples by granularity, then axis labels by four.
		if math.IsInf(freq, 0) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("spectrum overflow")
		}
		row := map[string]string{"sourcemacaddress": labels["sourcemacaddress"], "targetmacaddress": labels["targetmacaddress"], "frequency_mhz": strconv.FormatFloat(freq, 'f', 6, 64)}
		result = append(result, lua.LuaMetricValue{Value: value, Labels: row})
	}
	return result, nil
}
