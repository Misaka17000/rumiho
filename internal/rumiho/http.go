package rumiho

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

func jsonResponse(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}
func apiError(w http.ResponseWriter, code int, message string) {
	jsonResponse(w, code, map[string]string{"error": message})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("请求必须使用 JSON")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return errors.New("请求格式不正确")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("请求只能包含一个 JSON 对象")
	}
	return nil
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == "CONNECT" || r.URL.IsAbs() {
		a.serveProxy(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	if r.URL.Path == "/healthz" {
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		if a.coreRequest(r.Context(), "GET", "/version", nil, nil) != nil {
			apiError(w, 503, "内核未就绪")
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Header.Get("X-Rumiho-Request") != "1" {
			apiError(w, 403, "缺少请求校验标识")
			return
		}
		if !sameOrigin(r) {
			apiError(w, 403, "请求来源不匹配")
			return
		}
	}
	if r.URL.Path == "/api/login" {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		a.login(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/sub/") {
		http.NotFound(w, r)
		return
	}
	// The SPA shell and bundled assets are public; every data and controller
	// endpoint below still requires the authenticated administrator session.
	if r.URL.Path == "/" || r.URL.Path == "/login" || strings.HasPrefix(r.URL.Path, "/assets/") || r.URL.Path == "/favicon.svg" || r.URL.Path == "/favicon-dark.svg" || r.URL.Path == "/favicon.ico" || r.URL.Path == "/apple-touch-icon.png" {
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(405)
			return
		}
		if r.URL.Path == "/login" {
			http.Redirect(w, r, "/#/login", 303)
			return
		}
		a.dashboard.ServeHTTP(w, r)
		return
	}
	if r.URL.Path == "/dashboard" || r.URL.Path == "/dashboard/" {
		http.Redirect(w, r, "/", 302)
		return
	}
	if !a.authenticated(r) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/mihomo/") {
			apiError(w, 401, "请先登录")
		} else {
			http.Redirect(w, r, "/login", 303)
		}
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		a.api(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/mihomo/") {
		a.mihomo(w, r)
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		w.WriteHeader(405)
		return
	}
	http.NotFound(w, r)
}

func sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return err == nil && u.Scheme == scheme && strings.EqualFold(u.Host, r.Host)
}

func (a *App) authenticated(r *http.Request) bool {
	c, err := r.Cookie(a.cookieName)
	if err != nil {
		return false
	}
	a.authMu.Lock()
	defer a.authMu.Unlock()
	s, ok := a.sessions[c.Value]
	if ok && s.Expires.After(time.Now()) {
		return true
	}
	if ok {
		s.Cancel()
	}
	delete(a.sessions, c.Value)
	return false
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var data struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &data); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	a.authMu.Lock()
	for key, v := range a.attempts {
		if v.Until.Before(time.Now()) {
			delete(a.attempts, key)
		}
	}
	v := a.attempts[host]
	if v.Count >= 5 || len(a.attempts) >= 4096 {
		a.authMu.Unlock()
		apiError(w, 429, "登录失败次数过多，请 15 分钟后重试")
		return
	}
	// Count before checking credentials so concurrent attempts cannot bypass the limit.
	if v.Count == 0 {
		v.Until = time.Now().Add(15 * time.Minute)
	}
	v.Count++
	a.attempts[host] = v
	a.authMu.Unlock()
	a.changeMu.Lock()
	defer a.changeMu.Unlock()
	s := a.snapshot()
	wantPassword, gotPassword := sha256.Sum256([]byte(s.Admin.Password)), sha256.Sum256([]byte(data.Password))
	wantUsername, gotUsername := sha256.Sum256([]byte(s.Admin.Username)), sha256.Sum256([]byte(data.Username))
	if subtle.ConstantTimeCompare(wantPassword[:], gotPassword[:])&subtle.ConstantTimeCompare(wantUsername[:], gotUsername[:]) != 1 {
		apiError(w, 401, "用户名或密码错误")
		return
	}
	token := randomToken()
	a.authMu.Lock()
	delete(a.attempts, host)
	for k, v := range a.sessions {
		if v.Expires.Before(time.Now()) {
			v.Cancel()
			delete(a.sessions, k)
		}
	}
	if len(a.sessions) >= 64 {
		a.authMu.Unlock()
		apiError(w, 429, "登录会话过多，请稍后再试")
		return
	}
	sessionCtx, sessionCancel := context.WithTimeout(context.Background(), 12*time.Hour)
	a.sessions[token] = session{Expires: time.Now().Add(12 * time.Hour), Context: sessionCtx, Cancel: sessionCancel}
	a.authMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: a.cookieName, Value: token, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 12 * 3600})
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

func (a *App) api(w http.ResponseWriter, r *http.Request) {
	switch r.Method + " " + r.URL.Path {
	case "GET /api/state":
		a.serveState(w, r)
	case "PUT /api/settings":
		a.settings(w, r)
	case "POST /api/refresh":
		if err := a.refresh(r.Context()); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		a.serveState(w, r)
	case "POST /api/rotate":
		a.changeMu.Lock()
		defer a.changeMu.Unlock()
		s := a.snapshot()
		s.ProxyPassword = randomToken()
		if err := a.commit(r.Context(), s); err != nil {
			apiError(w, 500, err.Error())
			return
		}
		a.closeTunnels()
		_ = a.coreRequest(r.Context(), "DELETE", "/connections", nil, nil)
		a.serveState(w, r)
	case "POST /api/password":
		a.password(w, r)
	case "POST /api/logout":
		if cookie, err := r.Cookie(a.cookieName); err == nil {
			a.authMu.Lock()
			if s, ok := a.sessions[cookie.Value]; ok {
				s.Cancel()
			}
			delete(a.sessions, cookie.Value)
			a.authMu.Unlock()
		}
		http.SetCookie(w, &http.Cookie{Name: a.cookieName, Path: "/", MaxAge: -1, HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode})
		jsonResponse(w, 200, map[string]bool{"ok": true})
	default:
		apiError(w, 404, "接口不存在")
	}
}

