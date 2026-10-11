package rumiho

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestSubscriptionDownloadFallback(t *testing.T) {
	valid := "\ufeffproxies:\n  - {name: node, type: http, server: example.com, port: 80}\nproxy-groups:\n  - {name: Service, type: select, proxies: [node]}\nrules: ['MATCH,Service']\n"
	for _, failure := range []string{"http-error", "html", "truncated", "none", "both-fail"} {
		t.Run(failure, func(t *testing.T) {
			var fallbackCalls atomic.Int32
			fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fallbackCalls.Add(1)
				if r.UserAgent() != "Clash.Meta/rumiho" {
					t.Error("missing Clash-compatible user agent")
				}
				if failure == "both-fail" {
					w.WriteHeader(http.StatusBadGateway)
					return
				}
				io.WriteString(w, valid)
			}))
			defer fallback.Close()
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch failure {
				case "http-error", "both-fail":
					w.WriteHeader(http.StatusServiceUnavailable)
				case "html":
					io.WriteString(w, "<html>login required</html>")
				case "truncated":
					w.Header().Set("Content-Length", "1024")
					io.WriteString(w, "proxies:")
				default:
					io.WriteString(w, valid)
				}
			}))
			defer origin.Close()
			o := testOptions(t)
			u, _ := url.Parse(fallback.URL)
			o.CoreProxy = u.Host
			a, err := New(o)
			if err != nil {
				t.Fatal(err)
			}
			defer a.proxyTransport.CloseIdleConnections()
			s := a.snapshot()
			s.Nodes = []map[string]any{{"name": "cached-node", "type": "http", "server": "example.com", "port": 80}}
			s.Settings.SubscriptionURL = origin.URL
			a.replaceState(s)
			if failure == "both-fail" {
				if err := a.refresh(context.Background()); err == nil || !reflect.DeepEqual(a.snapshot().Nodes, s.Nodes) {
					t.Fatal("failed download replaced cached nodes")
				}
				return
			}
			sub, err := a.fetchSubscription(context.Background(), origin.URL)
			if err != nil || len(sub.Nodes) != 1 || len(subscriptionGroups(State{Routing: sub.Routing})) != 1 {
				t.Fatal("download/parse fallback failed", err)
			}
			wantCalls := int32(1)
			if failure == "none" {
				wantCalls = 0
			}
			if fallbackCalls.Load() != wantCalls {
				t.Fatal("unnecessary or missing fallback request")
			}
		})
	}
}

func TestSubscriptionRoutingPreserved(t *testing.T) {
	sub, err := parseSubscription([]byte(`
mixed-port: 9999
external-controller: 0.0.0.0:9998
secret: upstream-secret
authentication: [untrusted:password]
tun: {enable: true}
dns: {enable: true, listen: 0.0.0.0:53}
proxies:
  - {name: node, type: http, server: example.com, port: 80}
proxy-groups:
  - {name: PROXY, type: select, proxies: [AUTO, node]}
  - {name: AUTO, type: url-test, include-all: true, exclude-filter: excluded, url: 'https://example.com', interval: 300}
  - {name: AI, type: select, proxies: [PROXY, DIRECT]}
rules:
  - RULE-SET,service,AI
  - MATCH,PROXY
rule-providers:
  service: {type: http, behavior: domain, url: 'https://example.com/service.yaml', path: ./state.json, interval: 3600}
geodata-mode: true
geox-url: {geosite: 'https://example.com/geosite.dat'}
`))
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(testOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	s := a.snapshot()
	s.Nodes, s.Routing = sub.Nodes, sub.Routing
	data, err := a.renderConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config["proxy-groups"], sub.Routing["proxy-groups"]) || !reflect.DeepEqual(config["rules"], sub.Routing["rules"]) {
		t.Fatal("subscription groups or rule order changed")
	}
	for _, key := range []string{"tun", "dns", "mixed-port"} {
		if config[key] != nil {
			t.Fatalf("upstream runtime option imported: %s", key)
		}
	}
	if config["secret"] != s.CoreSecret || config["external-controller"] != a.o.CoreAPI || config["allow-lan"] != false {
		t.Fatal("subscription replaced the instance's management boundary")
	}
	provider := config["rule-providers"].(map[string]any)["service"].(map[string]any)
	if !strings.HasPrefix(provider["path"].(string), "./providers/rules/") {
		t.Fatal("upstream provider path not isolated")
	}
	if err := saveState(a.o.ConfigFile, s); err != nil {
		t.Fatal(err)
	}
	restored, err := loadState(a.o)
	if err != nil || len(subscriptionGroups(restored)) != 3 || !hasSubscriptionRules(restored) {
		t.Fatal("subscription routing lost on reload", err)
	}
	restoredConfig, err := a.renderConfig(restored)
	if err != nil || string(data) != string(restoredConfig) {
		t.Fatal("persisting subscription changed the generated configuration", err)
	}
}

