package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/namsral/flag"
	"github.com/prometheus/client_golang/prometheus"
	lua "github.com/sberk42/fritzbox_exporter/fritzbox_lua"
	"github.com/sirupsen/logrus"
)

var flagAPIMetricsFile = flag.String("api-metrics-file", "", "Optional API v0 metric definitions, e.g. metrics-api.json; empty disables API collection")
var flagAPIURL = flag.String("gateway-apiurl", "", "FRITZ!Box UI origin for API v0; defaults to gateway-luaurl")
var flagAPITimeout = flag.Duration("api-timeout", 10*time.Second, "Timeout per API or login HTTP request")

// APICollector is independent of SOAP service discovery and legacy Lua collection.
// Its session and cache are protected against concurrent Prometheus scrapes.
type APICollector struct {
	diagnostics queryDiagnostics
	mu          sync.Mutex
	session     *lua.LuaSession
	metrics     []*LuaMetric
	renames     []lua.LabelRename
	cache       map[string]apiCacheEntry
	errors      prometheus.Counter
}
type apiCacheEntry struct {
	data    map[string]interface{}
	expires time.Time
}

func newAPICollector(filename, origin, username, password, loginVersion string, verifyTLS bool, timeout time.Duration) (*APICollector, error) {
	if timeout <= 0 {
		return nil, fmt.Errorf("api-timeout must be positive")
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("gateway-apiurl must be an HTTP(S) origin")
	}
	b, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var config LuaMetricsFile
	if err = json.Unmarshal(b, &config); err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: !verifyTLS} // follows existing verifyTls option
	c := &APICollector{
		metrics: config.Metrics, cache: make(map[string]apiCacheEntry),
		errors:  prometheus.NewCounter(prometheus.CounterOpts{Name: "fritzbox_exporter_api_collect_errors_total", Help: "Number of API v0 collection errors."}),
		session: &lua.LuaSession{BaseURL: strings.TrimRight(origin, "/"), Username: username, Password: password, ApiVer: loginVersion, Client: http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}},
	}
	if loginVersion != "v1" && loginVersion != "v2" {
		return nil, fmt.Errorf("unsupported sessionapi")
	}
	for _, r := range config.LabelRenames {
		re, err := regexp.Compile(r.MatchRegex)
		if err != nil {
			return nil, err
		}
		c.renames = append(c.renames, lua.LabelRename{Pattern: *re, Name: r.RenameLabel})
	}
	for _, m := range c.metrics {
		if m == nil {
			return nil, fmt.Errorf("null API metric")
		}
		if _, err := apiEndpoint(m.Path, m.Params); err != nil {
			return nil, err
		}
		if m.ResultKey == "" || m.PromDesc.FqName == "" {
			return nil, fmt.Errorf("API metric requires resultKey and promDesc.fqName")
		}
		switch m.PromType {
		case "GaugeValue", "CounterValue", "UntypedValue":
		default:
			return nil, fmt.Errorf("invalid API promType %q", m.PromType)
		}
		labels := make([]string, len(m.PromDesc.VarLabels))
		for i, l := range m.PromDesc.VarLabels {
			labels[i] = strings.ToLower(l)
		}
		m.Desc = prometheus.NewDesc(m.PromDesc.FqName, m.PromDesc.Help, labels, m.PromDesc.FixedLabels)
		m.MetricType = getValueType(m.PromType)
		m.LuaMetricDef = lua.LuaMetricValueDefinition{Path: m.ResultPath, Key: m.ResultKey, OkValue: m.OkValue, Labels: m.PromDesc.VarLabels}
		if m.CacheEntryTTL < minCacheTTL {
			m.CacheEntryTTL = minCacheTTL
		}
	}
	return c, nil
}

// Endpoints are relative to /api/v0/. No arbitrary URLs or modifying methods.
func apiEndpoint(path, params string) (string, error) {
	u, err := url.Parse(path)
	if err != nil || u.IsAbs() || u.Host != "" || strings.HasPrefix(path, "/") || u.RawQuery != "" || u.Fragment != "" || path == "" || strings.Contains(u.Path, "\\") {
		return "", fmt.Errorf("invalid relative API path %q", path)
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == ".." || part == "." {
			return "", fmt.Errorf("invalid API path")
		}
	}
	q, err := url.ParseQuery(params)
	if err != nil {
		return "", fmt.Errorf("invalid API params")
	}
	if q.Has("sid") {
		return "", fmt.Errorf("API SID belongs in the authentication header")
	}
	endpoint := "/api/v0/" + u.EscapedPath()
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}
	return endpoint, nil
}

