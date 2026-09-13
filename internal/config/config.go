// Package config loads service settings from a JSON file and environment variables.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	DefaultListen        = ":8080"
	DefaultDataDir       = "./data"
	DefaultProbeInterval = 5 * time.Minute
	DefaultTLSWarnDays   = 14
	DefaultCooldown      = time.Hour
	DefaultSMTPPort      = 587
)

// Config is the runtime configuration for the service.
type Config struct {
	Listen        string
	DataDir       string
	ProbeInterval time.Duration
	TLSWarnDays   int
	Alert         Alert
}

// Alert holds notification settings for webhook and optional SMTP.
type Alert struct {
	WebhookURL string
	SMTPHost   string
	SMTPPort   int
	SMTPUser   string
	SMTPPass   string
	SMTPFrom   string
	To         string
	Cooldown   time.Duration
}

type fileConfig struct {
	Listen        string    `json:"listen"`
	DataDir       string    `json:"data_dir"`
	ProbeInterval string    `json:"probe_interval"`
	TLSWarnDays   int       `json:"tls_warn_days"`
	Alert         fileAlert `json:"alert"`
}

type fileAlert struct {
	WebhookURL string `json:"webhook_url"`
	SMTPHost   string `json:"smtp_host"`
	SMTPPort   int    `json:"smtp_port"`
	SMTPUser   string `json:"smtp_user"`
	SMTPPass   string `json:"smtp_password"`
	SMTPFrom   string `json:"smtp_from"`
	To         string `json:"to"`
	Cooldown   string `json:"cooldown"`
}

// Defaults matches the intended MVP load: 50–200 URLs every 5 minutes
// on a 1 vCPU / 512 MB–1 GB box.
func Defaults() Config {
	return Config{
		Listen:        DefaultListen,
		DataDir:       DefaultDataDir,
		ProbeInterval: DefaultProbeInterval,
		TLSWarnDays:   DefaultTLSWarnDays,
		Alert: Alert{
			SMTPPort: DefaultSMTPPort,
			Cooldown: DefaultCooldown,
		},
	}
}

// Load reads OSH_CONFIG (JSON) if set, otherwise configs/config.example.json
// when that file exists. Environment variables override file values:
//
//	OSH_LISTEN, OSH_DATA_DIR, OSH_PROBE_INTERVAL, OSH_TLS_WARN_DAYS,
//	OSH_WEBHOOK_URL, OSH_ALERT_COOLDOWN, OSH_SMTP_HOST, OSH_SMTP_PORT,
//	OSH_SMTP_USER, OSH_SMTP_PASSWORD, OSH_SMTP_FROM, OSH_ALERT_TO
func Load() (Config, error) {
	cfg := Defaults()

	path := os.Getenv("OSH_CONFIG")
	if path == "" {
		if _, err := os.Stat("configs/config.example.json"); err == nil {
			path = "configs/config.example.json"
		}
	}
	if path != "" {
		if err := loadFile(path, &cfg); err != nil {
			return Config{}, err
		}
	}

	if err := applyEnv(&cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func loadFile(path string, cfg *Config) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config %s: %w", path, err)
	}
	var fc fileConfig
	if err := json.Unmarshal(raw, &fc); err != nil {
		return fmt.Errorf("parse config %s: %w", path, err)
	}
	if fc.Listen != "" {
		cfg.Listen = fc.Listen
	}
	if fc.DataDir != "" {
		cfg.DataDir = fc.DataDir
	}
	if fc.ProbeInterval != "" {
		d, err := time.ParseDuration(fc.ProbeInterval)
		if err != nil {
			return fmt.Errorf("probe_interval: %w", err)
		}
		cfg.ProbeInterval = d
	}
	if fc.TLSWarnDays != 0 {
		cfg.TLSWarnDays = fc.TLSWarnDays
	}
	if fc.Alert.WebhookURL != "" {
		cfg.Alert.WebhookURL = fc.Alert.WebhookURL
	}
	if fc.Alert.SMTPHost != "" {
		cfg.Alert.SMTPHost = fc.Alert.SMTPHost
	}
	if fc.Alert.SMTPPort != 0 {
		cfg.Alert.SMTPPort = fc.Alert.SMTPPort
	}
	if fc.Alert.SMTPUser != "" {
		cfg.Alert.SMTPUser = fc.Alert.SMTPUser
	}
	if fc.Alert.SMTPPass != "" {
		cfg.Alert.SMTPPass = fc.Alert.SMTPPass
	}
	if fc.Alert.SMTPFrom != "" {
		cfg.Alert.SMTPFrom = fc.Alert.SMTPFrom
	}
	if fc.Alert.To != "" {
		cfg.Alert.To = fc.Alert.To
	}
	if fc.Alert.Cooldown != "" {
		d, err := time.ParseDuration(fc.Alert.Cooldown)
		if err != nil {
			return fmt.Errorf("alert.cooldown: %w", err)
		}
		cfg.Alert.Cooldown = d
	}
	return nil
}

func applyEnv(cfg *Config) error {
	if v := os.Getenv("OSH_LISTEN"); v != "" {
		cfg.Listen = v
	}
	if v := os.Getenv("OSH_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("OSH_PROBE_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("OSH_PROBE_INTERVAL: %w", err)
		}
		cfg.ProbeInterval = d
	}
	if v := os.Getenv("OSH_TLS_WARN_DAYS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("OSH_TLS_WARN_DAYS: %w", err)
		}
		cfg.TLSWarnDays = n
	}
	if v := os.Getenv("OSH_WEBHOOK_URL"); v != "" {
		cfg.Alert.WebhookURL = v
	}
	if v := os.Getenv("OSH_ALERT_COOLDOWN"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("OSH_ALERT_COOLDOWN: %w", err)
		}
		cfg.Alert.Cooldown = d
	}
	if v := os.Getenv("OSH_SMTP_HOST"); v != "" {
		cfg.Alert.SMTPHost = v
	}
	if v := os.Getenv("OSH_SMTP_PORT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("OSH_SMTP_PORT: %w", err)
		}
		cfg.Alert.SMTPPort = n
	}
	if v := os.Getenv("OSH_SMTP_USER"); v != "" {
		cfg.Alert.SMTPUser = v
	}
	if v := os.Getenv("OSH_SMTP_PASSWORD"); v != "" {
		cfg.Alert.SMTPPass = v
	}
	if v := os.Getenv("OSH_SMTP_FROM"); v != "" {
		cfg.Alert.SMTPFrom = v
	}
	if v := os.Getenv("OSH_ALERT_TO"); v != "" {
		cfg.Alert.To = v
	}
	return nil
}

// Validate checks values that would make the process unusable.
func (c Config) Validate() error {
	if c.Listen == "" {
		return fmt.Errorf("listen address is empty")
	}
	if c.DataDir == "" {
		return fmt.Errorf("data_dir is empty")
	}
	if c.ProbeInterval <= 0 {
		return fmt.Errorf("probe_interval must be > 0")
	}
	if c.TLSWarnDays < 0 {
		return fmt.Errorf("tls_warn_days must be >= 0")
	}
	if c.Alert.Cooldown < 0 {
		return fmt.Errorf("alert.cooldown must be >= 0")
	}
	return nil
}
