package rumiho

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type session struct {
	Expires time.Time
	Context context.Context
	Cancel  context.CancelFunc
}
type attempt struct {
	Count int
	Until time.Time
}

type App struct {
	o              Options
	mu             sync.RWMutex
	state          State
	cookieName     string
	changeMu       sync.Mutex
	authMu         sync.Mutex
	sessions       map[string]session
	attempts       map[string]attempt
	coreClient     *http.Client
	proxyTransport *http.Transport
	controller     *httputil.ReverseProxy
	dashboard      http.Handler
	tunnels        sync.Map
}

func New(o Options) (*App, error) {
	dataDir, err := filepath.Abs(o.DataDir)
	if err != nil {
		return nil, err
	}
	o.DataDir = dataDir
	if (o.TLSCert == "") != (o.TLSKey == "") {
		return nil, errors.New("TLS_CERT 和 TLS_KEY 必须同时设置")
	}
	for _, addr := range []string{o.CoreProxy, o.CoreAPI} {
		host, _, err := net.SplitHostPort(addr)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			return nil, errors.New("mihomo 内部接口必须监听回环地址")
		}
	}
	if err := os.MkdirAll(o.DataDir, 0700); err != nil {
		return nil, err
	}
	s, err := loadState(o)
	if err != nil {
		return nil, err
	}
	a := &App{o: o, state: s, sessions: map[string]session{}, attempts: map[string]attempt{},
		coreClient: &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{}},
	}
	identity := sha256.Sum256([]byte(s.CoreSecret))
	a.cookieName = fmt.Sprintf("rumiho_session_%x", identity[:6])
	a.proxyTransport = &http.Transport{
		Proxy: func(_ *http.Request) (*url.URL, error) {
			s := a.snapshot()
			return &url.URL{Scheme: "http", Host: o.CoreProxy, User: url.UserPassword(s.Admin.Username, s.ProxyPassword)}, nil
		}, MaxIdleConns: 100, IdleConnTimeout: 60 * time.Second, ResponseHeaderTimeout: 30 * time.Second,
	}
	target := &url.URL{Scheme: "http", Host: o.CoreAPI}
	a.controller = httputil.NewSingleHostReverseProxy(target)
	original := a.controller.Director
	a.controller.Director = func(r *http.Request) {
		original(r)
		r.URL.Path = r.URL.Path[len("/mihomo"):]
		r.URL.RawPath = ""
		q := r.URL.Query()
		q.Del("token")
		r.URL.RawQuery = q.Encode()
		r.Header.Del("Cookie")
		r.Header.Del("Proxy-Authorization")
		r.Header.Set("Authorization", "Bearer "+a.snapshot().CoreSecret)
	}
	a.controller.Transport = &http.Transport{ResponseHeaderTimeout: 15 * time.Second, IdleConnTimeout: 60 * time.Second}
	a.controller.FlushInterval = -1
	a.controller.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) { apiError(w, 502, "mihomo 暂时不可用") }
	a.dashboard = http.FileServer(http.Dir(o.DashboardDir))
	return a, nil
}

func (a *App) snapshot() State      { a.mu.RLock(); defer a.mu.RUnlock(); return a.state }
func (a *App) replaceState(s State) { a.mu.Lock(); a.state = s; a.mu.Unlock() }

func (a *App) Run(ctx context.Context) error {
	if err := a.prepareRules(); err != nil {
		return err
	}
	s := a.snapshot()
	config, err := a.renderConfig(s)
	if err != nil {
		return err
	}
	path := filepath.Join(a.o.DataDir, "mihomo.yaml")
	if err = atomicWrite(path, config); err != nil {
		return err
	}
	if err = a.validateConfig(ctx, path); err != nil {
		return err
	}
	core := exec.Command(a.o.CoreBinary, "-d", a.o.DataDir, "-f", path)
	core.Stdout, core.Stderr = os.Stdout, os.Stderr
	if err = core.Start(); err != nil {
		return err
	}
	coreDone := make(chan struct{})
	var coreErr error
	go func() {
		coreErr = core.Wait()
		close(coreDone)
	}()
	defer func() {
		_ = core.Process.Signal(os.Interrupt)
		select {
		case <-coreDone:
		case <-time.After(5 * time.Second):
			_ = core.Process.Kill()
		}
	}()
	ready := false
	for i := 0; i < 100; i++ {
		if a.coreRequest(ctx, "GET", "/version", nil, nil) == nil {
			ready = true
			break
		}
		select {
		case <-coreDone:
			return fmt.Errorf("mihomo 启动失败: %v", coreErr)
		case <-ctx.Done():
			return nil
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !ready {
		return errors.New("等待 mihomo 就绪超时")
	}
	server := &http.Server{Addr: a.o.Listen, Handler: a, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 64 << 10}
	serverDone := make(chan error, 1)
	go func() {
		if a.o.TLSCert != "" {
			serverDone <- server.ListenAndServeTLS(a.o.TLSCert, a.o.TLSKey)
		} else {
			serverDone <- server.ListenAndServe()
		}
	}()
	schedulerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go a.schedule(schedulerCtx)
	log.Printf("Rumiho 已启动，入口 %s，管理员 %s", a.o.Listen, s.Admin.Username)
	var result error
	select {
	case <-ctx.Done():
	case <-coreDone:
		result = fmt.Errorf("mihomo 异常退出: %v", coreErr)
	case err := <-serverDone:
		if !errors.Is(err, http.ErrServerClosed) {
			result = err
		}
	}
	a.closeTunnels()
	a.closeSessions()
	shutdownCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	_ = server.Shutdown(shutdownCtx)
	a.proxyTransport.CloseIdleConnections()
	return result
}

func (a *App) closeSessions() {
	a.authMu.Lock()
	defer a.authMu.Unlock()
	for _, s := range a.sessions {
		s.Cancel()
	}
	clear(a.sessions)
}

func (a *App) coreRequest(ctx context.Context, method, path string, body any, result any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://"+a.o.CoreAPI+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.snapshot().CoreSecret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.coreClient.Do(req)
	if err != nil {
		return errors.New("无法连接 mihomo 控制接口")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("mihomo 控制接口返回 %d", resp.StatusCode)
	}
	if result != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(result)
	}
	return nil
}

func Healthcheck() error {
	o := OptionsFromEnv()
	_, port, err := net.SplitHostPort(o.Listen)
	if err != nil {
		return err
	}
	scheme := "http"
	if o.TLSCert != "" {
		scheme = "https"
	}
	// Check TCP entry plus the authenticated core API; no TLS certificate bypass.
	if scheme == "https" {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), 2*time.Second)
		if err != nil {
			return err
		}
		conn.Close()
	} else {
		client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{}}
		resp, err := client.Get("http://127.0.0.1:" + port + "/healthz")
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			return errors.New("入口未就绪")
		}
		// /healthz already checks the authenticated core API. Avoid loading the
		// entire node cache and making that same request again on every probe.
		return nil
	}
	s, err := loadState(o)
	if err != nil {
		return err
	}
	req, _ := http.NewRequest("GET", "http://"+o.CoreAPI+"/version", nil)
	req.Header.Set("Authorization", "Bearer "+s.CoreSecret)
	resp, err := (&http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{}}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("内核未就绪")
	}
	return nil
}
