package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCollectionBudgetCancelsResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	client := http.Client{Transport: budgetTransport{ctx, http.DefaultTransport}}
	start := time.Now()
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("body not cancelled promptly: %v", err)
	}
}

func TestRequestTimeoutPreservedInsideBudget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client := http.Client{Timeout: 40 * time.Millisecond, Transport: budgetTransport{ctx, http.DefaultTransport}}
	start := time.Now()
	_, err := client.Get(server.URL)
	if err == nil || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("request timeout lost: %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("request exhausted entire collection budget")
	}
}

func TestOverlappingScrapeRejectedAndGateRecovers(t *testing.T) {
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	handler := rejectOverlappingScrapes(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/first" {
			close(entered)
			<-release
		}
		w.WriteHeader(200)
	}))
	go func() {
		defer close(done)
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/first", nil))
	}()
	<-entered
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, httptest.NewRequest("GET", "/second", nil))
	close(release)
	<-done
	if out.Code != 503 {
		t.Fatalf("overlap status %d", out.Code)
	}
	out = httptest.NewRecorder()
	handler.ServeHTTP(out, httptest.NewRequest("GET", "/third", nil))
	if out.Code != 200 {
		t.Fatalf("gate did not recover: %d", out.Code)
	}
}
