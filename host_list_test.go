package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	upnp "github.com/sberk42/fritzbox_exporter/fritzbox_upnp"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestHostListParse(t *testing.T) {
	rows, e := parseHostList([]byte(`<List><Item><Active>1</Active><HostName>A &amp; B</HostName><MACAddress></MACAddress></Item><Item><Active>0</Active></Item></List>`))
	if e != nil || len(rows) != 2 || rows[0]["Active"] != true || rows[1]["Active"] != false || rows[0]["HostName"] != "A & B" {
		t.Fatalf("%v %v", rows, e)
	}
	for _, s := range []string{`<html/>`, `<List><Item/></List>`, `<List><Item><Active>2</Active></Item></List>`, `<List><Item><Active>1</Active><Active>0</Active></Item></List>`} {
		if _, e := parseHostList([]byte(s)); e == nil {
			t.Fatal(s)
		}
	}
	rows, e = parseHostList([]byte(`<List/>`))
	if e != nil || rows == nil || len(rows) != 0 {
		t.Fatal("empty list")
	}
}
func TestHostListURL(t *testing.T) {
	for _, s := range []string{"http://other/list", "//other/list", "http://u:p@router:49000/list", "https://router:49000/list", ""} {
		if _, e := hostListURL("http://router:49000", s); e == nil {
			t.Fatal(s)
		}
	}
	u, e := hostListURL("http://router:49000", "/devicehostlist.lua?token=test")
	if e != nil || u != "http://router:49000/devicehostlist.lua?token=test" {
		t.Fatal(u, e)
	}
}

func TestHostListCollectionAndCache(t *testing.T) {
	downloads, paths := 0, 0
	const service = "urn:dslforum-org:service:Hosts:1"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/igddesc.xml", "/tr64desc.xml":
			fmt.Fprintf(w, `<root><device><serviceList><service><serviceType>%s</serviceType><controlURL>/hosts</controlURL><SCPDURL>/scpd</SCPDURL></service></serviceList></device></root>`, service)
		case "/scpd":
			fmt.Fprint(w, `<scpd><actionList><action><name>X_AVM-DE_GetHostListPath</name><argumentList><argument><name>NewX_AVM-DE_HostListPath</name><direction>out</direction><relatedStateVariable>X_AVM-DE_HostListPath</relatedStateVariable></argument></argumentList></action></actionList><serviceStateTable><stateVariable><name>X_AVM-DE_HostListPath</name><dataType>string</dataType></stateVariable></serviceStateTable></scpd>`)
		case "/hosts":
			paths++
			fmt.Fprintf(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:X_AVM-DE_GetHostListPathResponse xmlns:u="%s"><NewX_AVM-DE_HostListPath>/list?token=secret</NewX_AVM-DE_HostListPath></u:X_AVM-DE_GetHostListPathResponse></s:Body></s:Envelope>`, service)
		case "/list":
			downloads++
			fmt.Fprint(w, `<List><Item><Index>22</Index><Active>1</Active><HostName>host</HostName></Item><Item><Index>23</Index><Active>1</Active><HostName>host</HostName></Item></List>`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	defer server.Close()
	root, e := upnp.LoadServices(server.URL, "", "", true)
	if e != nil {
		t.Fatal(e)
	}
	fc := &FritzboxCollector{Root: root, Gateway: "test", collectionContext: context.Background(), soapClient: server.Client()}
	m := &Metric{Service: service, Action: "GetGenericHostEntry", Result: "Active", CacheEntryTTL: 60, MetricType: prometheus.GaugeValue, PromDesc: JSONPromDesc{FqName: "gateway_hosts", VarLabels: []string{"gateway", "HostName"}}, Desc: prometheus.NewDesc("gateway_hosts", "hosts", []string{"gateway", "hostname"}, nil)}
	for i := 0; i < 2; i++ {
		fc.diagnostics.begin("soap", 0, "hosts", "gateway_hosts")
		ch := make(chan prometheus.Metric, 4)
		fc.collectHostList(ch, m, map[string]bool{})
		if len(ch) != 1 || fc.diagnostics.current.reason != "" {
			t.Fatal("collection failed", fc.diagnostics.current.reason, len(ch))
		}
	}
	if downloads != 1 || paths != 1 {
		t.Fatal(downloads, paths)
	}
	m.PromDesc.VarLabels = append(m.PromDesc.VarLabels, "AddressSource")
	fc.diagnostics.begin("soap", 0, "hosts", "gateway_hosts")
	ch := make(chan prometheus.Metric, 4)
	fc.collectHostList(ch, m, map[string]bool{})
	if len(ch) != 0 || fc.diagnostics.current.reason == "" {
		t.Fatal("missing field accepted")
	}
	fc.hostList.at = time.Now().Add(-2 * time.Minute)
	if _, e = fc.readHostList(m); e != nil || downloads != 2 {
		t.Fatal(e, downloads)
	}
}

func TestHostListErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{context.Canceled, "canceled"},
		{&url.Error{Op: "Get", URL: "http://router/?token=secret", Err: context.DeadlineExceeded}, "timeout"},
		{errors.New("secret URL"), "transport error"},
	} {
		if got := hostListErrorKind(tc.err); got != tc.want {
			t.Fatal(got)
		}
	}
}

type failedHostReader struct{}

func (failedHostReader) Read(p []byte) (int, error) {
	return copy(p, "partial"), context.DeadlineExceeded
}
func TestHostListReadFailures(t *testing.T) {
	_, err := readHostListBody(failedHostReader{})
	if err == nil || err.Error() != "host list read failed (timeout)" {
		t.Fatal(err)
	}
	_, err = readHostListBody(strings.NewReader(strings.Repeat("x", 4*1024*1024+1)))
	if err == nil || err.Error() != "host list exceeds 4 MiB" {
		t.Fatal(err)
	}
	data, err := readHostListBody(io.LimitReader(strings.NewReader("ok"), 2))
	if err != nil || string(data) != "ok" {
		t.Fatal(err)
	}
}
