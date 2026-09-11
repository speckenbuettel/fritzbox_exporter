package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
	_ "time/tzdata"
)

type archiveTransport struct{}
type archiveBody struct {
	io.Reader
	io.Closer
}

func (archiveTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err == nil {
		resp.Body = &archiveBody{io.LimitReader(resp.Body, 4*1024*1024), resp.Body}
	}
	return resp, err
}

func (a *archive) poll(ctx context.Context, fc *FritzboxCollector) {
	ticker := time.NewTicker(*flagArchiveInterval)
	defer ticker.Stop()
	for {
		a.pollOnce(ctx, fc)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (a *archive) pollOnce(ctx context.Context, fc *FritzboxCollector) {
	if ctx.Err() != nil {
		return
	}
	callCtx, cancel := context.WithTimeout(ctx, *flagSOAPTimeout)
	defer cancel()
	fc.Lock()
	root := fc.Root
	fc.Unlock()
	var body string
	var err error
	if root == nil {
		err = fmt.Errorf("SOAP service discovery not ready")
	} else {
		service := root.Services["urn:dslforum-org:service:DeviceInfo:1"]
		if service == nil || service.Actions["GetDeviceLog"] == nil {
			err = fmt.Errorf("GetDeviceLog unavailable")
		} else {
			data, e := service.Actions["GetDeviceLog"].CallWithClient(nil, &http.Client{Timeout: *flagSOAPTimeout, Transport: budgetTransport{callCtx, archiveTransport{}}})
			err = e
			if err == nil {
				var ok bool
				body, ok = data["DeviceLog"].(string)
				if !ok {
					err = fmt.Errorf("DeviceLog is missing or not text")
				}
			}
		}
	}
	record := archiveRecord{Kind: "query", Backend: "archive", Key: "router-log", Source: "DeviceInfo/GetDeviceLog"}
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		record.Failed = true
		record.Reason = "request"
		record.Message = err.Error()
		a.pollErrors.Add(1)
		archivePollErrors.Inc()
		a.enqueue(archiveJob{Records: []archiveRecord{record}})
		return
	}
	records := a.parseRouterLog(body)
	records = append(records, record)
	a.enqueue(archiveJob{Records: records, Poll: true})
}
