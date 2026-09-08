package main

import (
	lua "github.com/sberk42/fritzbox_exporter/fritzbox_lua"
	"testing"
)

func TestAPIFilterAndSeries(t *testing.T) {
	data := map[string]interface{}{"items": []interface{}{map[string]interface{}{"type": "4", "state": "ready", "series": "51,49", "label": "LAN:1"}, map[string]interface{}{"type": "3", "state": "not active", "series": "9,8", "label": "WAN:1"}}}
	m := &LuaMetric{ResultPath: "items.*", ResultKey: "series", Filter: map[string]string{"type": "4"}, LabelPaths: map[string]string{"name": "label"}, ValueTransform: &APIValueTransform{Split: ",", Index: 0}, PromDesc: JSONPromDesc{VarLabels: []string{"name"}}, LuaMetricDef: lua.LuaMetricValueDefinition{Labels: []string{"name"}}}
	renames := []lua.LabelRename{}
	v, e := extractAPIMetrics(&renames, data, m)
	if e != nil || len(v) != 1 || v[0].Value != 51 || v[0].Labels["name"] != "LAN:1" {
		t.Fatalf("%+v %v", v, e)
	}
	m.ValueTransform.Index = -1
	v, e = extractAPIMetrics(&renames, data, m)
	if e != nil || v[0].Value != 49 {
		t.Fatal(v, e)
	}
	m.Filter["type"] = "5"
	m.AllowEmpty = true
	v, e = extractAPIMetrics(&renames, data, m)
	if e != nil || len(v) != 0 {
		t.Fatal(v, e)
	}
	m.Filter = map[string]string{"missing": "5"}
	if _, e = extractAPIMetrics(&renames, data, m); e == nil {
		t.Fatal("missing filter field hidden")
	}
}
func TestAPIInvalidSeries(t *testing.T) {
	for _, s := range []interface{}{"", "NaN", "Inf", "broken", 12} {
		m := &LuaMetric{ResultKey: "series", ValueTransform: &APIValueTransform{Split: ",", Index: 0}}
		if _, e := extractAPIMetrics(nil, map[string]interface{}{"series": s}, m); e == nil {
			t.Fatalf("accepted %v", s)
		}
	}
}

func TestAPIParentLabels(t *testing.T) {
	data := map[string]interface{}{"devices": []interface{}{map[string]interface{}{"UID": "disk1", "partitions": []interface{}{map[string]interface{}{"label": "data", "capacity": 12.0}}}}}
	m := &LuaMetric{ParentPath: "devices.*", ResultPath: "partitions.*", ResultKey: "capacity", LabelPaths: map[string]string{"device": "$parent.UID", "partition": "label"}, PromDesc: JSONPromDesc{VarLabels: []string{"device", "partition"}}, LuaMetricDef: lua.LuaMetricValueDefinition{Labels: []string{"device", "partition"}}, AllowEmpty: true}
	renames := []lua.LabelRename{}
	v, e := extractAPIMetrics(&renames, data, m)
	if e != nil || len(v) != 1 || v[0].Labels["device"] != "disk1" || v[0].Labels["partition"] != "data" {
		t.Fatal(v, e)
	}
	data["devices"] = []interface{}{}
	v, e = extractAPIMetrics(&renames, data, m)
	if e != nil || len(v) != 0 {
		t.Fatal(v, e)
	}
	delete(data, "devices")
	if _, e = extractAPIMetrics(&renames, data, m); e == nil {
		t.Fatal("missing parent hidden")
	}
}
