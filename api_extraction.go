package main

import (
	"fmt"
	lua "github.com/sberk42/fritzbox_exporter/fritzbox_lua"
	"math"
	"strconv"
	"strings"
)

// ValueTransform explicitly selects one entry from a delimited numeric series.
// Index zero is the first entry; negative indexes count backwards from the end.
type APIValueTransform struct {
	Split string `json:"split"`
	Index int    `json:"index"`
}

func apiValue(data interface{}, path string) (interface{}, error) {
	if path == "" {
		return data, nil
	}
	for _, part := range strings.Split(path, ".") {
		switch v := data.(type) {
		case map[string]interface{}:
			var ok bool
			data, ok = v[part]
			if !ok {
				return nil, fmt.Errorf("missing field %s", part)
			}
		case []interface{}:
			i, e := strconv.Atoi(part)
			if e != nil {
				return nil, e
			}
			if i < 0 {
				i += len(v)
			}
			if i < 0 || i >= len(v) {
				return nil, fmt.Errorf("array index out of range")
			}
			data = v[i]
		default:
			return nil, fmt.Errorf("unexpected path type")
		}
	}
	if data == nil {
		return nil, fmt.Errorf("null value")
	}
	return data, nil
}
func apiRows(data interface{}, parts []string) ([]interface{}, error) {
	if len(parts) == 0 {
		return []interface{}{data}, nil
	}
	if parts[0] != "*" {
		v, e := apiValue(data, parts[0])
		if e != nil {
			return nil, e
		}
		return apiRows(v, parts[1:])
	}
	var children []interface{}
	switch v := data.(type) {
	case []interface{}:
		children = v
	case map[string]interface{}:
		for _, item := range v {
			children = append(children, item)
		}
	default:
		return nil, fmt.Errorf("wildcard requires a collection")
	}
	var rows []interface{}
	for _, v := range children {
		r, e := apiRows(v, parts[1:])
		if e != nil {
			return nil, e
		}
		rows = append(rows, r...)
	}
	return rows, nil
}
func extractAPIMetrics(renames *[]lua.LabelRename, data map[string]interface{}, m *LuaMetric) ([]lua.LuaMetricValue, error) {
	if len(m.LabelValues) == 0 && m.ParentPath == "" && len(m.Filter) == 0 && len(m.LabelPaths) == 0 && m.ValueTransform == nil {
		return lua.GetMetricsWithEmpty(renames, data, m.LuaMetricDef, m.AllowEmpty)
	}
	var parts []string
	if m.ResultPath != "" {
		parts = strings.Split(m.ResultPath, ".")
	}
	rows, err := apiRows(data, parts)
	if m.ParentPath != "" {
		var parents []interface{}
		parents, err = apiRows(data, strings.Split(m.ParentPath, "."))
		if err != nil {
			return nil, err
		}
		rows = nil
		for _, parent := range parents {
			children, e := apiRows(parent, parts)
			if e != nil {
				return nil, e
			}
			for _, child := range children {
				obj, ok := child.(map[string]interface{})
				if !ok {
					return nil, fmt.Errorf("child row must be an object")
				}
				copied := map[string]interface{}{"$parent": parent}
				for k, v := range obj {
					if k == "$parent" {
						return nil, fmt.Errorf("reserved parent key")
					}
					copied[k] = v
				}
				rows = append(rows, copied)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	var values []lua.LuaMetricValue
	for _, row := range rows {
		matches := true
		for key, want := range m.Filter {
			v, e := apiValue(row, key)
			if e != nil {
				return nil, e
			}
			if fmt.Sprint(v) != want {
				matches = false
			}
		}
		if !matches {
			continue
		}
		value, e := apiValue(row, m.ResultKey)
		if e != nil {
			return nil, e
		}
		if t := m.ValueTransform; t != nil {
			s, ok := value.(string)
			if !ok || t.Split == "" {
				return nil, fmt.Errorf("split requires a string and delimiter")
			}
			series := strings.Split(s, t.Split)
			i := t.Index
			if i < 0 {
				i += len(series)
			}
			if i < 0 || i >= len(series) {
				return nil, fmt.Errorf("series index out of range")
			}
			n, e := strconv.ParseFloat(strings.TrimSpace(series[i]), 64)
			if e != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return nil, fmt.Errorf("invalid numeric series sample")
			}
			value = n
		}
		prepared := map[string]interface{}{"value": value}
		for _, label := range m.PromDesc.VarLabels {
			if label == "gateway" {
				continue
			}
			if value, ok := m.LabelValues[label]; ok {
				prepared[label] = value
				continue
			}
			path, ok := m.LabelPaths[label]
			if !ok {
				path = label
			}
			v, e := apiValue(row, path)
			if e != nil {
				return nil, e
			}
			prepared[label] = v
		}
		def := m.LuaMetricDef
		def.Path = ""
		def.Key = "value"
		v, e := lua.GetMetrics(renames, prepared, def)
		if e != nil {
			return nil, e
		}
		values = append(values, v...)
	}
	if len(values) == 0 && !m.AllowEmpty {
		return nil, fmt.Errorf("no matching values")
	}
	return values, nil
}
