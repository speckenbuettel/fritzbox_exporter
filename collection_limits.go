package main

import (
	"context"
	"github.com/namsral/flag"
	"github.com/prometheus/client_golang/prometheus"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

var flagSOAPTimeout = flag.Duration("soap-timeout", 10*time.Second, "Timeout per SOAP HTTP request")
var flagLuaTimeout = flag.Duration("lua-timeout", 10*time.Second, "Timeout per Lua/login HTTP request")
var flagCollectionTimeout = flag.Duration("collection-timeout", 25*time.Second, "Total collection budget for each concurrently gathered backend (SOAP/Lua share one budget)")
var collectionTimeouts = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "fritzbox_exporter_collection_timeouts_total", Help: "Collections exhausting their total time budget."}, []string{"backend"})
var overlappingScrapes = prometheus.NewCounter(prometheus.CounterOpts{Name: "fritzbox_exporter_scrapes_rejected_total", Help: "Concurrent metrics requests rejected while a collection is still running."})

func collectionBudget(backend string) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(context.Background(), *flagCollectionTimeout)
	return ctx, func() {
		if ctx.Err() == context.DeadlineExceeded {
			collectionTimeouts.WithLabelValues(backend).Inc()
		}
		cancel()
	}
}
func transportOrDefault(t http.RoundTripper) http.RoundTripper {
	if t == nil {
		return http.DefaultTransport
	}
	return t
}

type budgetTransport struct {
	ctx  context.Context
	base http.RoundTripper
}
type budgetBody struct {
	io.ReadCloser
	cleanup func()
}

func (b *budgetBody) Close() error { defer b.cleanup(); return b.ReadCloser.Close() }
func (t budgetTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := t.ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(t.ctx, cancel)
	cleanup := func() { stop(); cancel() }
	response, err := t.base.RoundTrip(r.Clone(ctx))
	if err != nil {
		cleanup()
		return nil, err
	}
	response.Body = &budgetBody{response.Body, cleanup}
	return response, nil
}
func rejectOverlappingScrapes(next http.Handler) http.Handler {
	var active atomic.Bool
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !active.CompareAndSwap(false, true) {
			overlappingScrapes.Inc()
			http.Error(w, "collection already running", http.StatusServiceUnavailable)
			return
		}
		defer active.Store(false)
		next.ServeHTTP(w, r)
	})
}
