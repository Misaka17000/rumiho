package rumiho

import (
	"bufio"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

func (a *App) proxyAuthorized(r *http.Request) bool {
	parts := strings.SplitN(r.Header.Get("Proxy-Authorization"), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Basic") {
		return false
	}
	data, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	s := a.snapshot()
	return subtle.ConstantTimeCompare(data, []byte(s.Admin.Username+":"+s.ProxyPassword)) == 1
}

func (a *App) serveProxy(w http.ResponseWriter, r *http.Request) {
	if !a.proxyAuthorized(r) {
		w.Header().Set("Proxy-Authenticate", `Basic realm="Rumiho"`)
		http.Error(w, "需要代理账号和密码", http.StatusProxyAuthRequired)
		return
	}
	if r.Method == "CONNECT" {
		a.connect(w, r)
		return
	}
	if !r.URL.IsAbs() || (r.URL.Scheme != "http" && r.URL.Scheme != "https") || r.URL.Hostname() == "" {
		http.Error(w, "无效的代理请求", 400)
		return
	}
	req := r.Clone(r.Context())
	req.RequestURI = ""
	req.Header.Del("Proxy-Authorization")
	removeHopHeaders(req.Header)
	resp, err := a.proxyTransport.RoundTrip(req)
	if err != nil {
		http.Error(w, "代理连接失败", 502)
		return
	}
	defer resp.Body.Close()
	removeHopHeaders(resp.Header)
	for key, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	io.Copy(flushWriter{w}, resp.Body)
}

type flushWriter struct{ http.ResponseWriter }

func (w flushWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
	return n, err
}

func removeHopHeaders(h http.Header) {
	for _, token := range strings.Split(h.Get("Connection"), ",") {
		h.Del(strings.TrimSpace(token))
	}
	for _, key := range []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		h.Del(key)
	}
}

func (a *App) connect(w http.ResponseWriter, r *http.Request) {
	if _, port, err := net.SplitHostPort(r.Host); err != nil || port == "" {
		http.Error(w, "CONNECT 必须包含目标主机和端口", 400)
		return
	}
	upstream, err := net.DialTimeout("tcp", a.o.CoreProxy, 10*time.Second)
	if err != nil {
		http.Error(w, "代理内核不可用", 502)
		return
	}
	defer upstream.Close()
	upstream.SetDeadline(time.Now().Add(15 * time.Second))
	s := a.snapshot()
	request := &http.Request{Method: "CONNECT", URL: r.URL, Host: r.Host, Header: http.Header{}}
	request.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(s.Admin.Username+":"+s.ProxyPassword)))
	if err = request.Write(upstream); err != nil {
		http.Error(w, "代理连接失败", 502)
		return
	}
	reader := bufio.NewReader(upstream)
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		http.Error(w, "代理连接失败", 502)
		return
	}
	if response.StatusCode != 200 {
		response.Body.Close()
		http.Error(w, "目标连接失败", 502)
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "当前协议不支持 CONNECT", 400)
		return
	}
	client, buffer, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer client.Close()
	upstream.SetDeadline(time.Time{})
	client.SetDeadline(time.Time{})
	a.tunnels.Store(client, upstream)
	defer a.tunnels.Delete(client)
	if _, err = fmt.Fprint(buffer, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if err = buffer.Flush(); err != nil {
		return
	}
	done := make(chan struct{}, 2)
	go func() { io.Copy(upstream, buffer); closeWrite(upstream); done <- struct{}{} }()
	go func() { io.Copy(client, reader); closeWrite(client); done <- struct{}{} }()
	<-done
	// Give a half-closed peer time to consume the response, while preventing leaked tunnels.
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

func closeWrite(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	}
}
func (a *App) closeTunnels() {
	a.tunnels.Range(func(k, v any) bool { k.(net.Conn).Close(); v.(net.Conn).Close(); return true })
}
