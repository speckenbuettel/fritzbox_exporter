package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAPIRebootRecovery(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		valid, permanent400   bool
		wantCalls, wantLogins int
	}{
		{"expired session after reboot", false, false, 2, 1},
		{"genuine bad request", true, true, 1, 0},
		{"400 persists after renewal", false, true, 2, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, checks, logins := 0, 0, 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/login_sid.lua" {
					if r.URL.Query().Get("sid") != "" {
						checks++
						sid := "0000000000000000"
						if tc.valid {
							sid = "before-reboot"
						}
						fmt.Fprintf(w, "<SessionInfo><SID>%s</SID></SessionInfo>", sid)
					} else {
						logins++
						fmt.Fprint(w, "<SessionInfo><SID>after-reboot</SID></SessionInfo>")
					}
					return
				}
				calls++
				if r.Header.Get("Authorization") == "AVM-SID before-reboot" || tc.permanent400 {
					w.WriteHeader(400)
					return
				}
				if r.Header.Get("Authorization") != "AVM-SID after-reboot" {
					t.Error("missing renewed SID")
				}
				fmt.Fprint(w, `{"value":42}`)
			}))
			defer s.Close()
			c := testAPI(t, s.URL)
			c.session.SID = "before-reboot"
			c.cache["old"] = apiCacheEntry{expires: time.Now().Add(time.Hour)}
			data, err := c.load("/api/v0/generic/example")
			if tc.permanent400 {
				if err == nil || !strings.Contains(err.Error(), "400") {
					t.Fatalf("expected HTTP 400: %v", err)
				}
			} else if err != nil || data["value"] != float64(42) {
				t.Fatalf("did not recover: %v %v", data, err)
			}
			if calls != tc.wantCalls || checks != 1 || logins != tc.wantLogins {
				t.Fatalf("calls=%d checks=%d logins=%d", calls, checks, logins)
			}
			if !tc.valid && len(c.cache) != 0 {
				t.Fatal("old session cache survived")
			}
		})
	}
}

func TestAPITransportRecoveryAndProviderReconnect(t *testing.T) {
	failed, logins := false, 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login_sid.lua" {
			logins++
			fmt.Fprint(w, "<SessionInfo><SID>renewed</SID></SessionInfo>")
			return
		}
		if !failed {
			failed = true
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			conn.Close()
			return
		}
		fmt.Fprint(w, `{"connected":1}`)
	}))
	defer s.Close()
	c := testAPI(t, s.URL)
	c.session.SID = "old"
	c.cache["old"] = apiCacheEntry{expires: time.Now().Add(time.Hour)}
	if _, err := c.load("/api/v0/generic/example"); err == nil || !strings.Contains(err.Error(), "EOF") {
		t.Fatalf("missing transport cause: %v", err)
	}
	if c.session.SID != "" || len(c.cache) != 0 {
		t.Fatal("transport error kept stale state")
	}
	if _, err := c.load("/api/v0/generic/example"); err != nil {
		t.Fatal(err)
	}
	// With the local session still valid, later reads (e.g. after a WAN
	// reconnection) need no new authentication and require no process restart.
	if _, err := c.load("/api/v0/generic/example"); err != nil {
		t.Fatal(err)
	}
	if logins != 1 {
		t.Fatalf("logins=%d", logins)
	}
}

func TestAPISessionCheckFailureIsBounded(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/login_sid.lua" {
			fmt.Fprint(w, "<html>private-session</html>")
			return
		}
		w.WriteHeader(400)
	}))
	defer s.Close()
	c := testAPI(t, s.URL)
	c.session.SID = "private-session"
	_, err := c.load("/api/v0/generic/example")
	if err == nil || strings.Contains(err.Error(), "private-session") || calls != 2 || c.session.SID != "" {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}
