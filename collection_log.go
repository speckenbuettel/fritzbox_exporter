package main

import (
	"encoding/json"
	"fmt"
	"github.com/sirupsen/logrus"
	"sync"
	"time"
)

// Limit only collector messages. Metrics and query diagnostics run independently.
type collectionLogEntry struct{ entry *logrus.Entry }

var collectionLog = collectionLogEntry{logrus.NewEntry(logrus.StandardLogger())}
var collectionLogLimiter *repeatLogLimiter

type repeatedLog struct {
	entry   *logrus.Entry
	level   logrus.Level
	message string
	count   int
}
type repeatLogLimiter struct {
	mu      sync.Mutex
	pending map[string]*repeatedLog
}

func (l collectionLogEntry) WithFields(fields logrus.Fields) collectionLogEntry {
	return collectionLogEntry{l.entry.WithFields(fields)}
}
func (l collectionLogEntry) Warn(args ...interface{}) { l.emit(logrus.WarnLevel, fmt.Sprint(args...)) }
func (l collectionLogEntry) Warnf(f string, args ...interface{}) {
	l.emit(logrus.WarnLevel, fmt.Sprintf(f, args...))
}
func (l collectionLogEntry) Errorf(f string, args ...interface{}) {
	l.emit(logrus.ErrorLevel, fmt.Sprintf(f, args...))
}
func (l collectionLogEntry) emit(level logrus.Level, message string) {
	if collectionLogLimiter == nil {
		l.entry.Log(level, message)
		return
	}
	collectionLogLimiter.log(l.entry, level, message)
}
func (r *repeatLogLimiter) log(entry *logrus.Entry, level logrus.Level, message string) {
	// Index, count and cache age vary during the same fault. Keep identities and
	// differing field names in the key so unrelated conflicts are not combined.
	fields := logrus.Fields{}
	for _, k := range []string{"gateway", "service", "action", "metric", "xml_differing_fields", "soap_differing_fields"} {
		if v, ok := entry.Data[k]; ok {
			fields[k] = v
		}
	}
	encoded, _ := json.Marshal(fields)
	key := fmt.Sprintf("%d:%s:%s", level, message, encoded)
	r.mu.Lock()
	if previous := r.pending[key]; previous != nil {
		previous.count++
		previous.entry = entry
		r.mu.Unlock()
		return
	}
	// Bound memory even if errors contain changing text; overflow is logged.
	if len(r.pending) < 1024 {
		r.pending[key] = &repeatedLog{entry: entry, level: level, message: message}
	}
	r.mu.Unlock()
	entry.Log(level, message)
}
func (r *repeatLogLimiter) flush() {
	r.mu.Lock()
	pending := r.pending
	r.pending = make(map[string]*repeatedLog)
	r.mu.Unlock()
	for _, p := range pending {
		if p.count > 0 {
			p.entry.WithFields(logrus.Fields{"suppressed_repeats": p.count, "summary_window_seconds": 300}).Log(p.level, "repeated collection error: "+p.message)
		}
	}
}
func startCollectionLogSummaries() func() {
	limiter := &repeatLogLimiter{pending: make(map[string]*repeatedLog)}
	collectionLogLimiter = limiter
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				limiter.flush()
			case <-stop:
				limiter.flush()
				return
			}
		}
	}()
	return func() { close(stop); <-done }
}
