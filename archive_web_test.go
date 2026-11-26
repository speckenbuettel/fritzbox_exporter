package main

import (
	"context"
	"encoding/json"
	lua "github.com/sberk42/fritzbox_exporter/fritzbox_lua"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWebEventsAllCategoriesAndDedup(t *testing.T) {
	a := testArchive(t)
	data := []byte(`{"data":{"filter":"all","log":[{"date":"25.11.26","time":"22:17:26","msg":"VPN getrennt. Ursache: 3 IKE server","group":"net"},{"date":"25.11.26","time":"22:17:26","msg":"VPN getrennt. Ursache: 3 IKE server","group":"net"},{"date":"25.11.26","time":"22:16:29","msg":"VPN getrennt. Ursache: 12 SA loss","group":"net"},{"date":"25.11.26","time":"22:18:00","msg":"WLAN verbunden","group":"wlan"}]}}`)
	rows, err := a.parseWebEvents(data)
	if err != nil || len(rows) != 4 {
		t.Fatal(err, len(rows))
	}
	soap := a.parseRouterLog("25.11.26 22:17:26 VPN getrennt. Ursache: 3 IKE server")
	if rows[0].Key != soap[0].Key || rows[1].Key == rows[0].Key {
		t.Fatal("dedup keys")
	}
	now := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	for _, r := range [][]archiveRecord{soap, rows, rows} {
		if err := a.store(archiveJob{Records: r}, now); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	a.db.QueryRow("select count(*) from events").Scan(&n)
	if n != 4 {
		t.Fatal(n)
	}
	for _, raw := range []string{`{}`, `{"data":{"filter":"net","log":[]}}`, `{"data":{"filter":"all","log":null}}`, `{"data":{"filter":"all","log":[{}]}}`} {
		if _, err := a.parseWebEvents([]byte(raw)); err == nil {
			t.Fatal(raw)
		}
	}
	if _, err := a.parseWebEvents([]byte(`{"data":{"filter":"all","log":[]}}`)); err != nil {
		t.Fatal(err)
	}
}
func TestWebPollAndFailure(t *testing.T) {
	a := testArchive(t)
	valid := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.URL.Path != "/data.lua" || r.Form.Get("page") != "log" || r.Form.Get("filter") != "all" {
			t.Error("wrong request")
		}
		if valid {
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"filter": "all", "log": []any{}}})
		} else {
			w.Write([]byte(`{"data":{}}`))
		}
	}))
	defer server.Close()
	a.webTransport = http.DefaultTransport
	a.webSession = &lua.LuaSession{BaseURL: server.URL, SID: "test"}
	a.pollWebEvents(context.Background())
	job := <-a.queue
	if !job.Poll || job.Records[0].Failed {
		t.Fatal(job)
	}
	valid = false
	a.pollWebEvents(context.Background())
	job = <-a.queue
	if job.Poll || !job.Records[0].Failed || a.pollErrors.Load() != 1 {
		t.Fatal(job)
	}
}

func TestWebPollCanceledDoesNotEnqueue(t *testing.T) {
	a := testArchive(t)
	a.webTransport = http.DefaultTransport
	a.webSession = &lua.LuaSession{BaseURL: "http://127.0.0.1:1", SID: "test"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.pollWebEvents(ctx)
	if len(a.queue) != 0 || a.pollErrors.Load() != 0 {
		t.Fatal("shutdown logged as failure")
	}
}
