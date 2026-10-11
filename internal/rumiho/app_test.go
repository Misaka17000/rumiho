package rumiho

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testOptions(t *testing.T) Options {
	t.Helper()
	dashboard := t.TempDir()
	if err := os.Mkdir(filepath.Join(dashboard, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"index.html":    `<!doctype html><div id="app"></div><script src="/assets/app.js"></script>`,
		"assets/app.js": `/* public application shell */`,
	} {
		if err := os.WriteFile(filepath.Join(dashboard, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configFile, []byte("admin:\n  username: admin\n  password: test-admin-password-123\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return Options{Listen: "127.0.0.1:0", DataDir: t.TempDir(), CoreBinary: os.Getenv("MIHOMO_TEST_BINARY"),
		CoreProxy: freeAddress(t), CoreAPI: freeAddress(t), RulesDir: "../../third_party/rules", DashboardDir: dashboard,
		ConfigFile: configFile}
}

func freeAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

func apiCall(t *testing.T, c *http.Client, base, method, path string, body any) (*http.Response, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(b)
	}
	r, _ := http.NewRequest(method, base+path, reader)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Rumiho-Request", "1")
	resp, err := c.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return resp, data
}

func loginClient(t *testing.T, base string) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 15 * time.Second}
	r, b := apiCall(t, c, base, "POST", "/api/login", map[string]string{"username": "admin", "password": "test-admin-password-123"})
	if r.StatusCode != 200 {
		t.Fatalf("login: %d %s", r.StatusCode, b)
	}
	return c
}

func TestSubscriptionExtraction(t *testing.T) {
	sub, err := parseSubscription([]byte("port: 9999\nsecret: evil\nproxies:\n - name: node\n   type: http\n   server: example.com\n   port: 80\n"))
	if err != nil || len(sub.Nodes) != 1 {
		t.Fatal(sub, err)
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
	if bytes.Contains(data, []byte("evil")) || bytes.Contains(data, []byte("9999")) {
		t.Fatal("upstream configuration escaped node extraction")
	}
	for _, invalid := range []string{"c3M6Ly9leGFtcGxl", "proxies: []", "- {name: DIRECT, type: http}", "- {name: x, type: http}\n- {name: x, type: http}", "proxies: []\n---\nproxies: []"} {
		if _, err := parseSubscription([]byte(invalid)); err == nil {
			t.Fatalf("accepted invalid subscription: %s", invalid)
		}
	}
}

func TestAuthenticationAndManagementBoundaries(t *testing.T) {
	a, err := New(testOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a)
	defer server.Close()
	// Login and authenticated routes share one public SPA, but no state is
	// embedded in its HTML and API/controller access still requires a session.
	for _, p := range []string{"/", "/assets/app.js", "/login", "/dashboard/"} {
		r, body := apiCall(t, server.Client(), server.URL, "GET", p, nil)
		if r.StatusCode != 200 || bytes.Contains(body, []byte(a.snapshot().ProxyPassword)) {
			t.Fatalf("public shell route %s: %d", p, r.StatusCode)
		}
	}
	for _, p := range []string{"/api/state", "/mihomo/version", "/mihomo/connections"} {
		r, _ := apiCall(t, server.Client(), server.URL, "GET", p, nil)
		if r.StatusCode != 401 {
			t.Fatalf("unprotected %s: %d", p, r.StatusCode)
		}
	}
	c := loginClient(t, server.URL)
	r, page := apiCall(t, c, server.URL, "GET", "/", nil)
	if r.StatusCode != 200 || !bytes.Contains(page, []byte(`id="app"`)) {
		t.Fatalf("home did not load: %d", r.StatusCode)
	}
	for _, p := range []string{"/configs", "/restart", "/upgrade"} {
		r, _ := apiCall(t, c, server.URL, "PUT", "/mihomo"+p, map[string]string{"path": "https://evil"})
		if r.StatusCode != 403 {
			t.Fatalf("unsafe mutation %s: %d", p, r.StatusCode)
		}
	}
	r, _ = apiCall(t, c, server.URL, "PATCH", "/mihomo/configs", map[string]any{"allow-lan": true})
	if r.StatusCode != 403 {
		t.Fatal("allowed network setting mutation")
	}
	req, _ := http.NewRequest("POST", server.URL+"/api/rotate", nil)
	req.Header.Set("X-Rumiho-Request", "1")
	req.Header.Set("Origin", "https://evil.example")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("allowed cross origin mutation")
	}
	req, _ = http.NewRequest("POST", server.URL+"/api/rotate", nil)
	resp, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("allowed missing CSRF marker")
	}
	proxyURL, _ := url.Parse(server.URL)
	p := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	resp, err = p.Get("http://destination.invalid/api/state")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 407 {
		t.Fatal("absolute proxy request reached management")
	}
	apiCall(t, c, server.URL, "POST", "/api/logout", nil)
	r, _ = apiCall(t, c, server.URL, "GET", "/api/state", nil)
	if r.StatusCode != 401 {
		t.Fatal("logout did not invalidate session")
	}
	data, err := os.ReadFile(a.o.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(a.snapshot().Admin.Password)) {
		t.Fatal("plaintext login password missing from unified config")
	}
}

