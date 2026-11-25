package main

import (
	"context"
	"fmt"
	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestArchiveQueryFailureDiagnostic(t *testing.T) {
	a := testArchive(t)
	logger := logrus.StandardLogger()
	previous := logger.ReplaceHooks(make(logrus.LevelHooks))
	defer logger.ReplaceHooks(previous)
	hook := logtest.NewGlobal()
	a.db.Close()
	request := httptest.NewRequest("GET", "/api/events?q=private-search", nil)
	request.Header.Set("Authorization", "Bearer "+a.token)
	response := httptest.NewRecorder()
	a.events(response, request)
	if response.Code != 503 || response.Body.String() != "archive unavailable\n" {
		t.Fatal(response)
	}
	event := hook.LastEntry()
	if event == nil || event.Data["stage"] != "query" || event.Data["error"] != "sql: database is closed" {
		t.Fatalf("%+v", event)
	}
	line, _ := event.String()
	if strings.Contains(line, "private-search") || strings.Contains(line, a.token) {
		t.Fatal("request data leaked")
	}
}
func TestArchiveScanDiagnosticOmitsValues(t *testing.T) {
	a := testArchive(t)
	logger := logrus.StandardLogger()
	previous := logger.ReplaceHooks(make(logrus.LevelHooks))
	defer logger.ReplaceHooks(previous)
	hook := logtest.NewGlobal()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.logDatabaseFailure(ctx, "scan", fmt.Errorf("invalid value private-host"), time.Now(), a.db.Stats(), 200, 0, 3)
	event := hook.LastEntry()
	line, _ := event.String()
	if strings.Contains(line, "private-host") || event.Data["context_error"] != "context canceled" || event.Data["rows_read"] != 3 {
		t.Fatal(line)
	}
}