func TestSubscriptionRoutingValidation(t *testing.T) {
	base := "proxies:\n  - {name: node, type: http, server: example.com, port: 80}\n"
	for _, extra := range []string{
		"proxy-groups: invalid",
		"proxy-groups: [{name: node, type: select, proxies: [DIRECT]}]",
		"proxy-groups: [{name: GLOBAL, type: select, proxies: [node]}]",
		"proxy-groups: [{name: A, type: select}, {name: A, type: select}]",
		"rules: invalid",
		"rules: [42]",
		"rule-providers: {x: {type: file, path: ./state.json}}",
		"rule-providers: {x: {type: http, url: 'file:///data/state.json'}}",
		"geox-url: {geoip: 'file:///data/state.json'}",
		"proxy-providers: {x: {type: http, url: 'https://example.com'}}",
	} {
		if _, err := parseSubscription([]byte(base + extra)); err == nil {
			t.Fatalf("accepted invalid routing: %s", extra)
		}
	}
	// Empty groups/rules are node-only subscriptions, including nodes named
	// PROXY/AUTO that must not collide with generated fallback groups.
	sub, err := parseSubscription([]byte(strings.Replace(base, "name: node", "name: PROXY", 1) + "proxy-groups: []\nrules: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	s := State{Nodes: sub.Nodes, Routing: sub.Routing}
	if hasSubscriptionRules(s) || len(subscriptionGroups(s)) != 0 || primaryGroup(s) == "PROXY" {
		t.Fatal("node-only fallback has an invalid group or rule configuration")
	}
}

func TestRealCoreSubscriptionGroups(t *testing.T) {
	if os.Getenv("MIHOMO_TEST_BINARY") == "" {
		t.Skip("set MIHOMO_TEST_BINARY to run integration against the real core")
	}
	o := testOptions(t)
	o.Listen = freeAddress(t)
	a, base, cancel, done := startRealApp(t, o)
	defer func() {
		if cancel != nil {
			cancel()
			<-done
		}
	}()
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("routed-via-AI"))
	}))
	defer node.Close()
	u, _ := url.Parse(node.URL)
	host, port, _ := net.SplitHostPort(u.Host)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := "AI"
		if r.URL.Path == "/bad" {
			target = "missing-group"
		}
		fmt.Fprintf(w, `proxies:
  - {name: TEST, type: http, server: %s, port: %s}
proxy-groups:
  - {name: 代理, type: select, proxies: [TEST, 自动]}
  - {name: 自动, type: url-test, include-all: true, url: '%s', interval: 300, lazy: true}
  - {name: AI, type: select, proxies: [DIRECT, 代理]}
rules:
  - DOMAIN,ai.invalid,%s
  - MATCH,REJECT
`, host, port, node.URL, target)
	}))
	defer upstream.Close()
	c := loginClient(t, base)
	settings := Settings{SubscriptionURL: upstream.URL, Mode: "rule", UpdateHours: 24}
	r, body := apiCall(t, c, base, "PUT", "/api/settings", settings)
	if r.StatusCode != 200 {
		t.Fatalf("import full config: %d %s", r.StatusCode, body)
	}
	var state struct {
		SubscriptionGroupCount int  `json:"subscriptionGroupCount"`
		UsesSubscriptionRules  bool `json:"usesSubscriptionRules"`
	}
	if err := json.Unmarshal(body, &state); err != nil || state.SubscriptionGroupCount != 3 || !state.UsesSubscriptionRules {
		t.Fatal("missing routing summary", err)
	}
	assertGroups := func(app *App) {
		t.Helper()
		var result struct {
			Proxies map[string]struct {
				All []string `json:"all"`
				Now string   `json:"now"`
			} `json:"proxies"`
		}
		if err := app.coreRequest(context.Background(), "GET", "/proxies", nil, &result); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result.Proxies["自动"].All, []string{"TEST"}) || result.Proxies["AI"].Now != "代理" {
			t.Fatal("upstream groups, include-all or selections lost")
		}
		if _, ok := result.Proxies["PROXY"]; ok {
			t.Fatal("generated fallback group replaced upstream groups")
		}
	}
	r, body = apiCall(t, c, base, "PUT", "/mihomo/proxies/AI", map[string]string{"name": "代理"})
	if r.StatusCode != 204 {
		t.Fatalf("select group: %d %s", r.StatusCode, body)
	}
	assertGroups(a)
	s := a.snapshot()
	proxyURL, _ := url.Parse(base)
	proxyURL.User = url.UserPassword(s.Admin.Username, s.ProxyPassword)
	tr := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 5 * time.Second}
	response, err := client.Get("http://ai.invalid/check")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if string(data) != "routed-via-AI" {
		t.Fatal("subscription rule did not use the selected AI group")
	}
	settings.SubscriptionURL = upstream.URL + "/bad"
	r, _ = apiCall(t, c, base, "PUT", "/api/settings", settings)
	if r.StatusCode != 400 || a.snapshot().Settings.SubscriptionURL != upstream.URL {
		t.Fatal("bad group reference replaced working subscription")
	}
	assertGroups(a)
	r, body = apiCall(t, c, base, "POST", "/api/refresh", nil)
	if r.StatusCode != 200 {
		t.Fatalf("refresh: %d %s", r.StatusCode, body)
	}
	assertGroups(a)
	cancel()
	<-done
	cancel = nil
	a2, base2, cancel2, done2 := startRealApp(t, o)
	defer func() { cancel2(); <-done2 }()
	assertGroups(a2)
	c2 := loginClient(t, base2)
	settings.SubscriptionURL = ""
	r, body = apiCall(t, c2, base2, "PUT", "/api/settings", settings)
	if r.StatusCode != 200 || len(a2.snapshot().Nodes) != 0 || len(a2.snapshot().Routing) != 0 {
		t.Fatalf("clearing subscription left stale routing: %d %s", r.StatusCode, body)
	}
}
