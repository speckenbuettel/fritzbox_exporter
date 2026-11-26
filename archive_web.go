package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	lua "github.com/sberk42/fritzbox_exporter/fritzbox_lua"
	"io"
	"net/http"
	"strings"
	"time"
)

func (a *archive) configureEventSource(fc *FritzboxCollector) error {
	a.source = *flagArchiveSource
	if a.source != "webui" && a.source != "soap" {
		return fmt.Errorf("archive-source must be webui or soap")
	}
	if a.source == "soap" {
		return nil
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: !fc.VerifyTls}
	a.webTransport = transport
	a.webSession = &lua.LuaSession{
		BaseURL: strings.TrimRight(*flagGatewayLuaURL, "/"), Username: fc.Username, Password: fc.Password, ApiVer: *flagSessionApi,
	}
	return nil
}
func (a *archive) pollWebEvents(ctx context.Context) {
	a.webSession.Client = http.Client{Timeout: *flagSOAPTimeout, Transport: budgetTransport{ctx, archiveWebTransport{a.webTransport}}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	body, err := a.webSession.LoadData(lua.LuaPage{Path: "data.lua", Params: "page=log&filter=all"})
	var records []archiveRecord
	if err == nil {
		records, err = a.parseWebEvents(body)
		if err != nil {
			a.webSession.SID = ""
		}
	}
	record := archiveRecord{Kind: "query", Backend: "archive", Key: "router-log", Source: "data.lua?page=log"}
	if err != nil {
		if ctx.Err() == context.Canceled {
			return
		}
		// Preserve the session on transport timeouts; LoadData handles expired sessions.
		record.Failed = true
		record.Reason = "request"
		record.Message = archiveSafe(err.Error())
		a.pollErrors.Add(1)
		archivePollErrors.Inc()
		a.enqueue(archiveJob{Records: []archiveRecord{record}})
		return
	}
	records = append(records, record)
	a.enqueue(archiveJob{Records: records, Poll: true})
}

type archiveWebTransport struct{ base http.RoundTripper }

func (t archiveWebTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(r)
	if err == nil {
		response.Body = &archiveBody{Reader: io.LimitReader(response.Body, 4*1024*1024), Closer: response.Body}
	}
	return response, err
}
func (a *archive) parseWebEvents(body []byte) ([]archiveRecord, error) {
	var doc struct {
		Data struct {
			Filter string          `json:"filter"`
			Log    json.RawMessage `json:"log"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &doc) != nil || len(doc.Data.Log) == 0 || string(doc.Data.Log) == "null" {
		return nil, fmt.Errorf("WebUI event list missing or invalid")
	}
	if doc.Data.Filter != "all" {
		return nil, fmt.Errorf("WebUI event list does not include all categories")
	}
	var entries []struct {
		Date    string `json:"date"`
		Time    string `json:"time"`
		Message string `json:"msg"`
	}
	if json.Unmarshal(doc.Data.Log, &entries) != nil {
		return nil, fmt.Errorf("invalid WebUI event rows")
	}
	records := make([]archiveRecord, 0, len(entries))
	seen := map[string]int{}
	for _, e := range entries {
		raw := e.Date + " " + e.Time
		stamp, err := time.ParseInLocation("02.01.06 15:04:05", raw, a.zone)
		if err != nil || e.Message == "" {
			return nil, fmt.Errorf("invalid WebUI event timestamp or message")
		}
		// Same key as the existing SOAP parser, avoiding duplicates when switching.
		line := raw + " " + e.Message
		seen[line]++
		records = append(records, archiveRecord{Kind: "router", Backend: "router", Source: "data.lua?page=log", Message: e.Message, RawTime: raw, EventTime: stamp.UTC().Format(time.RFC3339), Key: fmt.Sprintf("%s|%d", line, seen[line])})
	}
	return records, nil
}
