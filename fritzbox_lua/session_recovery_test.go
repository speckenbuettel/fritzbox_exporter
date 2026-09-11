package lua_client

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoginDoesNotReuseMissingSID(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<SessionInfo></SessionInfo>") }))
	defer s.Close()
	session := LuaSession{BaseURL: s.URL, ApiVer: "v2", SID: "stale", SessionInfo: SessionInfo{SID: "stale"}}
	if err := session.Login(); err == nil || session.SID != "" {
		t.Fatalf("stale login accepted: %v", err)
	}
}

func TestLuaRecoversExpiredSession(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login_sid.lua" {
			fmt.Fprint(w, "<SessionInfo><SID>renewed</SID></SessionInfo>")
			return
		}
		calls++
		r.ParseForm()
		if r.Form.Get("sid") == "stale" {
			w.WriteHeader(403)
			return
		}
		fmt.Fprint(w, `{"data":{"connected":1}}`)
	}))
	defer s.Close()
	session := LuaSession{BaseURL: s.URL, ApiVer: "v2", SID: "stale"}
	if _, err := session.LoadData(LuaPage{Path: "data.lua"}); err != nil || calls != 2 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}
