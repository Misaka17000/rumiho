package rumiho

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPasswordLength(t *testing.T) {
	for _, tc := range []struct {
		name, password string
		valid          bool
	}{
		{"seven ASCII", "abc123!", false},
		{"eight ASCII", "abc123!?", true},
		{"seven Chinese", "一二三四五六七", false},
		{"eight Chinese", "一二三四五六七八", true},
		{"seven emoji", strings.Repeat("🌟", 7), false},
		{"eight emoji", strings.Repeat("🌟", 8), true},
		{"byte limit", strings.Repeat("a", 4096), true},
		{"over byte limit", strings.Repeat("a", 4097), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAccount(account{Username: "admin", Password: tc.password})
			if (err == nil) != tc.valid {
				t.Fatalf("valid = %v, want %v", err == nil, tc.valid)
			}
		})
	}
}

func TestUnifiedConfigAndPasswordChange(t *testing.T) {
	o := testOptions(t)
	a, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a)
	defer server.Close()
	client := loginClient(t, server.URL)
	original := a.snapshot()
	resp, body := apiCall(t, client, server.URL, "GET", "/api/state", nil)
	if resp.StatusCode != 200 || bytes.Contains(body, []byte(original.Admin.Password)) {
		t.Fatal("administrator password exposed or state request failed")
	}
	var state struct {
		Access map[string]string `json:"access"`
	}
	if err := json.Unmarshal(body, &state); err != nil || len(state.Access) != 2 {
		t.Fatal("unexpected HTTP access fields", err)
	}
	proxyURL, err := url.Parse(state.Access["proxyURL"])
	if err != nil || proxyURL.User.Username() != original.Admin.Username {
		t.Fatal("proxy URL did not use login username")
	}
	resp, _ = apiCall(t, server.Client(), server.URL, "GET", "/sub/obsolete.yaml", nil)
	if resp.StatusCode != 404 {
		t.Fatal("removed subscription endpoint is still available")
	}
	resp, _ = apiCall(t, client, server.URL, "POST", "/api/password", map[string]string{"currentPassword": "wrong", "newPassword": "updated-plain-password"})
	if resp.StatusCode != 400 {
		t.Fatal("accepted incorrect current password")
	}
	resp, _ = apiCall(t, client, server.URL, "POST", "/api/password", map[string]string{"currentPassword": original.Admin.Password, "newPassword": "short-7"})
	if resp.StatusCode != 400 || a.snapshot().Admin.Password != original.Admin.Password {
		t.Fatal("short password accepted or changed the account")
	}
	resp, _ = apiCall(t, client, server.URL, "POST", "/api/password", map[string]string{"currentPassword": original.Admin.Password, "newPassword": "new-1234"})
	if resp.StatusCode != 200 {
		t.Fatal("password change failed")
	}
	resp, _ = apiCall(t, client, server.URL, "GET", "/api/state", nil)
	if resp.StatusCode != 401 {
		t.Fatal("password change did not revoke existing sessions")
	}
	saved, err := loadState(o)
	if err != nil || saved.Admin.Password != "new-1234" || saved.ProxyPassword != original.ProxyPassword {
		t.Fatal("new password not persisted or changed proxy password", err)
	}
	resp, _ = apiCall(t, server.Client(), server.URL, "POST", "/api/login", map[string]string{"username": "admin", "password": original.Admin.Password})
	if resp.StatusCode != 401 {
		t.Fatal("old password still accepted")
	}
	resp, _ = apiCall(t, server.Client(), server.URL, "POST", "/api/login", map[string]string{"username": "admin", "password": saved.Admin.Password})
	if resp.StatusCode != 200 {
		t.Fatal("new password requires an application restart")
	}
	// Editing the same file and restarting also updates the proxy username.
	saved.Admin.Username = "owner"
	if err := saveState(o.ConfigFile, saved); err != nil {
		t.Fatal(err)
	}
	b, err := New(o)
	if err != nil || !reflect.DeepEqual(saved, b.snapshot()) {
		t.Fatal("unified config not retained on restart", err)
	}
	nextServer := httptest.NewServer(b)
	defer nextServer.Close()
	resp, _ = apiCall(t, nextServer.Client(), nextServer.URL, "POST", "/api/login", map[string]string{"username": "owner", "password": saved.Admin.Password})
	if resp.StatusCode != 200 {
		t.Fatal("file-edited credentials not applied")
	}
	config, err := b.renderConfig(b.snapshot())
	if err != nil || !bytes.Contains(config, []byte("owner:"+saved.ProxyPassword)) {
		t.Fatal("core authentication did not follow login username", err)
	}
	if _, err := os.Stat(filepath.Join(o.DataDir, "state.json")); !os.IsNotExist(err) {
		t.Fatal("created separate application state")
	}
	data, err := os.ReadFile(o.ConfigFile)
	if err != nil || !bytes.Contains(data, []byte(saved.Admin.Password)) {
		t.Fatal("password not stored as plaintext in config.yaml", err)
	}
}

