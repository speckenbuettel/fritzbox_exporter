package main

import (
	"context"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

//go:embed archive.html
var archiveHTML string

type eventRow struct {
	ID        int64  `json:"id"`
	Gateway   string `json:"gateway"`
	Kind      string `json:"kind"`
	Backend   string `json:"backend"`
	Source    string `json:"source"`
	Metric    string `json:"metric"`
	Reason    string `json:"reason"`
	Message   string `json:"message"`
	EventTime string `json:"event_time"`
	RawTime   string `json:"raw_time"`
	FirstSeen string `json:"first_seen"`
	LastSeen  string `json:"last_seen"`
	Count     int64  `json:"count"`
	Active    bool   `json:"active"`
}

func (a *archive) page(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", 405)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'")
	w.Write([]byte(strings.Replace(archiveHTML, "/*ARCHIVE_AUTH*/true", strconv.FormatBool(a.token != ""), 1)))
}
func (a *archive) events(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method != "GET" {
		http.Error(w, "method not allowed", 405)
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if a.token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(a.token)) != 1 {
		http.Error(w, "archive token required", 401)
		return
	}
	q := r.URL.Query()
	limit, offset := 200, 0
	if s := q.Get("limit"); s != "" {
		v, e := strconv.Atoi(s)
		if e != nil || v < 1 || v > 500 {
			http.Error(w, "invalid limit", 400)
			return
		}
		limit = v
	}
	if s := q.Get("offset"); s != "" {
		v, e := strconv.Atoi(s)
		if e != nil || v < 0 || v > 1000000 {
			http.Error(w, "invalid offset", 400)
			return
		}
		offset = v
	}
	where := []string{"1=1"}
	args := []any{}
	for _, filter := range []struct{ key, col string }{{"kind", "kind"}, {"backend", "backend"}, {"gateway", "gateway"}} {
		if s := q.Get(filter.key); s != "" {
			if len(s) > 256 {
				http.Error(w, "filter too long", 400)
				return
			}
			where = append(where, filter.col+" = ?")
			args = append(args, s)
		}
	}
	if s := q.Get("q"); s != "" {
		if len(s) > 256 {
			http.Error(w, "search too long", 400)
			return
		}
		where = append(where, "(instr(message,?)>0 OR instr(source,?)>0 OR instr(metric,?)>0)")
		args = append(args, s, s, s)
	}
	for _, filter := range []struct{ key, op string }{{"from", ">="}, {"to", "<="}} {
		if s := q.Get(filter.key); s != "" {
			t, e := time.Parse(time.RFC3339, s)
			if e != nil {
				http.Error(w, "invalid time; use RFC3339", 400)
				return
			}
			where = append(where, "last_seen "+filter.op+" ?")
			args = append(args, t.UTC().Format(archiveTimeLayout))
		}
	}
	args = append(args, limit, offset)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := a.db.QueryContext(ctx, `SELECT id,gateway,kind,backend,source,metric,reason,message,event_time,raw_time,first_seen,last_seen,count,active_key IS NOT NULL FROM events WHERE `+strings.Join(where, " AND ")+` ORDER BY last_seen DESC,event_time DESC,id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		http.Error(w, "archive unavailable", 503)
		return
	}
	data := []eventRow{}
	for rows.Next() {
		var e eventRow
		if err = rows.Scan(&e.ID, &e.Gateway, &e.Kind, &e.Backend, &e.Source, &e.Metric, &e.Reason, &e.Message, &e.EventTime, &e.RawTime, &e.FirstSeen, &e.LastSeen, &e.Count, &e.Active); err != nil {
			break
		}
		data = append(data, e)
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil || rowErr != nil {
		http.Error(w, "archive unavailable", 503)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{"events": data, "offset": offset, "limit": limit, "gateway": a.gateway, "router_timezone": a.zone.String(), "last_poll": a.lastPoll.Load(), "write_errors": a.writeErrors.Load(), "dropped": a.dropped.Load(), "poll_errors": a.pollErrors.Load()})
}
