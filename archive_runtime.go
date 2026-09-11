package main

import (
	"context"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

func startEventArchive(fc *FritzboxCollector) (func(), error) {
	if *flagArchiveFile == "" {
		return func() {}, nil
	}
	if *flagArchiveInterval < 10*time.Second {
		return nil, fmt.Errorf("ARCHIVE_INTERVAL must be at least 10s")
	}
	a, err := openArchive(*flagArchiveFile, fc.Gateway, *flagArchiveToken, *flagArchiveTimezone, *flagArchiveRetention, *flagArchiveMaxRows, *flagArchiveMaxMB)
	if err != nil {
		return nil, err
	}
	eventArchive = a
	a.run()
	prometheus.MustRegister(archiveWriteErrors, archiveDropped, archivePollErrors, archiveLastPoll)
	http.HandleFunc("/events", a.page)
	http.HandleFunc("/api/events", a.events)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); a.poll(ctx, fc) }()
	logrus.Info("event archive enabled; viewer at /events")
	return func() { cancel(); wg.Wait(); a.close() }, nil
}
func serveExporter() {
	server := &http.Server{Addr: *flagAddr, Handler: http.DefaultServeMux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logrus.Error(err)
	}
	stop()
	<-finished
}
