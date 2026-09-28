package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	upnp "github.com/sberk42/fritzbox_exporter/fritzbox_upnp"
	"github.com/sirupsen/logrus"
)

// Recheck only after enumeration, without changing its cache or index limits.
// The existing client enforces both request timeout and collection budget.
func (fc *FritzboxCollector) logHostCountCheck(m *Metric, index, count int, age int64, started time.Time) {
	aa := m.ActionArgument
	fields := logrus.Fields{
		"gateway": fc.Gateway, "service": m.Service, "action": m.Action,
		"first_invalid_index": index, "count": count, "count_age_seconds": age,
		"cache_ttl_seconds": m.CacheEntryTTL, "provider_action": aa.ProviderAction,
		"enumeration_duration_ms": time.Since(started).Milliseconds(),
	}
	result, err := fc.readUncachedHostCount(m)
	if err != nil {
		fields["recheck_error"] = err.Error()
	} else if value, ok := result[aa.Value]; !ok {
		fields["recheck_error"] = "count result missing: " + aa.Value
	} else {
		fields["fresh_count"] = value
	}
	logrus.WithFields(fields).Warn("host count diagnostic after invalid index (cache unchanged)")
}

func (fc *FritzboxCollector) readUncachedHostCount(m *Metric) (upnp.Result, error) {
	if fc.collectionContext != nil && fc.collectionContext.Err() != nil {
		return nil, fc.collectionContext.Err()
	}
	if fc.Root == nil {
		return nil, fmt.Errorf("services unavailable")
	}
	service, ok := fc.Root.Services[m.Service]
	if !ok {
		return nil, fmt.Errorf("service %s not found", m.Service)
	}
	action, ok := service.Actions[m.ActionArgument.ProviderAction]
	if !ok {
		return nil, fmt.Errorf("action %s not found", m.ActionArgument.ProviderAction)
	}
	return action.CallWithClient(nil, fc.soapClient)
}

// Capture freshness before the cache lookup: timestamps alone cannot distinguish
// a just-cached row from a network response in the same second.
type hostReadDiagnostic struct {
	Index  int
	Source string
	Age    int64
	Result upnp.Result
}

func hostReadBeforeCall(m *Metric, arg *upnp.ActionArgument, now int64) hostReadDiagnostic {
	d := hostReadDiagnostic{Source: "network", Age: -1}
	if m.Action != "GetGenericHostEntry" {
		return d
	}
	key := fmt.Sprintf("%s|%s|%s|%v", m.Service, m.Action, arg.Name, arg.Value)
	if entry := upnpCache[key]; entry != nil && entry.Result != nil {
		d.Age = now - entry.Timestamp
		if d.Age <= m.CacheEntryTTL {
			d.Source = "cache"
		}
	}
	return d
}
func (fc *FritzboxCollector) logDuplicateHost(m *Metric, result upnp.Result, index, count int, countAge int64, read hostReadDiagnostic, seen map[string]hostReadDiagnostic) {
	labels := make([]string, len(m.PromDesc.VarLabels))
	for i, name := range m.PromDesc.VarLabels {
		if name == "gateway" {
			labels[i] = fc.Gateway
			continue
		}
		if value, ok := result[name]; ok {
			labels[i] = fmt.Sprint(value)
		}
		if name == "HostName" || name == "MACAddress" {
			labels[i] = strings.ToLower(labels[i])
		}
	}
	// Structured encoding avoids ambiguous concatenation. Values stay in memory;
	// no host names, addresses or MAC addresses are written to the log.
	encoded, _ := json.Marshal(labels)
	key := string(encoded)
	read.Index = index
	read.Result = make(upnp.Result, len(result))
	for k, v := range result {
		read.Result[k] = v
	}
	if first, ok := seen[key]; ok {
		differing := hostResultDifferences(first.Result, result)
		missing := make([]string, 0)
		empty := make([]string, 0)
		for _, name := range m.PromDesc.VarLabels {
			if name == "gateway" {
				continue
			}
			a, aok := first.Result[name]
			b, bok := result[name]
			if !aok || !bok {
				missing = append(missing, name)
			}
			if (aok && fmt.Sprint(a) == "") || (bok && fmt.Sprint(b) == "") {
				empty = append(empty, name)
			}
		}
		fc.diagnostics.fail("duplicate", fmt.Errorf("duplicate host metric labels: first_index=%d first_source=%s first_cache_age_seconds=%d duplicate_index=%d duplicate_source=%s duplicate_cache_age_seconds=%d count=%d count_age_seconds=%d cache_ttl_seconds=%d", first.Index, first.Source, first.Age, read.Index, read.Source, read.Age, count, countAge, m.CacheEntryTTL))
		logrus.WithFields(logrus.Fields{
			"gateway": fc.Gateway, "metric": m.PromDesc.FqName, "action": m.Action,
			"soap_results_equal":    len(differing) == 0,
			"soap_differing_fields": differing,
			"missing_label_fields":  missing,
			"empty_label_fields":    empty,
			"first_index":           first.Index, "first_source": first.Source, "first_cache_age_seconds": first.Age,
			"duplicate_index": read.Index, "duplicate_source": read.Source, "duplicate_cache_age_seconds": read.Age,
			"count": count, "count_age_seconds": countAge, "cache_ttl_seconds": m.CacheEntryTTL,
		}).Warn("duplicate host metric labels during enumeration")
	} else {
		seen[key] = read
	}
}

// Compare decoded SOAP fields before label normalization. Only field names are
// returned; values remain in memory for this enumeration and are never logged.
func hostResultDifferences(a, b upnp.Result) []string {
	names := make(map[string]bool)
	for k := range a {
		names[k] = true
	}
	for k := range b {
		names[k] = true
	}
	different := make([]string, 0)
	for k := range names {
		av, aok := a[k]
		bv, bok := b[k]
		if aok != bok || !reflect.DeepEqual(av, bv) {
			different = append(different, k)
		}
	}
	sort.Strings(different)
	return different
}
