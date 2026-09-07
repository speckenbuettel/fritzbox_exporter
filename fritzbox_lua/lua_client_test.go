package lua_client

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLegacyLuaTransport(t *testing.T) {
	for _, method := range []string{"POST", "GET"} {
		t.Run(method, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.ParseForm()
				if r.Method != method || r.Form.Get("sid") != "legacy" || r.Form.Get("page") != "energy" {
					t.Errorf("legacy request changed: %s %v", r.Method, r.Form)
				}
				fmt.Fprint(w, `{"data":{"value":12}}`)
			}))
			defer s.Close()
			session := LuaSession{BaseURL: s.URL, SID: "legacy"}
			path := "data.lua"
			if method == "GET" {
				path = "GET:" + path
			}
			b, err := session.LoadData(LuaPage{Path: path, Params: "page=energy"})
			if err != nil || string(b) != `{"data":{"value":12}}` {
				t.Fatalf("%s %v", b, err)
			}
		})
	}
}
