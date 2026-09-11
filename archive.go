package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/namsral/flag"
	"github.com/prometheus/client_golang/prometheus"
	_ "modernc.org/sqlite"
)

var (
	flagArchiveFile      = flag.String("archive-file", "", "SQLite archive path; empty disables archive")
	flagArchiveToken     = flag.String("archive-token", "", "Read API bearer token (required when archive enabled)")
	flagArchiveTimezone  = flag.String("archive-timezone", "Europe/Berlin", "Router event timezone")
	flagArchiveInterval  = flag.Duration("archive-interval", 30*time.Second, "Router event polling interval")
	flagArchiveRetention = flag.Duration("archive-retention", 90*24*time.Hour, "Archive retention")
	flagArchiveMaxRows   = flag.Int("archive-max-rows", 50000, "Maximum archive rows")
	flagArchiveMaxMB     = flag.Int("archive-max-mb", 64, "Maximum main SQLite database size in MiB; journal overhead additional")
	eventArchive         *archive
	archiveWriteErrors   = prometheus.NewCounter(prometheus.CounterOpts{Name: "fritzbox_exporter_archive_write_errors_total", Help: "Failed archive transactions."})
	archiveDropped       = prometheus.NewCounter(prometheus.CounterOpts{Name: "fritzbox_exporter_archive_dropped_total", Help: "Archive records dropped because queue is full."})
	archivePollErrors    = prometheus.NewCounter(prometheus.CounterOpts{Name: "fritzbox_exporter_archive_poll_errors_total", Help: "Failed router event polls."})
	archiveLastPoll      = prometheus.NewGauge(prometheus.GaugeOpts{Name: "fritzbox_exporter_archive_last_success_timestamp_seconds", Help: "Last router log successfully committed to archive."})
)

type archiveRecord struct {
	Key       string
	Kind      string
	Backend   string
	Source    string
	Metric    string
	Reason    string
	Message   string
	EventTime string
	RawTime   string
	Failed    bool
}
type archiveJob struct {
	Records []archiveRecord
	Poll    bool
}

const archiveTimeLayout = "2006-01-02T15:04:05.000000000Z"

type archive struct {
	db             *sql.DB
	gateway, token string
	zone           *time.Location
	retention      time.Duration
	maxRows        int
	queue          chan archiveJob
	queueMu        sync.RWMutex
	closed         bool
	wg             sync.WaitGroup
	lastPoll       atomic.Int64
	writeErrors    atomic.Uint64
	dropped        atomic.Uint64
	pollErrors     atomic.Uint64
}

