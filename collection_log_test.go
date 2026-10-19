package main

import (
	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"sync"
	"testing"
)

func TestRepeatLogSummaries(t *testing.T) {
	logger, hook := logtest.NewNullLogger()
	entry := logrus.NewEntry(logger)
	r := &repeatLogLimiter{pending: make(map[string]*repeatedLog)}
	r.log(entry, logrus.WarnLevel, "timeout A")
	r.log(entry, logrus.WarnLevel, "timeout A")
	r.log(entry, logrus.ErrorLevel, "timeout B")
	if len(hook.AllEntries()) != 2 {
		t.Fatal("first errors must be immediate")
	}
	r.flush()
	events := hook.AllEntries()
	if len(events) != 3 || events[2].Data["suppressed_repeats"] != 1 {
		t.Fatalf("%+v", events)
	}
	r.flush()
	if len(hook.AllEntries()) != 3 {
		t.Fatal("summary repeated")
	}
	r.log(entry, logrus.WarnLevel, "timeout A")
	if len(hook.AllEntries()) != 4 {
		t.Fatal("new window suppressed")
	}
}
func TestRepeatLogConcurrencyAndBound(t *testing.T) {
	logger, hook := logtest.NewNullLogger()
	entry := logrus.NewEntry(logger)
	r := &repeatLogLimiter{pending: make(map[string]*repeatedLog)}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r.log(entry, logrus.WarnLevel, "timeout") }()
	}
	wg.Wait()
	r.flush()
	events := hook.AllEntries()
	if len(events) != 2 || events[1].Data["suppressed_repeats"] != 99 {
		t.Fatalf("%+v", events)
	}
	for i := 0; i < 1100; i++ {
		r.log(entry.WithField("gateway", i), logrus.WarnLevel, "timeout")
	}
	if len(r.pending) != 1024 {
		t.Fatal(len(r.pending))
	}
}

func TestRepeatLogConflictGrouping(t *testing.T) {
	logger, hook := logtest.NewNullLogger()
	r := &repeatLogLimiter{pending: make(map[string]*repeatedLog)}
	for _, index := range []int{22, 23} {
		r.log(logrus.NewEntry(logger).WithFields(logrus.Fields{"gateway": "a", "index": index, "xml_differing_fields": []string{"Active"}}), logrus.WarnLevel, "duplicate")
	}
	r.log(logrus.NewEntry(logger).WithFields(logrus.Fields{"gateway": "b", "xml_differing_fields": []string{"Active"}}), logrus.WarnLevel, "duplicate")
	r.log(logrus.NewEntry(logger).WithFields(logrus.Fields{"gateway": "a", "xml_differing_fields": []string{"HostName"}}), logrus.WarnLevel, "duplicate")
	if len(hook.AllEntries()) != 3 {
		t.Fatal("unrelated conflicts grouped or positions not grouped")
	}
	r.flush()
	if len(hook.AllEntries()) != 4 || hook.LastEntry().Data["suppressed_repeats"] != 1 {
		t.Fatal("missing summary")
	}
}
