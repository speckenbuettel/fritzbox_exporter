package lua_client

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

func TestLuaDataUsesSessionTimeout(t *testing.T) {
	for _, path := range []string{"data.lua", "GET:data.lua"} {
		t.Run(path, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(150 * time.Millisecond) }))
			defer server.Close()
			session := LuaSession{BaseURL: server.URL, SID: "test", Client: http.Client{Timeout: 30 * time.Millisecond}}
			start := time.Now()
			_, err := session.LoadData(LuaPage{Path: path})
			if err == nil || time.Since(start) > time.Second {
				t.Fatalf("Lua timeout not respected: %v", err)
			}
		})
	}
}
