package main

import (
	"fmt"
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
