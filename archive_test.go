package main

import (
	"encoding/json"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testArchive(t *testing.T) *archive {
	t.Helper()
	a, e := openArchive(filepath.Join(t.TempDir(), "events.db"), "box", "0123456789abcdef", "Asia/Singapore", 24*time.Hour, 100, 8)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { a.db.Close() })
	return a
}
func TestArchivePersistenceDedupAndRecovery(t *testing.T) {
	a := testArchive(t)
	now := time.Now().UTC()
	r := a.parseRouterLog("11.09.26 08:12:00 Internetverbindung getrennt.\n11.09.26 08:12:00 Internetverbindung getrennt.")
	if len(r) != 2 || r[0].EventTime != "2026-09-11T00:12:00Z" {
		t.Fatal(r)
	}
	for i := 0; i < 2; i++ {
		if e := a.store(archiveJob{Records: r}, now); e != nil {
			t.Fatal(e)
		}
	}
	var n int
	a.db.QueryRow("SELECT count(*) FROM events").Scan(&n)
	if n != 2 {
		t.Fatal(n)
	}
	f := archiveRecord{Kind: "query", Backend: "soap", Key: "1", Failed: true, Message: "timeout"}
	for i := 0; i < 2; i++ {
		if e := a.store(archiveJob{Records: []archiveRecord{f}}, now); e != nil {
			t.Fatal(e)
		}
	}
	a.db.QueryRow("SELECT count FROM events WHERE active_key IS NOT NULL").Scan(&n)
	if n != 2 {
		t.Fatal(n)
	}
	f.Failed = false
	if e := a.store(archiveJob{Records: []archiveRecord{f}}, now); e != nil {
		t.Fatal(e)
	}
	a.db.QueryRow("SELECT count(*) FROM events WHERE kind='recovery'").Scan(&n)
	if n != 1 {
		t.Fatal(n)
	}
	if e := a.store(archiveJob{Records: []archiveRecord{f}}, now); e != nil {
		t.Fatal(e)
	}
	a.db.QueryRow("SELECT count(*) FROM events WHERE kind='recovery'").Scan(&n)
	if n != 1 {
		t.Fatal(n)
	}
	var path string
	a.db.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&path)
	a.db.Close()
	b, e := openArchive(path, "box", "0123456789abcdef", "Asia/Singapore", 24*time.Hour, 100, 8)
	if e != nil {
		t.Fatal(e)
	}
	defer b.db.Close()
	b.db.QueryRow("SELECT count(*) FROM events").Scan(&n)
	if n != 4 {
		t.Fatal(n)
	}
}
func TestArchiveBoundsAuthAndFilters(t *testing.T) {
	a := testArchive(t)
	now := time.Now().UTC()
	records := []archiveRecord{}
	for i := 0; i < 120; i++ {
		records = append(records, archiveRecord{Kind: "router", Key: fmt.Sprint(i), Message: "<script>alert(1)</script>"})
	}
	if e := a.store(archiveJob{Records: records}, now); e != nil {
		t.Fatal(e)
	}
	var n int
	a.db.QueryRow("SELECT count(*) FROM events").Scan(&n)
	if n != 100 {
		t.Fatal(n)
	}
	for _, tc := range []struct {
		url, token string
		status     int
	}{{"/api/events", "", 401}, {"/api/events?limit=501", "0123456789abcdef", 400}, {"/api/events?limit=2&kind=router", "0123456789abcdef", 200}, {"/api/events?from=no", "0123456789abcdef", 400}} {
		req := httptest.NewRequest("GET", tc.url, nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		w := httptest.NewRecorder()
		a.events(w, req)
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
		if w.Code == 200 {
			var data map[string]any
			if e := json.Unmarshal(w.Body.Bytes(), &data); e != nil {
				t.Fatal(e)
			}
			if len(data["events"].([]any)) != 2 {
				t.Fatal(data)
			}
		}
	}
	if e := a.store(archiveJob{}, now.Add(25*time.Hour)); e != nil {
		t.Fatal(e)
	}
	a.db.QueryRow("SELECT count(*) FROM events").Scan(&n)
	if n != 0 {
		t.Fatal(n)
	}
}
func TestArchiveRedactionAndAsync(t *testing.T) {
	s := archiveSafe(`Get "http://user:password@box/data.lua?sid=abcdef&token=secret": error sid=abcdef`)
	if strings.Contains(s, "abcdef") || strings.Contains(s, "user:") || strings.Contains(s, "secret") {
		t.Fatal(s)
	}
	a := testArchive(t)
	a.run()
	a.enqueue(archiveJob{Records: []archiveRecord{{Kind: "router", Key: "a", Message: "x"}}, Poll: true})
	close(a.queue)
	a.wg.Wait()
	if a.lastPoll.Load() == 0 {
		t.Fatal("poll not persisted")
	}
}
func TestArchiveDiagnosticsDetail(t *testing.T) {
	a := testArchive(t)
	eventArchive = a
	defer func() { eventArchive = nil }()
	d := &queryDiagnostics{}
	ch := make(chan prometheus.Metric, 20)
	d.begin("lua", 0, "data.lua?page=energy", "cpu")
	d.fail("request", fmt.Errorf("HTTP 200 Content-Type text/html sid=secret"))
	d.collect(ch)
	job := <-a.queue
	if len(job.Records) != 1 || strings.Contains(job.Records[0].Message, "secret") || !strings.Contains(job.Records[0].Message, "HTTP 200") {
		t.Fatal(job)
	}
	if e := a.store(job, time.Now().UTC()); e != nil {
		t.Fatal(e)
	}
	d.begin("lua", 0, "data.lua?page=energy", "cpu")
	d.collect(ch)
	if e := a.store(<-a.queue, time.Now().UTC()); e != nil {
		t.Fatal(e)
	}
	var n int
	a.db.QueryRow("SELECT count(*) FROM events WHERE kind='recovery'").Scan(&n)
	if n != 1 {
		t.Fatal(n)
	}
}
func TestArchiveFailedWriteDoesNotAdvancePoll(t *testing.T) {
	a := testArchive(t)
	a.db.Close()
	a.run()
	a.enqueue(archiveJob{Poll: true})
	close(a.queue)
	a.wg.Wait()
	if a.lastPoll.Load() != 0 || a.writeErrors.Load() != 1 {
		t.Fatal("failed transaction reported as saved")
	}
}

func TestArchiveOptionalToken(t *testing.T) {
	for _, token := range []string{"", "0123456789abcdef"} {
		a, err := openArchive(filepath.Join(t.TempDir(), "events.db"), "box", token, "Europe/Berlin", time.Hour, 100, 8)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("GET", "/api/events", nil)
		w := httptest.NewRecorder()
		a.events(w, req)
		expected := 200
		if token != "" {
			expected = 401
		}
		if w.Code != expected {
			t.Fatalf("token=%t status=%d", token != "", w.Code)
		}
		view := httptest.NewRecorder()
		a.page(view, httptest.NewRequest("GET", "/events", nil))
		want := "const requiresToken=false"
		if token != "" {
			want = "const requiresToken=true"
		}
		if !strings.Contains(view.Body.String(), want) || strings.Contains(view.Body.String(), "/*ARCHIVE_AUTH*/") {
			t.Fatal("wrong browser auth mode")
		}
		a.db.Close()
	}
}
