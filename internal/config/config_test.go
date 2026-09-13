package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	if cfg.Listen != DefaultListen {
		t.Fatalf("listen: got %q", cfg.Listen)
	}
	if cfg.Listen != "127.0.0.1:8080" {
		t.Fatalf("default listen should be loopback: got %q", cfg.Listen)
	}
	if cfg.ProbeInterval != 5*time.Minute {
		t.Fatalf("probe interval: got %s", cfg.ProbeInterval)
	}
	if cfg.TLSWarnDays != 14 {
		t.Fatalf("tls warn days: got %d", cfg.TLSWarnDays)
	}
	if cfg.APIToken != "" || cfg.AllowPrivateTargets {
		t.Fatalf("auth/ssrf defaults: token=%q allowPrivate=%v", cfg.APIToken, cfg.AllowPrivateTargets)
	}
}

func TestLoadFileAndEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{
  "listen": ":9090",
  "data_dir": "/tmp/osh-test",
  "probe_interval": "2m",
  "tls_warn_days": 7,
  "alert": {"cooldown": "30m"}
}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("OSH_CONFIG", path)
	t.Setenv("OSH_LISTEN", "127.0.0.1:18080")
	t.Setenv("OSH_PROBE_INTERVAL", "10s")
	t.Setenv("OSH_DATA_DIR", "")
	t.Setenv("OSH_TLS_WARN_DAYS", "")
	t.Setenv("OSH_WEBHOOK_URL", "")
	t.Setenv("OSH_ALERT_COOLDOWN", "")
	t.Setenv("OSH_SMTP_HOST", "")
	t.Setenv("OSH_SMTP_PASSWORD", "")
	t.Setenv("OSH_ALERT_TO", "")
	t.Setenv("OSH_API_TOKEN", "")
	t.Setenv("OSH_ALLOW_PRIVATE_TARGETS", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:18080" {
		t.Fatalf("env should win listen: got %q", cfg.Listen)
	}
	if cfg.DataDir != "/tmp/osh-test" {
		t.Fatalf("data_dir: got %q", cfg.DataDir)
	}
	if cfg.ProbeInterval != 10*time.Second {
		t.Fatalf("probe interval: got %s", cfg.ProbeInterval)
	}
	if cfg.TLSWarnDays != 7 {
		t.Fatalf("tls warn days: got %d", cfg.TLSWarnDays)
	}
	if cfg.Alert.Cooldown != 30*time.Minute {
		t.Fatalf("cooldown: got %s", cfg.Alert.Cooldown)
	}
}

func TestAlertEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"listen":"127.0.0.1:8080","data_dir":"`+dir+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OSH_CONFIG", path)
	t.Setenv("OSH_WEBHOOK_URL", "http://127.0.0.1:9/hook")
	t.Setenv("OSH_ALERT_COOLDOWN", "15m")
	t.Setenv("OSH_SMTP_HOST", "smtp.example.org")
	t.Setenv("OSH_SMTP_PORT", "2525")
	t.Setenv("OSH_SMTP_USER", "osh")
	t.Setenv("OSH_SMTP_PASSWORD", "secret")
	t.Setenv("OSH_SMTP_FROM", "osh@example.org")
	t.Setenv("OSH_ALERT_TO", "ops@example.org")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Alert.WebhookURL != "http://127.0.0.1:9/hook" {
		t.Fatalf("webhook: %s", cfg.Alert.WebhookURL)
	}
	if cfg.Alert.Cooldown != 15*time.Minute {
		t.Fatalf("cooldown: %s", cfg.Alert.Cooldown)
	}
	if cfg.Alert.SMTPHost != "smtp.example.org" || cfg.Alert.SMTPPort != 2525 {
		t.Fatalf("smtp: %s:%d", cfg.Alert.SMTPHost, cfg.Alert.SMTPPort)
	}
	if cfg.Alert.SMTPPass != "secret" || cfg.Alert.To != "ops@example.org" {
		t.Fatalf("smtp creds: user=%s to=%s", cfg.Alert.SMTPUser, cfg.Alert.To)
	}
}

func TestValidateRejectsBadInterval(t *testing.T) {
	cfg := Defaults()
	cfg.ProbeInterval = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateRequiresTokenForNonLoopback(t *testing.T) {
	cfg := Defaults()
	cfg.Listen = ":8080"
	cfg.APIToken = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("wide listen without token should fail")
	}

	cfg.APIToken = "secret"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}

	cfg.Listen = "0.0.0.0:8080"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}

	cfg.APIToken = ""
	cfg.Listen = "127.0.0.1:8080"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("loopback without token should be ok: %v", err)
	}

	cfg.Listen = "[::1]:8080"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("ipv6 loopback without token should be ok: %v", err)
	}

	cfg.Listen = "localhost:8080"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("localhost without token should be ok: %v", err)
	}
}

func TestListenIsLoopback(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:8080": true,
		"[::1]:8080":     true,
		"localhost:8080": true,
		":8080":          false,
		"0.0.0.0:8080":   false,
		"[::]:8080":      false,
		"":               false,
	}
	for addr, want := range cases {
		if got := ListenIsLoopback(addr); got != want {
			t.Fatalf("ListenIsLoopback(%q)=%v want %v", addr, got, want)
		}
	}
}

func TestLoadTokenAndPrivateOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"listen":":9090","data_dir":"`+dir+`","api_token":"file-token"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OSH_CONFIG", path)
	t.Setenv("OSH_LISTEN", "")
	t.Setenv("OSH_API_TOKEN", "env-token")
	t.Setenv("OSH_ALLOW_PRIVATE_TARGETS", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIToken != "env-token" {
		t.Fatalf("token: %q", cfg.APIToken)
	}
	if !cfg.AllowPrivateTargets {
		t.Fatal("expected allow_private_targets from env")
	}
}

func TestLoadRejectsWideListenWithoutToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"listen":":8080","data_dir":"`+dir+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OSH_CONFIG", path)
	t.Setenv("OSH_LISTEN", "")
	t.Setenv("OSH_API_TOKEN", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error")
	}
}
