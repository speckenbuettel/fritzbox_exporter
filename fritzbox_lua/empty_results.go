package lua_client

import (
	"fmt"
	"strings"
)

// GetMetricsWithEmpty allows only structurally valid empty wildcard collections.
// Missing keys, nulls and wrong types remain errors, even with allowEmpty.
func GetMetricsWithEmpty(renames *[]LabelRename, data map[string]interface{}, def LuaMetricValueDefinition, allowEmpty bool) ([]LuaMetricValue, error) {
	if !allowEmpty {
		return GetMetrics(renames, data, def)
	}
	var parts []string
	if def.Path != "" {
		parts = strings.Split(def.Path, ".")
	}
	rows, err := strictRows(data, parts)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []LuaMetricValue{}, nil
	}
	// Evaluate individually so malformed rows cannot disappear behind valid siblings.
	var values []LuaMetricValue
	for _, row := range rows {
		wrapped := map[string]interface{}{"row": row}
		single := def
		single.Path = "row"
		v, err := GetMetrics(renames, wrapped, single)
		if err != nil {
			return nil, err
		}
		values = append(values, v...)
	}
	return values, nil
}
func strictRows(data interface{}, parts []string) ([]interface{}, error) {
	if len(parts) == 0 {
		return []interface{}{data}, nil
	}
	if parts[0] != "*" {
		next, err := getValueFromHashOrArray(data, parts[0], "")
		if err != nil {
			return nil, err
		}
		return strictRows(next, parts[1:])
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
		return nil, fmt.Errorf("wildcard requires an object or array")
	}
	rows := []interface{}{}
	for _, child := range children {
		found, err := strictRows(child, parts[1:])
		if err != nil {
			return nil, err
		}
		rows = append(rows, found...)
	}
	return rows, nil
}
