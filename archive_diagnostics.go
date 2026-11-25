package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/sirupsen/logrus"
	"modernc.org/sqlite"
	"regexp"
	"strings"
	"time"
)

// Do not log request URLs, filter values, tokens or row values. In particular,
// database/sql scan errors can embed the complete value that failed conversion.
var archiveScanColumn = regexp.MustCompile(`column index [0-9]+, name "[a-z_]+"`)

func (a *archive) logDatabaseFailure(ctx context.Context, stage string, err error, started time.Time, before sql.DBStats, limit, offset, rows int) {
	after := a.db.Stats()
	detail := archiveSafe(err.Error())
	if stage == "scan" {
		detail = "row conversion failed (value omitted)"
		if column := archiveScanColumn.FindString(err.Error()); column != "" {
			detail += ": " + column
		}
		if strings.Contains(err.Error(), "converting NULL") {
			detail += "; NULL conversion"
		}
		if strings.Contains(err.Error(), "invalid syntax") {
			detail += "; invalid syntax"
		}
		if strings.Contains(err.Error(), "unsupported Scan") {
			detail += "; unsupported Scan"
		}
	}
	fields := logrus.Fields{
		"gateway": a.gateway, "stage": stage, "error": detail,
		"error_type": fmt.Sprintf("%T", err), "duration_ms": time.Since(started).Milliseconds(),
		"limit": limit, "offset": offset, "rows_read": rows,
		"db_open": after.OpenConnections, "db_in_use": after.InUse, "db_idle": after.Idle,
		"db_wait_count_delta": after.WaitCount - before.WaitCount,
		"db_wait_ms_delta":    (after.WaitDuration - before.WaitDuration).Milliseconds(),
	}
	if ctx != nil && ctx.Err() != nil {
		fields["context_error"] = ctx.Err().Error()
	}
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		fields["sqlite_code"] = sqliteErr.Code()
	}
	logrus.WithFields(fields).Warn("event archive database operation failed")
}
