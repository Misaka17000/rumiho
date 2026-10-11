package rumiho

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

func (a *App) prepareRules() error {
	dir := filepath.Join(a.o.DataDir, "rules")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	for _, name := range []string{"cn-domain.mrs", "cn-ip.mrs"} {
		// Use the version shipped with the image on each restart.
		data, err := os.ReadFile(filepath.Join(a.o.RulesDir, name))
		if err != nil {
			return fmt.Errorf("缺少内置分流规则 %s: %w", name, err)
		}
		if err = atomicWrite(filepath.Join(dir, name), data); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) renderConfig(s State) ([]byte, error) {
	_, port, _ := net.SplitHostPort(a.o.CoreProxy)
	p, err := strconv.Atoi(port)
	if err != nil {
		return nil, err
	}
	proxyName, autoName := fallbackGroupNames(s.Nodes)
	choices := []string{"DIRECT"}
	if len(s.Nodes) > 0 {
		choices = []string{autoName}
		for _, n := range s.Nodes {
			name, ok := n["name"].(string)
			if !ok || name == "" {
				return nil, errors.New("节点缺少有效的 name")
			}
			choices = append(choices, name)
		}
		choices = append(choices, "DIRECT")
	}
	groups := []map[string]any{{"name": proxyName, "type": "select", "proxies": choices}}
	if len(s.Nodes) > 0 {
		names := choices[1 : len(choices)-1]
		groups = append(groups, map[string]any{"name": autoName, "type": "url-test", "proxies": names,
			"url": "https://www.gstatic.com/generate_204", "interval": 300, "tolerance": 50, "lazy": true})
	}
	rules := []string{"DOMAIN,localhost,DIRECT", "DOMAIN-SUFFIX,local,DIRECT", "DOMAIN-SUFFIX,lan,DIRECT",
		"IP-CIDR,127.0.0.0/8,DIRECT,no-resolve", "IP-CIDR,10.0.0.0/8,DIRECT,no-resolve", "IP-CIDR,172.16.0.0/12,DIRECT,no-resolve",
		"IP-CIDR,192.168.0.0/16,DIRECT,no-resolve", "IP-CIDR,169.254.0.0/16,DIRECT,no-resolve",
		"IP-CIDR6,::1/128,DIRECT,no-resolve", "IP-CIDR6,fc00::/7,DIRECT,no-resolve",
		"RULE-SET,cn-domain,DIRECT", "RULE-SET,cn-ip,DIRECT", "MATCH," + primaryGroup(s)}
	c := map[string]any{"port": p, "bind-address": "127.0.0.1", "allow-lan": false,
		"authentication": []string{s.Admin.Username + ":" + s.ProxyPassword}, "skip-auth-prefixes": []string{},
		"external-controller": a.o.CoreAPI, "secret": s.CoreSecret, "mode": s.Settings.Mode,
		"log-level": "warning", "ipv6": false, "find-process-mode": "off",
		"profile": map[string]any{"store-selected": true}, "proxies": s.Nodes, "proxy-groups": groups, "rules": rules,
		"rule-providers": map[string]any{
			"cn-domain": map[string]any{"type": "file", "behavior": "domain", "format": "mrs", "path": "./rules/cn-domain.mrs"},
			"cn-ip":     map[string]any{"type": "file", "behavior": "ipcidr", "format": "mrs", "path": "./rules/cn-ip.mrs"},
		},
	}
	// Only routing data crosses the subscription boundary. Listeners, controller,
	// authentication, DNS and TUN remain owned by this instance.
	for _, key := range routingKeys {
		if value, ok := s.Routing[key]; ok {
			if key == "rule-providers" && !hasSubscriptionRules(s) {
				continue
			}
			c[key] = value
		}
	}
	if hasSubscriptionRules(s) && s.Routing["rule-providers"] == nil {
		delete(c, "rule-providers")
	}
	return yaml.Marshal(c)
}

func (a *App) validateConfig(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.o.CoreBinary, "-t", "-d", a.o.DataDir, "-f", path)
	if _, err := cmd.CombinedOutput(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errors.New("mihomo 配置校验超时，首次导入的 Geo 数据或规则集可能尚未下载完成，请稍后重试")
		}
		return errors.New("mihomo 配置校验失败，请检查节点、分组、规则及 Geo 数据下载是否可用")
	}
	return nil
}