func (a *App) serveState(w http.ResponseWriter, r *http.Request) {
	s := a.snapshot()
	u, _ := url.Parse(s.Settings.PublicURL)
	if u == nil || u.Host == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		u = &url.URL{Scheme: scheme, Host: r.Host}
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	u.User = nil
	proxy := *u
	proxy.User = url.UserPassword(s.Admin.Username, s.ProxyPassword)
	var version map[string]any
	online := a.coreRequest(r.Context(), "GET", "/version", nil, &version) == nil
	jsonResponse(w, 200, map[string]any{"username": s.Admin.Username, "settings": s.Settings, "nodeCount": len(s.Nodes),
		"subscriptionGroupCount": len(subscriptionGroups(s)), "usesSubscriptionRules": hasSubscriptionRules(s),
		"lastUpdated": s.LastUpdated, "lastError": s.LastError, "coreOnline": online, "version": version,
		"access": map[string]string{"publicURL": u.String(), "proxyURL": proxy.String()},
	})
}

func (a *App) settings(w http.ResponseWriter, r *http.Request) {
	var next Settings
	if err := decodeJSON(w, r, &next); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	next.SubscriptionURL = strings.TrimSpace(next.SubscriptionURL)
	next.PublicURL = strings.TrimRight(strings.TrimSpace(next.PublicURL), "/")
	a.changeMu.Lock()
	defer a.changeMu.Unlock()
	s := a.snapshot()
	// The instance page does not edit mode. Preserve the latest selection from
	// the proxy page even if it changed while the instance form was open.
	if next.Mode == "" {
		next.Mode = s.Settings.Mode
	}
	if err := validateSettings(next); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	if next.SubscriptionURL != s.Settings.SubscriptionURL {
		if next.SubscriptionURL == "" {
			s.Nodes = []map[string]any{}
			s.Routing = nil
			s.LastUpdated = time.Time{}
		} else {
			sub, err := a.fetchSubscription(r.Context(), next.SubscriptionURL)
			if err != nil {
				apiError(w, 400, err.Error())
				return
			}
			s.Nodes, s.Routing, s.LastUpdated = sub.Nodes, sub.Routing, time.Now()
		}
	}
	s.Settings, s.LastError = next, ""
	if err := a.commit(r.Context(), s); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	a.serveState(w, r)
}

func (a *App) password(w http.ResponseWriter, r *http.Request) {
	var data struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := decodeJSON(w, r, &data); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	a.changeMu.Lock()
	defer a.changeMu.Unlock()
	s := a.snapshot()
	want, got := sha256.Sum256([]byte(s.Admin.Password)), sha256.Sum256([]byte(data.CurrentPassword))
	if subtle.ConstantTimeCompare(want[:], got[:]) != 1 {
		apiError(w, 400, "当前密码错误")
		return
	}
	s.Admin.Password = data.NewPassword
	if err := validateAccount(s.Admin); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	if err := saveState(a.o.ConfigFile, s); err != nil {
		apiError(w, 500, "无法写入配置文件，请检查部署目录的写入权限")
		return
	}
	a.replaceState(s)
	a.closeSessions()
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

func (a *App) mihomo(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		apiError(w, 403, "请求来源不匹配")
		return
	}
	// End already-open controller streams/WebSockets on logout or expiry too.
	cookie, _ := r.Cookie(a.cookieName)
	a.authMu.Lock()
	session, ok := a.sessions[cookie.Value]
	a.authMu.Unlock()
	if !ok {
		apiError(w, 401, "请先登录")
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(session.Context, cancel)
	defer stop()
	defer cancel()
	r = r.WithContext(ctx)
	p := strings.TrimPrefix(r.URL.Path, "/mihomo")
	if path.Clean(p) != p {
		apiError(w, 403, "无效的接口路径")
		return
	}
	if r.Method == "PATCH" && p == "/configs" {
		var data map[string]json.RawMessage
		if err := decodeJSON(w, r, &data); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		if len(data) != 1 || data["mode"] == nil {
			apiError(w, 403, "请在 Rumiho 设置页管理此配置")
			return
		}
		var mode string
		if json.Unmarshal(data["mode"], &mode) != nil {
			apiError(w, 400, "模式无效")
			return
		}
		a.changeMu.Lock()
		defer a.changeMu.Unlock()
		s := a.snapshot()
		s.Settings.Mode = mode
		if err := validateSettings(s.Settings); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		if err := a.commit(r.Context(), s); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		w.WriteHeader(204)
		return
	}
	allowed := false
	if r.Method == "GET" {
		for _, prefix := range []string{"/version", "/configs", "/proxies", "/group", "/connections", "/traffic", "/memory", "/logs", "/rules", "/providers/proxies", "/providers/rules"} {
			if p == prefix || strings.HasPrefix(p, prefix+"/") {
				allowed = true
				break
			}
		}
	}
	if r.Method == "PUT" && strings.HasPrefix(p, "/proxies/") && !strings.HasSuffix(p, "/delay") {
		allowed = true
	}
	if r.Method == "DELETE" && (p == "/connections" || strings.HasPrefix(p, "/connections/")) {
		allowed = true
	}
	if !allowed {
		apiError(w, 403, "此操作由 Rumiho 管理，请使用设置页")
		return
	}
	a.controller.ServeHTTP(w, r)
}