func (c *APICollector) load(endpoint string) (map[string]interface{}, error) {
	for attempt := 0; attempt < 2; attempt++ {
		if c.session.SID == "" {
			if err := c.session.Login(); err != nil {
				return nil, fmt.Errorf("API login failed")
			}
		}
		req, err := http.NewRequest(http.MethodGet, c.session.BaseURL+endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "AVM-SID "+c.session.SID)
		req.Header.Set("Client-Name", "WebGUI")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		resp, err := c.session.Client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("API request failed")
		}
		b, readErr := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024+1))
		resp.Body.Close()
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			c.session.SID = ""
			if attempt == 0 {
				continue
			}
		}
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("API HTTP status %d", resp.StatusCode)
		}
		if readErr != nil || len(b) > 8*1024*1024 {
			return nil, fmt.Errorf("cannot read API response (maximum 8 MiB)")
		}
		var data map[string]interface{}
		if err = json.Unmarshal(b, &data); err != nil || data == nil {
			return nil, fmt.Errorf("API response must be a JSON object")
		}
		return data, nil
	}
	return nil, fmt.Errorf("API authentication failed")
}
func (c *APICollector) Describe(ch chan<- *prometheus.Desc) {
	describeQueries(ch, "api")
	for _, m := range c.metrics {
		ch <- m.Desc
	}
	c.errors.Describe(ch)
}
func (c *APICollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, done := collectionBudget("api")
	defer done()
	old := c.session.Client.Transport
	c.session.Client.Transport = budgetTransport{ctx, transportOrDefault(old)}
	defer func() { c.session.Client.Transport = old }()
	defer c.diagnostics.collect(ch)
	// Also memoize failures for this scrape, avoiding repeated requests/logins.
	results := make(map[string]map[string]interface{})
	seen := make(map[string]bool)
	requestErrors := map[string]error{}
	for i, m := range c.metrics {
		c.diagnostics.begin("api", i, m.Path, m.PromDesc.FqName)
		if ctx.Err() != nil {
			c.diagnostics.fail("timeout")
			continue
		}
		endpoint, _ := apiEndpoint(m.Path, m.Params)
		data, done := results[endpoint]
		if !done {
			entry, ok := c.cache[endpoint]
			if ok && time.Now().Before(entry.expires) {
				data = entry.data
			} else {
				var err error
				data, err = c.load(endpoint)
				if err != nil {
					c.errors.Inc()
					requestErrors[endpoint] = err
					logrus.Warnf("API %s: %s", m.Path, err)
				} else {
					ttl := m.CacheEntryTTL
					for _, other := range c.metrics {
						ep, _ := apiEndpoint(other.Path, other.Params)
						if ep == endpoint && other.CacheEntryTTL < ttl {
							ttl = other.CacheEntryTTL
						}
					}
					c.cache[endpoint] = apiCacheEntry{data: data, expires: time.Now().Add(time.Duration(ttl) * time.Second)}
				}
			}
			results[endpoint] = data
		}
		if data == nil {
			c.diagnostics.fail("request", requestErrors[endpoint])
			continue
		}
		values, err := extractAPIMetrics(&c.renames, data, m)
		if err != nil {
			c.errors.Inc()
			c.diagnostics.fail("extract", err)
			logrus.Warnf("API metric %s: value unavailable", m.PromDesc.FqName)
			continue
		}
		for _, v := range values {
			labels := make([]string, len(m.PromDesc.VarLabels))
			for i, l := range m.PromDesc.VarLabels {
				if l == "gateway" {
					u, _ := url.Parse(c.session.BaseURL)
					labels[i] = u.Hostname()
				} else {
					labels[i] = v.Labels[l]
				}
			}
			keyBytes, _ := json.Marshal([]interface{}{m.PromDesc.FqName, m.PromDesc.FixedLabels, labels})
			key := string(keyBytes)
			if seen[key] {
				c.diagnostics.fail("duplicate")
				c.errors.Inc()
				continue
			}
			seen[key] = true
			metric, err := prometheus.NewConstMetric(m.Desc, m.MetricType, v.Value, labels...)
			if err != nil {
				c.diagnostics.fail("metric", err)
				c.errors.Inc()
				continue
			}
			c.diagnostics.emitted()
			ch <- metric
		}
	}
	c.errors.Collect(ch)
}