func TestPasswordWriteFailureKeepsAccountAndSession(t *testing.T) {
	o := testOptions(t)
	a, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a)
	defer server.Close()
	client := loginClient(t, server.URL)
	old := a.snapshot()
	if err := os.Remove(o.ConfigFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(o.ConfigFile, 0700); err != nil {
		t.Fatal(err)
	}
	resp, _ := apiCall(t, client, server.URL, "POST", "/api/password", map[string]string{"currentPassword": old.Admin.Password, "newPassword": "not-saved-password"})
	if resp.StatusCode != 500 || !reflect.DeepEqual(old, a.snapshot()) {
		t.Fatal("failed file write changed the account")
	}
	resp, _ = apiCall(t, client, server.URL, "GET", "/api/state", nil)
	if resp.StatusCode != 200 {
		t.Fatal("failed password change revoked the session")
	}
}

func TestUnifiedConfigValidationAndLegacyMigration(t *testing.T) {
	for _, content := range []string{"", "admin: {}", "admin: {username: admin, password: short}", "admin: {username: 'bad:name', password: long-enough-password}", "admin: {username: admin, passwrod: secret-not-for-logs}", "admin: {}\n---\nadmin: {}"} {
		o := testOptions(t)
		if err := os.WriteFile(o.ConfigFile, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := New(o); err == nil || bytes.Contains([]byte(err.Error()), []byte("secret-not-for-logs")) {
			t.Fatal("invalid config accepted or secret exposed")
		}
	}
	o := testOptions(t)
	a, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	old := a.snapshot()
	old.Settings.PublicURL = "http://192.168.1.10:8080"
	data, _ := json.Marshal(old)
	var legacy map[string]any
	if err := json.Unmarshal(data, &legacy); err != nil {
		t.Fatal(err)
	}
	legacy["adminUsername"], legacy["adminHash"], legacy["shareToken"], legacy["proxyUsername"] = "obsolete", "old-hash", "old-token", "proxy"
	data, _ = json.Marshal(legacy)
	legacyPath := filepath.Join(o.DataDir, "state.json")
	if err := os.WriteFile(legacyPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(o.ConfigFile, []byte("admin:\n  username: admin\n  password: test-admin-password-123\n"), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := New(o)
	if err != nil || !reflect.DeepEqual(old, b.snapshot()) {
		t.Fatal("legacy migration lost configuration", err)
	}
	migrated, err := loadState(o)
	if err != nil || !reflect.DeepEqual(old, migrated) {
		t.Fatal("migration was not written to YAML", err)
	}
	// Once migrated, stale legacy JSON must not become a second source of truth.
	if err := os.WriteFile(legacyPath, []byte("invalid stale JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(o); err != nil {
		t.Fatal("stale JSON read after migration", err)
	}
	data, _ = os.ReadFile(o.ConfigFile)
	for _, removed := range []string{"adminHash", "shareToken", "proxyUsername"} {
		if bytes.Contains(data, []byte(removed)) {
			t.Fatal("legacy field retained", removed)
		}
	}
}
