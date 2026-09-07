package lua_client

import "testing"

func TestEmptyResults(t *testing.T) {
	for _, tc := range []struct {
		body  string
		allow bool
		ok    bool
	}{
		{`{"devices":[]}`, true, true}, {`{"devices":{}}`, true, true}, {`{"devices":[]}`, false, false},
		{`{}`, true, false}, {`{"devices":null}`, true, false}, {`{"devices":""}`, true, false},
		{`{"devices":[{}]}`, true, false}, {`{"devices":[{"value":12},{}]}`, true, false},
		{`{"devices":[{"value":12}]}`, true, true},
	} {
		data, err := ParseJSON([]byte(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		_, err = GetMetricsWithEmpty(nil, data, LuaMetricValueDefinition{Path: "devices.*", Key: "value"}, tc.allow)
		if (err == nil) != tc.ok {
			t.Errorf("%s allow=%v: %v", tc.body, tc.allow, err)
		}
	}
}
func TestJSONErrors(t *testing.T) {
	for _, body := range []string{"null", "[]", "<html>", "{"} {
		if _, err := ParseJSON([]byte(body)); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
}
