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
	if cfg.ProbeInterval != 5*time.Minute {
		t.Fatalf("probe interval: got %s", cfg.ProbeInterval)
	}
	if cfg.TLSWarnDays != 14 {
		t.Fatalf("tls warn days: got %d", cfg.TLSWarnDays)
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
	if err := os.WriteFile(path, []byte(`{"listen":":8080","data_dir":"`+dir+`"}`), 0o644); err != nil {
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
