package main

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"net/url"
	"strings"
	"time"
)

type queryDescriptors struct {
	success, results, errors, lastSuccess, errorReason *prometheus.Desc
}

var queryDescs = makeQueryDescriptors()

func makeQueryDescriptors() map[string]queryDescriptors {
	result := map[string]queryDescriptors{}
	for _, collector := range []string{"soap", "lua", "api"} {
		labels := []string{"query", "source", "metric"}
		constants := prometheus.Labels{"collector": collector}
		result[collector] = queryDescriptors{
			prometheus.NewDesc("fritzbox_exporter_query_success", "Whether the last evaluation succeeded, including a valid empty result. Cached data may be used.", labels, constants),
			prometheus.NewDesc("fritzbox_exporter_query_results", "Number of emitted samples in the last evaluation.", labels, constants),
			prometheus.NewDesc("fritzbox_exporter_query_errors_total", "Number of failed evaluations, at most once per definition per scrape.", labels, constants),
			prometheus.NewDesc("fritzbox_exporter_query_last_success_timestamp_seconds", "Time of last successful evaluation, including cached or empty results; zero means never.", labels, constants),
			prometheus.NewDesc("fritzbox_exporter_query_error", "Current failure category; emitted only for failed evaluations.", append(append([]string{}, labels...), "reason"), constants),
		}
	}
	return result
}

type queryObservation struct {
	labels      []string
	reason      string
	count       float64
	errors      float64
	lastSuccess float64
}
type queryDiagnostics struct {
	entries map[string]*queryObservation
	current *queryObservation
	pending []*queryObservation
}

func (d *queryDiagnostics) begin(collector string, index int, source, metric string) {
	if d.entries == nil {
		d.entries = map[string]*queryObservation{}
	}
	key := fmt.Sprintf("%s:%d", collector, index)
	q := d.entries[key]
	if q == nil {
		q = &queryObservation{labels: []string{collector, fmt.Sprint(index + 1), source, metric}}
		d.entries[key] = q
	}
	q.reason = ""
	q.count = 0
	d.current = q
	d.pending = append(d.pending, q)
}
func (d *queryDiagnostics) fail(reason string) {
	if d.current != nil && d.current.reason == "" {
		d.current.reason = reason
	}
}
func (d *queryDiagnostics) emitted() {
	if d.current != nil {
		d.current.count++
	}
}
func describeQueries(ch chan<- *prometheus.Desc, collectors ...string) {
	for _, collector := range collectors {
		desc := queryDescs[collector]
		for _, metric := range []*prometheus.Desc{desc.success, desc.results, desc.errors, desc.lastSuccess, desc.errorReason} {
			ch <- metric
		}
	}
}
func (d *queryDiagnostics) collect(ch chan<- prometheus.Metric) {
	for _, q := range d.pending {
		desc := queryDescs[q.labels[0]]
		labels := q.labels[1:]
		success := 1.0
		if q.reason != "" {
			success = 0
			q.errors++
			ch <- prometheus.MustNewConstMetric(desc.errorReason, prometheus.GaugeValue, 1, append(append([]string{}, labels...), q.reason)...)
		} else {
			q.lastSuccess = float64(time.Now().Unix())
		}
		ch <- prometheus.MustNewConstMetric(desc.success, prometheus.GaugeValue, success, labels...)
		ch <- prometheus.MustNewConstMetric(desc.results, prometheus.GaugeValue, q.count, labels...)
		ch <- prometheus.MustNewConstMetric(desc.errors, prometheus.CounterValue, q.errors, labels...)
		ch <- prometheus.MustNewConstMetric(desc.lastSuccess, prometheus.GaugeValue, q.lastSuccess, labels...)
	}
	d.pending = nil
	d.current = nil
}

// Only the Lua page selector is included; arbitrary query parameters could contain secrets.
func luaQuerySource(m *LuaMetric) string {
	source := strings.SplitN(m.Path, "?", 2)[0]
	if params, err := url.ParseQuery(m.Params); err == nil {
		if page := params.Get("page"); page != "" {
			source += "?page=" + page
		}
	}
	return source
}
