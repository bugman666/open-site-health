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

func TestValidateRejectsBadInterval(t *testing.T) {
	cfg := Defaults()
	cfg.ProbeInterval = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error")
	}
}
