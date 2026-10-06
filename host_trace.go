package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/namsral/flag"
	upnp "github.com/sberk42/fritzbox_exporter/fritzbox_upnp"
	"github.com/sirupsen/logrus"
	"net/http"
	"strings"
	"time"
)

var flagHostTrace = flag.Duration("host-diagnostic-duration", 0, "Opt-in host diagnostics from process start; 0 disables, maximum 72h")
var hostTraceStart = time.Now()

type hostTraceState struct {
	key           [32]byte
	initialized   bool
	last, summary time.Time
}
type hostTrace struct {
	fields  logrus.Fields
	rows    []string
	started time.Time
	seen    map[string]string
	anomaly bool
	total   int
}

// No raw identifiers or error strings enter the trace. The random HMAC key is
// process-local and is never persisted; identities are not cross-run linkable.
func (s *hostTraceState) identity(row upnp.Result) string {
	b, _ := json.Marshal([]interface{}{row["MACAddress"], row["IPAddress"], row["HostName"]})
	h := hmac.New(sha256.New, s.key[:])
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil)[:12])
}
func traceValue(r upnp.Result, key string) string {
	v, ok := r[key]
	if !ok {
		return "missing"
	}
	return fmt.Sprint(v)
}
func (fc *FritzboxCollector) traceCall(m *Metric, action string, arg *upnp.ActionArgument) (upnp.Result, string) {
	ctx := fc.collectionContext
	if ctx == nil || ctx.Err() != nil {
		return nil, "budget_exhausted"
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) < 2*time.Second {
		return nil, "budget_insufficient"
	}
	if fc.Root == nil {
		return nil, "unavailable"
	}
	svc := fc.Root.Services[m.Service]
	if svc == nil || svc.Actions[action] == nil {
		return nil, "unsupported"
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	result, err := svc.Actions[action].CallWithClient(arg, &http.Client{Timeout: time.Second, Transport: budgetTransport{callCtx, http.DefaultTransport}})
	if err != nil {
		if strings.Contains(err.Error(), "UPnPError 713 (") {
			return nil, "invalid_index_713"
		}
		return nil, "request_failed"
	}
	return result, "ok"
}
func (fc *FritzboxCollector) beginHostTrace(m *Metric, count int, age int64) *hostTrace {
	now := time.Now()
	if m.Action != "GetGenericHostEntry" || *flagHostTrace <= 0 || now.Sub(hostTraceStart) >= *flagHostTrace || now.Sub(fc.hostTrace.last) < time.Minute {
		return nil
	}
	// Observe fresh enumerations only; do not bypass the production cache.
	a := &upnp.ActionArgument{Name: m.ActionArgument.Name, Value: 0}
	if hostReadBeforeCall(m, a, now.Unix()).Source == "cache" {
		return nil
	}
	s := &fc.hostTrace
	if !s.initialized {
		if _, err := rand.Read(s.key[:]); err != nil {
			return nil
		}
		s.initialized = true
	}
	s.last = now
	t := &hostTrace{fields: logrus.Fields{"count_used": count, "count_age_seconds": age}, started: now, seen: map[string]string{}}
	r, status := fc.traceCall(m, m.ActionArgument.ProviderAction, nil)
	t.fields["count_before_status"] = status
	if status == "ok" {
		t.fields["count_before"] = traceValue(r, m.ActionArgument.Value)
	}
	r, status = fc.traceCall(m, "X_AVM-DE_GetChangeCounter", nil)
	t.fields["change_before_status"] = status
	if status == "ok" {
		t.fields["change_before"] = traceValue(r, "X_AVM-DE_ChangeCounter")
	}
	return t
}
func (fc *FritzboxCollector) traceHostRow(t *hostTrace, index int, row upnp.Result, source string) {
	if t == nil {
		return
	}
	t.total++
	id := fc.hostTrace.identity(row)
	active := traceValue(row, "Active")
	switch active {
	case "true", "1":
		active = "1"
	case "false", "0":
		active = "0"
	default:
		active = "other"
	}
	iface := traceValue(row, "InterfaceType")
	switch iface {
	case "", "Ethernet", "802.11", "USB", "HomePNA", "HomePlug":
	default:
		iface = "other"
	}
	if _, ok := t.seen[id]; ok {
		t.anomaly = true
	}
	t.seen[id] = active
	if len(t.rows) < 64 {
		t.rows = append(t.rows, fmt.Sprintf("%d:%s:%s:%s:%s", index, id, active, iface, source))
	}
}
func (fc *FritzboxCollector) endHostTrace(t *hostTrace, m *Metric, invalid int) {
	if t == nil {
		return
	}
	r, status := fc.traceCall(m, m.ActionArgument.ProviderAction, nil)
	t.fields["count_after_status"] = status
	if status == "ok" {
		t.fields["count_after"] = traceValue(r, m.ActionArgument.Value)
	}
	r, status = fc.traceCall(m, "X_AVM-DE_GetChangeCounter", nil)
	t.fields["change_after_status"] = status
	if status == "ok" {
		t.fields["change_after"] = traceValue(r, "X_AVM-DE_ChangeCounter")
	}
	if invalid >= 0 {
		t.anomaly = true
		r, status = fc.traceCall(m, m.Action, &upnp.ActionArgument{Name: m.ActionArgument.Name, Value: invalid})
		t.fields["retry_status"] = status
		if status == "ok" {
			t.fields["retry_host"] = fc.hostTrace.identity(r)
		}
	}
	t.fields["invalid_index"] = invalid
	t.fields["duration_ms"] = time.Since(t.started).Milliseconds()
	t.fields["rows_read"] = t.total
	t.fields["collection_canceled"] = fc.collectionContext.Err() != nil
	t.fields["rows"] = t.rows
	t.fields["rows_truncated"] = t.total > len(t.rows)
	if t.anomaly || time.Since(fc.hostTrace.summary) >= 5*time.Minute {
		logrus.WithFields(t.fields).Info("bounded host diagnostic")
		fc.hostTrace.summary = time.Now()
	}
}
