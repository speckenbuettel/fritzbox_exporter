package main

import (
	"encoding/json"
	"testing"
)

func TestPowerlineSpectrum(t *testing.T) {
	var data map[string]interface{}
	json.Unmarshal([]byte(`{"data":{"status":"SUCCESS","plcSpectrum":{"source":{"macAddress":"AA"},"target":{"macAddress":"BB"},"startFrequency":1806640,"spacingFrequency":97656,"granularity":4,"receiveCarrierData":{"primary":[576,0,160],"alternate":[]}}}}`), &data)
	m := &LuaMetric{PowerlineSpectrum: true, ResultPath: "data.plcSpectrum", ResultKey: "receiveCarrierData.primary"}
	m.Params = "sourceMacAddress=AA&targetMacAddress=BB"
	got, err := extractLuaDefinition(nil, data, m)
	if err != nil || len(got) != 3 {
		t.Fatalf("%v %v", got, err)
	}
	if got[0].Value != 36 || got[1].Labels["frequency_mhz"] != "1.904296" || got[2].Value != 10 {
		t.Fatalf("bad scaling: %v", got)
	}
	m.Params = "sourceMacAddress=AA&targetMacAddress=CC"
	if _, err = extractLuaDefinition(nil, data, m); err == nil {
		t.Fatal("accepted wrong target")
	}
	m.Params = "sourceMacAddress=AA&targetMacAddress=BB"
	m.ResultKey = "receiveCarrierData.alternate"
	m.AllowEmpty = true
	if got, err = extractLuaDefinition(nil, data, m); err != nil || len(got) != 0 {
		t.Fatal("empty alternate should be valid")
	}
	m.ResultKey = "receiveCarrierData.missing"
	if _, err = extractLuaDefinition(nil, data, m); err == nil {
		t.Fatal("accepted missing array")
	}
	m.ResultKey = "receiveCarrierData.primary"
	root := data["data"].(map[string]interface{})["plcSpectrum"].(map[string]interface{})
	root["granularity"] = float64(0)
	if _, err = extractLuaDefinition(nil, data, m); err == nil {
		t.Fatal("accepted invalid scale")
	}
	root["granularity"] = float64(4)
	root["receiveCarrierData"].(map[string]interface{})["primary"] = []interface{}{float64(576), "bad"}
	if got, err = extractLuaDefinition(nil, data, m); err == nil || len(got) != 0 {
		t.Fatal("accepted malformed partial spectrum")
	}
	data["data"].(map[string]interface{})["status"] = "VALIDATION_ERROR"
	if _, err = extractLuaDefinition(nil, data, m); err == nil {
		t.Fatal("accepted unsuccessful response")
	}
}