func TestInstancesDoNotOverwriteBrowserSessions(t *testing.T) {
	a, err := New(testOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(testOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	first, second := httptest.NewServer(a), httptest.NewServer(b)
	defer first.Close()
	defer second.Close()
	client := loginClient(t, first.URL)
	r, body := apiCall(t, client, second.URL, "POST", "/api/login", map[string]string{"username": "admin", "password": "test-admin-password-123"})
	if r.StatusCode != 200 {
		t.Fatalf("second login: %d %s", r.StatusCode, body)
	}
	for _, base := range []string{first.URL, second.URL} {
		r, _ := apiCall(t, client, base, "GET", "/api/state", nil)
		if r.StatusCode != 200 || r.Request.URL.Path != "/api/state" {
			t.Fatal("instance session overwritten")
		}
	}
	apiCall(t, client, first.URL, "POST", "/api/logout", nil)
	r, _ = apiCall(t, client, second.URL, "GET", "/api/state", nil)
	if r.StatusCode != 200 || r.Request.URL.Path != "/api/state" {
		t.Fatal("logout affected another instance")
	}
}

func TestMetadataUpdateWithoutCoreAndFailedPersistence(t *testing.T) {
	o := testOptions(t)
	o.CoreBinary = filepath.Join(t.TempDir(), "missing-core")
	a, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	config, err := a.renderConfig(a.snapshot())
	if err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(o.DataDir, "mihomo.yaml")
	if err = atomicWrite(active, config); err != nil {
		t.Fatal(err)
	}
	next := a.snapshot()
	next.Settings.PublicURL = "http://192.168.1.10:8080"
	next.Settings.UpdateHours = 48
	next.LastUpdated = time.Now().UTC()
	if err = a.commit(context.Background(), next); err != nil {
		t.Fatalf("metadata update unnecessarily required a running core: %v", err)
	}
	stored, err := loadState(o)
	if err != nil || stored.Settings != next.Settings || !stored.LastUpdated.Equal(next.LastUpdated) {
		t.Fatalf("metadata was not persisted: %v", err)
	}
	unchanged, err := os.ReadFile(active)
	if err != nil || !bytes.Equal(unchanged, config) {
		t.Fatal("metadata update changed core configuration")
	}
	statePath := o.ConfigFile
	if err = os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(statePath, 0700); err != nil {
		t.Fatal(err)
	}
	next.Settings.UpdateHours = 72
	if err = a.commit(context.Background(), next); err == nil {
		t.Fatal("accepted failed persistence")
	}
	if a.snapshot().Settings.UpdateHours != 48 {
		t.Fatal("failed persistence changed in-memory state")
	}
}

func TestHealthcheckUsesGatewayReadiness(t *testing.T) {
	status := http.StatusOK
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" || r.Method != "GET" {
			t.Error("incorrect readiness endpoint")
		}
		w.WriteHeader(status)
	}))
	defer gateway.Close()
	t.Setenv("LISTEN_ADDR", strings.TrimPrefix(gateway.URL, "http://"))
	t.Setenv("TLS_CERT", "")
	// The gateway performs the authenticated check. The probe does not need
	// access to the stored subscription or the administrator configuration.
	t.Setenv("DATA_DIR", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("CONFIG_FILE", filepath.Join(t.TempDir(), "missing.yaml"))
	if err := Healthcheck(); err != nil {
		t.Fatalf("healthy gateway failed probe: %v", err)
	}
	status = http.StatusServiceUnavailable
	if err := Healthcheck(); err == nil {
		t.Fatal("unhealthy gateway passed probe")
	}
}

