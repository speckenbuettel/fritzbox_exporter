package main

import (
	"crypto/tls"
	"fmt"

	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func testAPI(t *testing.T, origin string) *APICollector {
	t.Helper()
	path := filepath.Join(t.TempDir(), "metrics-api.json")
	config := `{"metrics":[{"path":"generic/example","resultPath":"items.*","resultKey":"value","promType":"GaugeValue","promDesc":{"fqName":"test_api_value","help":"Value","varLabels":["name","gateway"]}},{"path":"generic/example","resultPath":"items.*","resultKey":"state","okValue":"ready","promType":"GaugeValue","promDesc":{"fqName":"test_api_up","help":"Status","varLabels":["name"]}}]}`
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := newAPICollector(path, origin, "monitor", "secret", "v1", true, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestAPICollectionAndConcurrentCache(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/api/v0/generic/example" || r.URL.RawQuery != "" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Authorization") != "AVM-SID test-session" || r.Header.Get("Client-Name") != "WebGUI" {
			t.Error("missing headers")
		}
		fmt.Fprint(w, `{"items":[{"name":"vpn","value":"42","state":"ready"}]}`)
	}))
	defer s.Close()
	c := testAPI(t, s.URL)
	c.session.SID = "test-session"
	reg := prometheus.NewRegistry()
	reg.MustRegister(c)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			families, err := reg.Gather()
			if err != nil {
				t.Error(err)
				return
			}
			for _, f := range families {
				if f.GetName() == "test_api_value" && f.Metric[0].GetGauge().GetValue() != 42 {
					t.Error("numeric string conversion")
				}
				if f.GetName() == "test_api_up" && f.Metric[0].GetGauge().GetValue() != 1 {
					t.Error("state conversion")
				}
			}
			if len(families) != 3 {
				t.Errorf("metric families: %d", len(families))
			}
		}()
	}
	wg.Wait()
	if calls != 1 {
		t.Errorf("cache request count=%d", calls)
	}
}
func TestAPIReauthentication(t *testing.T) {
	calls, logins := 0, 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login_sid.lua" {
			logins++
			fmt.Fprint(w, `<SessionInfo><SID>renewed</SID></SessionInfo>`)
			return
		}
		calls++
		if r.Header.Get("Authorization") == "AVM-SID expired" {
			w.WriteHeader(403)
			return
		}
		if r.Header.Get("Authorization") != "AVM-SID renewed" {
			t.Error("SID not renewed")
		}
		fmt.Fprint(w, `{"ok":1}`)
	}))
	defer s.Close()
	c := testAPI(t, s.URL)
	c.session.SID = "expired"
	if _, err := c.load("/api/v0/generic/example"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || logins != 1 {
		t.Fatalf("calls=%d logins=%d", calls, logins)
	}
}
func TestAPIFailureHandling(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{{"missing", 404, `{}`}, {"server", 500, `{}`}, {"html", 200, `<html>login</html>`}, {"null", 200, `null`}, {"array", 200, `[]`}, {"broken", 200, `{"a":`}} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer s.Close()
			c := testAPI(t, s.URL)
			c.session.SID = "valid"
			reg := prometheus.NewRegistry()
			reg.MustRegister(c)
			f, err := reg.Gather()
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || len(f) != 1 || f[0].Metric[0].GetCounter().GetValue() != 1 {
				t.Fatalf("calls=%d families=%v", calls, f)
			}
			if c.session.SID != "valid" {
				t.Error("non-auth failure cleared session")
			}
		})
	}
}
func TestAPIRejectPaths(t *testing.T) {
	for _, p := range []string{"https://other/x", "//other/x", "/generic/vpn", "../login", "generic/%2e%2e/login", "generic/vpn?sid=x", "GET:generic/vpn", "generic/vpn#x"} {
		if _, err := apiEndpoint(p, ""); err == nil {
			t.Errorf("accepted %s", p)
		}
	}
	if _, err := apiEndpoint("generic/vpn", "sid=x"); err == nil {
		t.Error("accepted query SID")
	}
	if ep, err := apiEndpoint("generic/vpn", "ui=a b"); err != nil || ep != "/api/v0/generic/vpn?ui=a+b" {
		t.Fatalf("%s %v", ep, err)
	}
}
func TestAPITimeoutAndRedirect(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		t.Run(fmt.Sprint(redirect), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if redirect {
					http.Redirect(w, r, "/elsewhere", 302)
				} else {
					time.Sleep(50 * time.Millisecond)
					fmt.Fprint(w, `{}`)
				}
			}))
			defer s.Close()
			c := testAPI(t, s.URL)
			c.session.SID = "valid"
			c.session.Client.Timeout = 10 * time.Millisecond
			if _, err := c.load("/api/v0/generic/example"); err == nil {
				t.Fatal("expected failure")
			}
		})
	}
}
func TestAPIInvalidConfig(t *testing.T) {
	for _, config := range []string{`{`, `{"metrics":[null]}`, `{"metrics":[{"path":"generic/example","resultKey":"x","promType":"bad","promDesc":{"fqName":"test"}}]}`} {
		p := filepath.Join(t.TempDir(), "config.json")
		os.WriteFile(p, []byte(config), 0600)
		if _, err := newAPICollector(p, "http://localhost", "", "", "v1", true, time.Second); err == nil {
			t.Errorf("accepted %s", config)
		}
	}
}
func TestAPIV2Login(t *testing.T) {
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login_sid.lua" {
			if r.URL.Query().Get("version") != "2" {
				t.Error("expected v2")
			}
			if r.Method == "POST" {
				r.ParseForm()
				if r.Form.Get("username") != "monitor" || !strings.HasPrefix(r.Form.Get("response"), "02$") {
					t.Error("bad challenge response")
				}
				fmt.Fprint(w, `<SessionInfo><SID>authenticated</SID></SessionInfo>`)
			} else {
				fmt.Fprint(w, `<SessionInfo><SID>0000000000000000</SID><Challenge>2$1$01$1$02</Challenge></SessionInfo>`)
			}
			return
		}
		fmt.Fprint(w, `{"ok":1}`)
	}))
	defer s.Close()
	c := testAPI(t, s.URL)
	c.session.Client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	c.session.ApiVer = "v2"
	if err := c.session.Login(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.load("/api/v0/generic/example"); err != nil {
		t.Fatal(err)
	}
}

func TestAPIPersistentForbidden(t *testing.T) {
	calls, logins := 0, 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login_sid.lua" {
			logins++
			fmt.Fprint(w, `<SessionInfo><SID>renewed</SID></SessionInfo>`)
			return
		}
		calls++
		w.WriteHeader(403)
	}))
	defer s.Close()
	c := testAPI(t, s.URL)
	c.session.SID = "expired"
	reg := prometheus.NewRegistry()
	reg.MustRegister(c)
	if _, err := reg.Gather(); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || logins != 1 || c.session.SID != "" {
		t.Fatalf("calls=%d logins=%d", calls, logins)
	}
}
func TestAPICacheExpiryAndMissingValues(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, `{"items":[{"name":"vpn"}]}`) }))
	defer s.Close()
	c := testAPI(t, s.URL)
	c.session.SID = "valid"
	reg := prometheus.NewRegistry()
	reg.MustRegister(c)
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if len(families) != 1 {
		t.Error("missing values emitted")
	}
	for key, entry := range c.cache {
		entry.expires = time.Now().Add(-time.Second)
		c.cache[key] = entry
	}
	if _, err := reg.Gather(); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || c.session.SID != "valid" {
		t.Error("cache expiry or session retention")
	}
}