// Caller holds changeMu. Keep the old running config if any part of the change fails.
func (a *App) commit(ctx context.Context, next State) error {
	old := a.snapshot()
	data, err := a.renderConfig(next)
	if err != nil {
		return err
	}
	active := filepath.Join(a.o.DataDir, "mihomo.yaml")
	previous, err := os.ReadFile(active)
	if err != nil {
		return err
	}
	// Public URL, refresh interval and a refresh with identical routing only
	// change persisted metadata. Keep the running core and connections intact.
	if bytes.Equal(previous, data) {
		if err = saveState(a.o.ConfigFile, next); err != nil {
			return errors.New("无法保存设置")
		}
		a.replaceState(next)
		return nil
	}
	candidate := filepath.Join(a.o.DataDir, "candidate.yaml")
	if err = atomicWrite(candidate, data); err != nil {
		return err
	}
	defer os.Remove(candidate)
	if err = a.validateConfig(ctx, candidate); err != nil {
		return err
	}
	if err = atomicWrite(active, data); err != nil {
		return err
	}
	rollback := func() {
		if err := atomicWrite(active, previous); err != nil {
			logRollback(err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := a.coreRequest(ctx, "PUT", "/configs?force=true", map[string]string{"path": active}, nil); err != nil {
			logRollback(err)
		}
	}
	if err = a.coreRequest(ctx, "PUT", "/configs?force=true", map[string]string{"path": active}, nil); err != nil {
		rollback()
		return err
	}
	// The empty bootstrap configuration selects DIRECT. Switch to AUTO on the
	// first import rather than inheriting that placeholder selection forever.
	firstImport := len(old.Nodes) == 0 && len(next.Nodes) > 0
	if firstImport && len(subscriptionGroups(next)) == 0 {
		proxyName, autoName := fallbackGroupNames(next.Nodes)
		if err = a.coreRequest(ctx, "PUT", "/proxies/"+url.PathEscape(proxyName), map[string]string{"name": autoName}, nil); err != nil {
			rollback()
			return err
		}
	}
	if len(next.Nodes) > 0 && (firstImport || primaryGroup(old) != primaryGroup(next) || (old.Settings.Mode != "global" && next.Settings.Mode == "global")) {
		if err = a.coreRequest(ctx, "PUT", "/proxies/GLOBAL", map[string]string{"name": primaryGroup(next)}, nil); err != nil {
			rollback()
			return err
		}
	}
	if err = a.coreRequest(ctx, "GET", "/version", nil, nil); err != nil {
		rollback()
		return err
	}
	if err = saveState(a.o.ConfigFile, next); err != nil {
		rollback()
		return errors.New("无法保存设置，已恢复原配置")
	}
	a.replaceState(next)
	a.proxyTransport.CloseIdleConnections()
	return nil
}

func logRollback(err error) { fmt.Fprintf(os.Stderr, "恢复配置失败: %v\n", err) }

func (a *App) refresh(ctx context.Context) error {
	a.changeMu.Lock()
	defer a.changeMu.Unlock()
	s := a.snapshot()
	if s.Settings.SubscriptionURL == "" {
		return errors.New("请先填写订阅地址")
	}
	sub, err := a.fetchSubscription(ctx, s.Settings.SubscriptionURL)
	if err == nil {
		s.Nodes, s.Routing, s.LastUpdated, s.LastError = sub.Nodes, sub.Routing, time.Now(), ""
		err = a.commit(ctx, s)
	}
	if err != nil {
		s = a.snapshot()
		s.LastError = err.Error()
		if saveState(a.o.ConfigFile, s) == nil {
			a.replaceState(s)
		}
	}
	return err
}

func (a *App) schedule(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	nextTry := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		s := a.snapshot()
		if s.Settings.SubscriptionURL == "" || time.Now().Before(nextTry) || time.Since(s.LastUpdated) < time.Duration(s.Settings.UpdateHours)*time.Hour {
			continue
		}
		if a.refresh(ctx) != nil {
			nextTry = time.Now().Add(5 * time.Minute)
		}
	}
}
