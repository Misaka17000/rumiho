package rumiho

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Full profiles retain their routing graph; this is not a proxy-provider parser.
// Reference behavior and deliberately unsupported formats: docs/SUBSCRIPTIONS.md.
var routingKeys = []string{"proxy-groups", "rules", "rule-providers", "geodata-mode", "geodata-loader", "geo-auto-update", "geo-update-interval", "geox-url"}

type subscription struct {
	Nodes   []map[string]any
	Routing map[string]any
}

func hasSubscriptionRules(s State) bool { return s.Routing["rules"] != nil }

func subscriptionGroups(s State) []map[string]any {
	// The same representation is used after parsing YAML and reloading JSON.
	var groups []map[string]any
	if values, ok := s.Routing["proxy-groups"].([]any); ok {
		for _, raw := range values {
			if group, ok := raw.(map[string]any); ok {
				groups = append(groups, group)
			}
		}
	}
	return groups
}

func fallbackGroupNames(nodes []map[string]any) (string, string) {
	seen := map[string]bool{}
	for _, n := range nodes {
		name, _ := n["name"].(string)
		seen[name] = true
	}
	unique := func(base string) string {
		name := base
		for i := 2; seen[name]; i++ {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		return name
	}
	return unique("PROXY"), unique("AUTO")
}

func primaryGroup(s State) string {
	if groups := subscriptionGroups(s); len(groups) > 0 {
		if name, ok := groups[0]["name"].(string); ok && name != "" {
			return name
		}
	}
	name, _ := fallbackGroupNames(s.Nodes)
	return name
}

func parseSubscription(data []byte) (*subscription, error) {
	var root any
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&root); err != nil {
		return nil, errors.New("订阅不是有效的 YAML 配置")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("订阅必须只包含一个 YAML 文档")
	}
	var values []any
	routing := map[string]any{}
	switch v := root.(type) {
	case map[string]any:
		values, _ = v["proxies"].([]any)
		if providers, exists := v["proxy-providers"]; exists && providers != nil {
			if m, ok := providers.(map[string]any); !ok || len(m) > 0 {
				return nil, errors.New("暂不支持 proxy-providers，请使用内含 proxies 节点的订阅")
			}
		}
		for _, key := range routingKeys {
			if value, ok := v[key]; ok && value != nil {
				routing[key] = value
			}
		}
	case []any:
		values = v
	}
	if len(values) == 0 {
		return nil, errors.New("订阅中没有 proxies 节点；目前支持 Clash/mihomo YAML 节点列表")
	}
	if len(values) > 2048 {
		return nil, errors.New("订阅最多支持 2048 个节点")
	}
	seen := map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "GLOBAL": true, "PASS": true, "COMPATIBLE": true}
	nodes := make([]map[string]any, 0, len(values))
	for _, raw := range values {
		n, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("节点格式不正确")
		}
		name, _ := n["name"].(string)
		kind, _ := n["type"].(string)
		if strings.TrimSpace(name) == "" || len(name) > 256 || kind == "" {
			return nil, errors.New("每个节点必须有有效的 name 和 type")
		}
		if seen[name] {
			return nil, fmt.Errorf("节点名称重复或与内置名称冲突：%s", name)
		}
		seen[name] = true
		nodes = append(nodes, n)
	}
	if raw, exists := routing["proxy-groups"]; exists {
		groups, ok := raw.([]any)
		if !ok || len(groups) > 512 {
			return nil, errors.New("proxy-groups 必须是分组列表，最多支持 512 个分组")
		}
		if len(groups) == 0 {
			delete(routing, "proxy-groups")
		}
		for _, raw := range groups {
			g, ok := raw.(map[string]any)
			if !ok {
				return nil, errors.New("订阅分组格式不正确")
			}
			name, _ := g["name"].(string)
			kind, _ := g["type"].(string)
			if strings.TrimSpace(name) == "" || len(name) > 256 || kind == "" || seen[name] {
				return nil, errors.New("分组必须有有效且不重复的 name 和 type")
			}
			seen[name] = true
		}
	}
	if raw, exists := routing["rules"]; exists {
		rules, ok := raw.([]any)
		if !ok {
			return nil, errors.New("rules 必须是规则列表")
		}
		if len(rules) == 0 {
			delete(routing, "rules")
		}
		for _, raw := range rules {
			if rule, ok := raw.(string); !ok || strings.TrimSpace(rule) == "" {
				return nil, errors.New("订阅规则必须是非空字符串")
			}
		}
	}
	if raw, exists := routing["rule-providers"]; exists {
		providers, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("rule-providers 格式不正确")
		}
		for name, raw := range providers {
			provider, ok := raw.(map[string]any)
			if !ok || (provider["type"] != "http" && provider["type"] != "inline") {
				return nil, errors.New("订阅规则集仅支持 http 或 inline 类型")
			}
			if provider["type"] == "http" {
				if !httpAddress(provider["url"]) {
					return nil, errors.New("规则集地址必须是 HTTP/HTTPS URL")
				}
				// Never let a subscription pick a local file to read or overwrite.
				delete(provider, "path")
				encoded, _ := yaml.Marshal(provider)
				digest := sha256.Sum256(append([]byte(name+"\n"), encoded...))
				provider["path"] = fmt.Sprintf("./providers/rules/%x", digest[:16])
			} else {
				delete(provider, "path")
			}
		}
	}
	if raw, exists := routing["geox-url"]; exists {
		urls, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("geox-url 格式不正确")
		}
		for _, address := range urls {
			if !httpAddress(address) {
				return nil, errors.New("Geo 数据地址必须是 HTTP/HTTPS URL")
			}
		}
	}
	return &subscription{Nodes: nodes, Routing: routing}, nil
}

func httpAddress(value any) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && u.Hostname() != "" && (u.Scheme == "http" || u.Scheme == "https")
}

func (a *App) fetchSubscription(ctx context.Context, address string) (*subscription, error) {
	direct := &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 15 * time.Second, IdleConnTimeout: 30 * time.Second}
	defer direct.CloseIdleConnections()
	transports := []http.RoundTripper{direct}
	if len(a.snapshot().Nodes) > 0 {
		transports = append(transports, a.proxyTransport)
	}
	var lastErr error
	for _, tr := range transports {
		sub, err := downloadSubscription(ctx, address, tr)
		if err == nil {
			return sub, nil
		}
		lastErr = err
		// A successful HTTP response may still be a captive-portal page or a
		// truncated download. Retry the entire fetch/parse with the next route.
		if ctx.Err() != nil {
			break
		}
	}
	return nil, lastErr
}

func downloadSubscription(ctx context.Context, address string, transport http.RoundTripper) (*subscription, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
	if err != nil {
		return nil, errors.New("订阅地址无效")
	}
	req.Header.Set("User-Agent", "Clash.Meta/rumiho")
	client := &http.Client{Timeout: 25 * time.Second, Transport: transport}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("订阅下载失败，请检查地址或网络连接")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("订阅下载失败，HTTP %d", resp.StatusCode)
	}
	const maxSize = 8 << 20
	if resp.ContentLength > maxSize {
		return nil, errors.New("订阅不能超过 8 MB")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return nil, errors.New("订阅下载不完整")
	}
	if len(data) > maxSize {
		return nil, errors.New("订阅不能超过 8 MB")
	}
	return parseSubscription(data)
}
