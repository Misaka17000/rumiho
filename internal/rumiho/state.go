package rumiho

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

type Options struct {
	Listen, DataDir, CoreBinary, CoreProxy, CoreAPI, DashboardDir, RulesDir string
	ConfigFile, TLSCert, TLSKey                                             string
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func OptionsFromEnv() Options {
	return Options{
		Listen: env("LISTEN_ADDR", ":8080"), DataDir: env("DATA_DIR", "/config/cache"),
		CoreBinary: env("MIHOMO_BINARY", "/usr/local/bin/mihomo"),
		CoreProxy:  env("MIHOMO_PROXY_ADDR", "127.0.0.1:7890"), CoreAPI: env("MIHOMO_API_ADDR", "127.0.0.1:9090"),
		DashboardDir: env("DASHBOARD_DIR", "/app/dashboard"), RulesDir: env("RULES_DIR", "/app/rules"),
		ConfigFile: env("CONFIG_FILE", "/config/config.yaml"),
		TLSCert:    os.Getenv("TLS_CERT"), TLSKey: os.Getenv("TLS_KEY"),
	}
}

type Settings struct {
	SubscriptionURL string `json:"subscriptionURL" yaml:"subscription_url"`
	Mode            string `json:"mode" yaml:"mode"`
	UpdateHours     int    `json:"updateHours" yaml:"update_hours"`
	PublicURL       string `json:"publicURL" yaml:"public_url"`
}

type State struct {
	Admin         account          `json:"-" yaml:"admin"`
	ProxyPassword string           `json:"proxyPassword" yaml:"proxy_password"`
	CoreSecret    string           `json:"coreSecret" yaml:"core_secret"`
	Settings      Settings         `json:"settings" yaml:"settings"`
	Nodes         []map[string]any `json:"nodes" yaml:"nodes"`
	Routing       map[string]any   `json:"routing,omitempty" yaml:"routing,omitempty"`
	LastUpdated   time.Time        `json:"lastUpdated" yaml:"last_updated,omitempty"`
	LastError     string           `json:"lastError" yaml:"last_error,omitempty"`
}

type account struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

func validateAccount(a account) error {
	if strings.TrimSpace(a.Username) == "" || len(a.Username) > 256 || strings.ContainsAny(a.Username, ":\r\n") {
		return errors.New("账号不能为空或超过 256 字节，不能包含冒号或换行")
	}
	if utf8.RuneCountInString(a.Password) < 8 {
		return errors.New("登录密码至少需要 8 个字符")
	}
	if len(a.Password) > 4096 {
		return errors.New("登录密码不能超过 4096 字节")
	}
	return nil
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// One writable YAML file owns credentials and all application configuration.
// Only mihomo's generated runtime files and downloaded assets live in DataDir.
func loadState(o Options) (State, error) {
	s := State{Settings: Settings{Mode: "rule", UpdateHours: 24}, Nodes: []map[string]any{}}
	f, err := os.Open(o.ConfigFile)
	if err != nil {
		return s, fmt.Errorf("无法读取配置 %s，请检查 config.yaml 文件及部署目录挂载: %w", o.ConfigFile, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (32<<20)+1))
	if err != nil || len(data) > 32<<20 {
		return s, errors.New("配置读取失败或超过 32 MB")
	}
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(&s); err != nil {
		return s, errors.New("配置文件格式不正确，请检查字段和 YAML 格式")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return s, errors.New("配置文件只能包含一个 YAML 文档")
	}
	if err := validateAccount(s.Admin); err != nil {
		return s, err
	}
	changed := false
	// Upgrade the former account-only YAML plus state.json layout. Once the
	// unified file is saved it is authoritative; stale JSON is never reread.
	if s.CoreSecret == "" && s.ProxyPassword == "" {
		legacyData, legacyErr := os.ReadFile(filepath.Join(o.DataDir, "state.json"))
		if legacyErr == nil {
			var legacy State
			if json.Unmarshal(legacyData, &legacy) != nil || legacy.CoreSecret == "" || legacy.ProxyPassword == "" {
				return s, errors.New("旧 state.json 无效，未覆盖现有配置")
			}
			var fields map[string]yaml.Node
			if yaml.Unmarshal(data, &fields) != nil {
				return s, errors.New("无法读取配置字段")
			}
			legacy.Admin = s.Admin
			if _, explicit := fields["settings"]; explicit {
				legacy.Settings = s.Settings
			}
			s = legacy
			changed = true
		} else if !errors.Is(legacyErr, os.ErrNotExist) {
			return s, legacyErr
		}
	}
	if err := validateSettings(s.Settings); err != nil {
		return s, err
	}
	if s.ProxyPassword == "" {
		s.ProxyPassword = randomToken()
		changed = true
	}
	if s.CoreSecret == "" {
		s.CoreSecret = randomToken()
		changed = true
	}
	if changed {
		if err := saveState(o.ConfigFile, s); err != nil {
			return s, err
		}
	}
	return s, nil
}

func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".rumiho-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}

func saveState(path string, s State) error {
	data, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}

func validateSettings(s Settings) error {
	if s.Mode != "rule" && s.Mode != "global" && s.Mode != "direct" {
		return errors.New("请选择规则、全局代理或直连模式")
	}
	if s.UpdateHours < 1 || s.UpdateHours > 168 {
		return errors.New("自动更新间隔应为 1–168 小时")
	}
	if s.SubscriptionURL != "" {
		u, err := url.Parse(s.SubscriptionURL)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.Fragment != "" {
			return errors.New("订阅地址必须是有效的 HTTP/HTTPS URL")
		}
	}
	if s.PublicURL != "" {
		u, err := url.Parse(s.PublicURL)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("接入地址应为 http(s)://主机:端口，不包含路径或认证信息")
		}
		if u.Port() != "" {
			if _, err := net.LookupPort("tcp", u.Port()); err != nil {
				return errors.New("接入地址端口无效")
			}
		}
	}
	return nil
}