func openArchive(path, gateway, token, zone string, retention time.Duration, maxRows, maxMB int) (*archive, error) {
	if len(token) < 16 {
		return nil, fmt.Errorf("ARCHIVE_TOKEN must contain at least 16 characters")
	}
	if retention <= 0 || maxRows < 100 || maxMB < 8 {
		return nil, fmt.Errorf("invalid archive retention or size limits")
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	// Open a local filename, never a user supplied SQL URI.
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	uriPath := filepath.ToSlash(abs)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	uri := url.URL{Scheme: "file", Path: uriPath}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	good := false
	defer func() {
		if !good {
			db.Close()
		}
	}()
	for _, q := range []string{"PRAGMA busy_timeout=2000", "PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL"} {
		if _, err = db.Exec(q); err != nil {
			return nil, fmt.Errorf("%s: %w", q, err)
		}
	}
	var version, pageSize int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return nil, err
	}
	if version > 1 {
		return nil, fmt.Errorf("unsupported archive schema version %d", version)
	}
	if err = db.QueryRow("PRAGMA page_size").Scan(&pageSize); err != nil {
		return nil, err
	}
	if _, err = db.Exec(fmt.Sprintf("PRAGMA max_page_count=%d", int64(maxMB)*1024*1024/int64(pageSize))); err != nil {
		return nil, err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS events (
 id INTEGER PRIMARY KEY, gateway TEXT NOT NULL, kind TEXT NOT NULL, backend TEXT NOT NULL,
 source TEXT NOT NULL, metric TEXT NOT NULL, reason TEXT NOT NULL, message TEXT NOT NULL,
 event_time TEXT NOT NULL, raw_time TEXT NOT NULL, first_seen TEXT NOT NULL, last_seen TEXT NOT NULL,
 count INTEGER NOT NULL DEFAULT 1, active_key TEXT UNIQUE, dedup_key TEXT UNIQUE);
 CREATE INDEX IF NOT EXISTS events_time ON events(last_seen);
 PRAGMA user_version=1;`)
	if err != nil {
		return nil, err
	}
	os.Chmod(path, 0600)
	a := &archive{db: db, gateway: gateway, token: token, zone: loc, retention: retention, maxRows: maxRows, queue: make(chan archiveJob, 128)}
	good = true
	return a, nil
}
func (a *archive) enqueue(job archiveJob) {
	a.queueMu.RLock()
	defer a.queueMu.RUnlock()
	if a.closed {
		return
	}
	select {
	case a.queue <- job:
	default:
		a.dropped.Add(1)
		archiveDropped.Inc()
	}
}
func (a *archive) run() {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		for job := range a.queue {
			if err := a.store(job, time.Now().UTC()); err != nil {
				a.writeErrors.Add(1)
				archiveWriteErrors.Inc()
				fmt.Fprintln(os.Stderr, "event archive: write failed:", err)
			}
		}
	}()
}
func (a *archive) close() {
	a.queueMu.Lock()
	a.closed = true
	close(a.queue)
	a.queueMu.Unlock()
	a.wg.Wait()
	a.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	a.db.Close()
}
func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

var secretParameter = regexp.MustCompile(`(?i)(sid|password|passwd|token|authorization|response|challenge)=([^\s&"<>]+)`)

func archiveSafe(s string) string {
	if *flagPassword != "" {
		s = strings.ReplaceAll(s, *flagPassword, "[redacted]")
	}
	s = secretParameter.ReplaceAllString(s, "${1}=[redacted]")
	// Error URLs can carry session identifiers in query strings or userinfo.
	words := strings.Fields(s)
	for _, w := range words {
		candidate := strings.Trim(w, "\"',():")
		if strings.HasPrefix(candidate, "http://") || strings.HasPrefix(candidate, "https://") {
			if u, e := url.Parse(candidate); e == nil {
				u.RawQuery = ""
				u.Fragment = ""
				u.User = nil
				s = strings.ReplaceAll(s, candidate, u.String())
			}
		}
	}
	if len(s) > 8192 {
		s = s[:8192] + " [truncated]"
	}
	return strings.ToValidUTF8(s, "�")
}
func (a *archive) store(job archiveJob, now time.Time) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stamp := now.UTC().Format(archiveTimeLayout)
	if _, err = tx.ExecContext(ctx, "DELETE FROM events WHERE last_seen < ?", now.Add(-a.retention).UTC().Format(archiveTimeLayout)); err != nil {
		return err
	}
	for _, r := range job.Records {
		r.Message = archiveSafe(r.Message)
		r.Source = archiveSafe(r.Source)
		var active, dedup any
		kind := r.Kind
		if kind == "router" {
			dedup = digest(a.gateway + "|" + r.Key)
		} else {
			key := a.gateway + "|" + r.Backend + "|" + r.Key
			if !r.Failed {
				res, e := tx.ExecContext(ctx, "UPDATE events SET active_key=NULL WHERE active_key=?", key)
				if e != nil {
					return e
				}
				n, _ := res.RowsAffected()
				if n == 0 {
					continue
				}
				kind = "recovery"
				r.Message = "Abfrage wieder erfolgreich"
			} else {
				var id int64
				var reason, message string
				e := tx.QueryRowContext(ctx, "SELECT id,reason,message FROM events WHERE active_key=?", key).Scan(&id, &reason, &message)
				if e == nil && reason == r.Reason && message == r.Message {
					if _, e = tx.ExecContext(ctx, "UPDATE events SET last_seen=?,count=count+1 WHERE id=?", stamp, id); e != nil {
						return e
					}
					continue
				}
				if e != nil && e != sql.ErrNoRows {
					return e
				}
				if _, e = tx.ExecContext(ctx, "UPDATE events SET active_key=NULL WHERE active_key=?", key); e != nil {
					return e
				}
				active = key
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO events(gateway,kind,backend,source,metric,reason,message,event_time,raw_time,first_seen,last_seen,active_key,dedup_key)
   VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(dedup_key) DO NOTHING`, a.gateway, kind, r.Backend, r.Source, r.Metric, r.Reason, r.Message, r.EventTime, r.RawTime, stamp, stamp, active, dedup)
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, "DELETE FROM events WHERE id IN (SELECT id FROM events ORDER BY last_seen DESC,id DESC LIMIT -1 OFFSET ?)", a.maxRows)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if job.Poll {
		a.lastPoll.Store(now.Unix())
		archiveLastPoll.Set(float64(now.Unix()))
	}
	// Single short transactions and explicit checkpoints bound the sidecar growth.
	a.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return nil
}
func (a *archive) parseRouterLog(body string) []archiveRecord {
	records := []archiveRecord{}
	seen := map[string]int{}
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		r := archiveRecord{Kind: "router", Backend: "router", Source: "DeviceInfo/GetDeviceLog", Message: line}
		if len(line) >= 17 {
			raw := line[:17]
			if t, e := time.ParseInLocation("02.01.06 15:04:05", raw, a.zone); e == nil {
				r.EventTime = t.UTC().Format(time.RFC3339)
				r.RawTime = raw
				r.Message = strings.TrimSpace(line[17:])
			}
		}
		seen[line]++
		r.Key = fmt.Sprintf("%s|%d", line, seen[line])
		records = append(records, r)
	}
	return records
}