func TestRealCoreExitStopsGatewayPromptly(t *testing.T) {
	if os.Getenv("MIHOMO_TEST_BINARY") == "" {
		t.Skip("set MIHOMO_TEST_BINARY to run integration against the real core")
	}
	o := testOptions(t)
	o.Listen = freeAddress(t)
	pidPath := filepath.Join(t.TempDir(), "core.pid")
	wrapper := filepath.Join(t.TempDir(), "core-wrapper")
	t.Setenv("RUMIHO_TEST_CORE", o.CoreBinary)
	t.Setenv("RUMIHO_TEST_PID_FILE", pidPath)
	script := "#!/bin/sh\nif [ \"$1\" != \"-t\" ]; then printf '%s' \"$$\" > \"$RUMIHO_TEST_PID_FILE\"; fi\nexec \"$RUMIHO_TEST_CORE\" \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	o.CoreBinary = wrapper
	_, base, cancel, done := startRealApp(t, o)
	defer cancel()
	pidBytes, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(pidBytes))
	if err != nil {
		t.Fatal(err)
	}
	core, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	if err = core.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "mihomo") {
			t.Fatalf("core failure was not reported: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("gateway delayed exit after core stopped")
	}
	client := &http.Client{Timeout: time.Second}
	if response, err := client.Get(base + "/healthz"); err == nil {
		response.Body.Close()
		t.Fatal("gateway still listening after core exit")
	}
}

func startRealApp(t *testing.T, o Options) (*App, string, context.CancelFunc, <-chan error) {
	t.Helper()
	a, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	base := "http://" + o.Listen
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return a, base, cancel, done
			}
		}
		select {
		case err := <-done:
			cancel()
			t.Fatalf("app failed to start: %v", err)
		default:
		}
		time.Sleep(100 * time.Millisecond)
	}
	cancel()
	t.Fatal("app startup timed out")
	return nil, "", nil, nil
}

