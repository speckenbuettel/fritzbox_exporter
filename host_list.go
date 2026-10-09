package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"github.com/namsral/flag"
	"github.com/prometheus/client_golang/prometheus"
	upnp "github.com/sberk42/fritzbox_exporter/fritzbox_upnp"
	"github.com/sirupsen/logrus"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var flagHostList = flag.Bool("host-list-xml", false, "Experimental: replace indexed Hosts reads with a cached XML host list")

type hostListCache struct {
	rows []upnp.Result
	at   time.Time
}
type hostXMLField struct {
	XMLName xml.Name
	Value   string `xml:",chardata"`
}
type hostXMLItem struct {
	Fields []hostXMLField `xml:",any"`
}

func parseHostList(data []byte) ([]upnp.Result, error) {
	var doc struct {
		XMLName xml.Name
		Items   []hostXMLItem `xml:"Item"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil || doc.XMLName.Local != "List" {
		return nil, fmt.Errorf("invalid host list XML")
	}
	rows := make([]upnp.Result, 0, len(doc.Items))
	for _, item := range doc.Items {
		row := upnp.Result{}
		for _, f := range item.Fields {
			if _, ok := row[f.XMLName.Local]; ok {
				return nil, fmt.Errorf("duplicate XML field")
			}
			row[f.XMLName.Local] = f.Value
		}
		a, ok := row["Active"].(string)
		if !ok || (a != "0" && a != "1") {
			return nil, fmt.Errorf("missing or invalid Active field")
		}
		row["Active"] = a == "1"
		rows = append(rows, row)
	}
	return rows, nil
}
func hostListURL(base, path string) (string, error) {
	b, e := url.Parse(base)
	if e != nil {
		return "", fmt.Errorf("invalid router URL")
	}
	p, e := url.Parse(path)
	if e != nil || path == "" {
		return "", fmt.Errorf("invalid host list path")
	}
	u := b.ResolveReference(p)
	if u.Scheme != b.Scheme || u.Host != b.Host || u.User != nil || u.Fragment != "" {
		return "", fmt.Errorf("host list URL must use router origin")
	}
	return u.String(), nil
}
func (fc *FritzboxCollector) readHostList(m *Metric) ([]upnp.Result, error) {
	if fc.hostList.rows != nil && time.Since(fc.hostList.at) <= time.Duration(m.CacheEntryTTL)*time.Second {
		return fc.hostList.rows, nil
	}
	svc := fc.Root.Services[m.Service]
	if svc == nil || svc.Actions["X_AVM-DE_GetHostListPath"] == nil {
		return nil, fmt.Errorf("X_AVM-DE_GetHostListPath unsupported")
	}
	r, e := svc.Actions["X_AVM-DE_GetHostListPath"].CallWithClient(nil, fc.soapClient)
	if e != nil {
		return nil, fmt.Errorf("host list path request failed")
	}
	path, ok := r["X_AVM-DE_HostListPath"].(string)
	if !ok {
		return nil, fmt.Errorf("host list path missing")
	}
	u, e := hostListURL(fc.Root.BaseURL, path)
	if e != nil {
		return nil, e
	}
	req, e := http.NewRequestWithContext(fc.collectionContext, "GET", u, nil)
	if e != nil {
		return nil, fmt.Errorf("invalid host list request")
	}
	client := *fc.soapClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, e := client.Do(req)
	if e != nil {
		return nil, fmt.Errorf("host list download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("host list HTTP status %d", resp.StatusCode)
	}
	const limit = 4 * 1024 * 1024
	data, e := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if e != nil || len(data) > limit {
		return nil, fmt.Errorf("host list read failed or exceeds 4 MiB")
	}
	rows, e := parseHostList(bytes.TrimSpace(data))
	if e != nil {
		return nil, e
	}
	fc.hostList = hostListCache{rows: rows, at: time.Now()}
	return rows, nil
}
func (fc *FritzboxCollector) collectHostList(ch chan<- prometheus.Metric, m *Metric, dup map[string]bool) {
	rows, e := fc.readHostList(m)
	if e == nil {
		// Validate before emitting anything; unavailable SOAP fields are not invented.
		for _, r := range rows {
			for _, key := range append(append([]string{}, m.PromDesc.VarLabels...), m.Result) {
				if key == "gateway" {
					continue
				}
				if _, ok := r[key]; !ok {
					e = fmt.Errorf("XML host list lacks configured field %s", key)
					break
				}
			}
			if e != nil {
				break
			}
		}
	}
	if e != nil {
		collectErrors.Inc()
		fc.diagnostics.fail("request", e)
		logrus.Warn(e)
		return
	}
	seen := map[string]hostReadDiagnostic{}
	for i, r := range rows {
		if fc.logDuplicateHost(m, r, i, len(rows), 0, hostReadDiagnostic{Source: "xml"}, seen) {
			continue
		}
		fc.reportMetric(ch, m, r, dup)
	}
}
func useHostList(m *Metric) bool {
	return *flagHostList && strings.HasPrefix(m.Service, "urn:dslforum-org:service:Hosts:") && m.Action == "GetGenericHostEntry" && m.ActionArgument != nil && m.ActionArgument.IsIndex
}