func TestRealCoreEndToEnd(t *testing.T) {
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
	c := loginClient(t, base)
	s := a.snapshot()
	proxyURL, _ := url.Parse(base)
	proxyURL.User = url.UserPassword(s.Admin.Username, s.ProxyPassword)
	tr := &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} // Test's self-signed origin only.
	defer tr.CloseIdleConnections()
	proxy := &http.Client{Transport: tr, Timeout: 15 * time.Second}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credentials leaked to origin")
		}
		w.Write([]byte("http-ok"))
	}))
	defer origin.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("https-ok")) }))
	defer secure.Close()
	for target, expected := range map[string]string{origin.URL: "http-ok", secure.URL: "https-ok"} {
		resp, err := proxy.Get(target)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(body) != expected {
			t.Fatalf("proxy %s: %d %s", target, resp.StatusCode, body)
		}
	}
	// Real WebSocket upgrade and streaming data, with the controller secret kept server-side.
	u, _ := url.Parse(base)
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	cookie := c.Jar.Cookies(u)[0]
	fmt.Fprintf(conn, "GET /mihomo/connections HTTP/1.1\r\nHost: %s\r\nCookie: %s=%s\r\nOrigin: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n", u.Host, cookie.Name, cookie.Value, base)
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 101 {
		t.Fatalf("websocket: %d", resp.StatusCode)
	}
	if _, err = br.ReadByte(); err != nil {
		t.Fatal("websocket data:", err)
	}
	conn.Close()
	releaseStream := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hold" {
			w.Write([]byte("stream"))
			w.(http.Flusher).Flush()
			<-releaseStream
			return
		}
		w.Write([]byte("subscription-node-ok"))
	}))
	defer upstream.Close()
	nodeURL, _ := url.Parse(upstream.URL)
	nodeHost, nodePort, _ := net.SplitHostPort(nodeURL.Host)
	subscription := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			w.Write([]byte("proxies:\n - {name: BAD, type: unknown-protocol}\n"))
			return
		}
		fmt.Fprintf(w, "port: 1234\nsecret: ignored\nproxies:\n - {name: TEST, type: http, server: %s, port: %s}\n", nodeHost, nodePort)
	}))
	defer subscription.Close()
	settings := Settings{SubscriptionURL: subscription.URL, Mode: "global", UpdateHours: 24, PublicURL: base}
	resp, body := apiCall(t, c, base, "PUT", "/api/settings", settings)
	if resp.StatusCode != 200 {
		t.Fatalf("import: %d %s", resp.StatusCode, body)
	}
	var automatic struct {
		Now string `json:"now"`
	}
	if err := a.coreRequest(context.Background(), "GET", "/proxies/PROXY", nil, &automatic); err != nil || automatic.Now != "AUTO" {
		t.Fatalf("first import did not select AUTO: %+v %v", automatic, err)
	}
	for group, selection := range map[string]string{"PROXY": "TEST", "GLOBAL": "PROXY"} {
		resp, body := apiCall(t, c, base, "PUT", "/mihomo/proxies/"+group, map[string]string{"name": selection})
		if resp.StatusCode != 204 {
			t.Fatalf("select: %d %s", resp.StatusCode, body)
		}
	}
	resp, err = proxy.Get("http://example.invalid/test")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "subscription-node-ok" {
		t.Fatalf("subscription node unused: %s", body)
	}
	settings.SubscriptionURL += "/bad"
	resp, _ = apiCall(t, c, base, "PUT", "/api/settings", settings)
	if resp.StatusCode != 400 {
		t.Fatal("accepted invalid core config")
	}
	if a.snapshot().Settings.SubscriptionURL != subscription.URL || len(a.snapshot().Nodes) != 1 {
		t.Fatal("invalid subscription replaced working config")
	}
	oldProxyPassword := a.snapshot().ProxyPassword
	// LAN clients use the authenticated HTTP proxy URL directly.
	stream, err := proxy.Get("http://example.invalid/hold")
	if err != nil {
		close(releaseStream)
		t.Fatal("HTTP proxy request failed:", err)
	}
	var live struct {
		Connections []struct {
			ID string `json:"id"`
		} `json:"connections"`
	}
	if err := a.coreRequest(context.Background(), "GET", "/connections", nil, &live); err != nil || len(live.Connections) == 0 {
		close(releaseStream)
		stream.Body.Close()
		t.Fatalf("HTTP proxy connection not visible: %v", err)
	}
	connectionID := live.Connections[0].ID
	settings = a.snapshot().Settings
	// The instance page no longer submits mode. Saving another field must keep
	// the mode selected on the proxy page and must not break open connections.
	metadata := map[string]any{"subscriptionURL": settings.SubscriptionURL, "updateHours": 48, "publicURL": settings.PublicURL}
	for _, change := range []struct {
		method, path string
		body         any
	}{{"PUT", "/api/settings", metadata}, {"POST", "/api/refresh", nil}} {
		resp, body = apiCall(t, c, base, change.method, change.path, change.body)
		if resp.StatusCode != 200 {
			close(releaseStream)
			stream.Body.Close()
			t.Fatalf("metadata update or unchanged refresh: %d %s", resp.StatusCode, body)
		}
	}
	if a.snapshot().Settings.Mode != "global" || a.snapshot().Settings.UpdateHours != 48 {
		t.Fatal("instance settings overwrote the proxy page's mode")
	}
	if err := a.coreRequest(context.Background(), "GET", "/connections", nil, &live); err != nil {
		close(releaseStream)
		stream.Body.Close()
		t.Fatal(err)
	}
	preserved := false
	for _, connection := range live.Connections {
		preserved = preserved || connection.ID == connectionID
	}
	if !preserved {
		close(releaseStream)
		stream.Body.Close()
		t.Fatal("metadata save or unchanged refresh interrupted the active connection")
	}
	close(releaseStream)
	streamBody, _ := io.ReadAll(stream.Body)
	stream.Body.Close()
	if string(streamBody) != "stream" {
		t.Fatalf("HTTP proxy stream: %s", streamBody)
	}
	resp, body = apiCall(t, c, base, "POST", "/api/rotate", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("rotate: %d %s", resp.StatusCode, body)
	}
	resp, err = proxy.Get(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 407 {
		t.Fatal("old proxy credentials still valid")
	}
	newProxyURL := *proxyURL
	newProxyURL.User = url.UserPassword(a.snapshot().Admin.Username, a.snapshot().ProxyPassword)
	newTransport := &http.Transport{Proxy: http.ProxyURL(&newProxyURL)}
	defer newTransport.CloseIdleConnections()
	newClient := &http.Client{Transport: newTransport, Timeout: 5 * time.Second}
	resp, err = newClient.Get(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal("new proxy link is unusable after reset")
	}
	// Logout revokes an already-established monitoring socket as well.
	watch, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Close()
	fmt.Fprintf(watch, "GET /mihomo/connections HTTP/1.1\r\nHost: %s\r\nCookie: %s=%s\r\nOrigin: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n", u.Host, cookie.Name, cookie.Value, base)
	watchReader := bufio.NewReader(watch)
	watchResp, err := http.ReadResponse(watchReader, nil)
	if err != nil || watchResp.StatusCode != 101 {
		t.Fatal("monitor socket failed", err)
	}
	ended := make(chan struct{})
	go func() { io.Copy(io.Discard, watchReader); close(ended) }()
	apiCall(t, c, base, "POST", "/api/logout", nil)
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("logout left WebSocket authorized")
	}
	// Recreate the application against the same volume and verify all durable state.
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	cancel = nil
	a2, _, cancel2, done2 := startRealApp(t, o)
	defer func() { cancel2(); <-done2 }()
	if len(a2.snapshot().Nodes) != 1 || a2.snapshot().Settings.Mode != "global" || a2.snapshot().ProxyPassword == oldProxyPassword {
		t.Fatal("state not retained on restart")
	}
	var selected struct {
		Now string `json:"now"`
	}
	if err := a2.coreRequest(context.Background(), "GET", "/proxies/PROXY", nil, &selected); err != nil || selected.Now != "TEST" {
		t.Fatalf("node selection not retained: %+v %v", selected, err)
	}
	// A separate Docker-style instance must have entirely separate credentials and configuration.
	other := testOptions(t)
	other.Listen = freeAddress(t)
	a3, _, cancel3, done3 := startRealApp(t, other)
	defer func() { cancel3(); <-done3 }()
	if a3.snapshot().ProxyPassword == a2.snapshot().ProxyPassword || len(a3.snapshot().Nodes) != 0 {
		t.Fatal("instances share state")
	}
	if strings.Contains(a2.snapshot().Settings.SubscriptionURL, "/bad") {
		t.Fatal("failed import persisted")
	}
}
