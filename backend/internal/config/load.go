package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func Load(path string) (*RuntimeConfig, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("config path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	applyDefaults(&cfg)
	if err := validate(&cfg); err != nil {
		return nil, err
	}

	if cfg.Database.Path != ":memory:" && !strings.HasPrefix(cfg.Database.Path, "file:") {
		absDB, err := filepath.Abs(cfg.Database.Path)
		if err == nil {
			cfg.Database.Path = absDB
		}
	}

	return &RuntimeConfig{
		Config:      cfg,
		ApprovalTTL: time.Duration(cfg.Mail.ApprovalTTLHour) * time.Hour,
	}, nil
}

func applyDefaults(cfg *Config) {
	if cfg.App.ListenAddr == "" {
		cfg.App.ListenAddr = "127.0.0.1:8080"
	}
	if cfg.App.BaseURL == "" {
		cfg.App.BaseURL = "http://127.0.0.1:8080"
	}
	if cfg.App.StaticDir == "" {
		cfg.App.StaticDir = "./public"
	}
	if cfg.Database.Driver == "" {
		cfg.Database.Driver = "sqlite"
	}
	if cfg.Database.Path == "" {
		cfg.Database.Path = "./giftcode.db"
	}
	if cfg.Sync.IntervalSeconds <= 0 {
		cfg.Sync.IntervalSeconds = 300
	}
	if cfg.AuxScheduler.IntervalSeconds <= 0 {
		cfg.AuxScheduler.IntervalSeconds = 30
	}
	if cfg.Mail.SubjectPrefix == "" {
		cfg.Mail.SubjectPrefix = "[sub2api-giftcode]"
	}
	if cfg.Mail.ApprovalTTLHour <= 0 {
		cfg.Mail.ApprovalTTLHour = 72
	}
}

func validate(cfg *Config) error {
	switch {
	case cfg.Sub2API.BaseURL == "":
		return errors.New("sub2api.base_url is required")
	case cfg.Sub2API.AdminAPIKey == "":
		return errors.New("sub2api.admin_api_key is required")
	case cfg.Session.CookieSecret == "":
		return errors.New("session.cookie_secret is required")
	case cfg.Mail.SMTPHost == "":
		return errors.New("mail.smtp_host is required")
	case cfg.Mail.SMTPPort <= 0:
		return errors.New("mail.smtp_port is required")
	case cfg.Mail.FromAddress == "":
		return errors.New("mail.from_address is required")
	case cfg.Mail.AdminToAddress == "":
		return errors.New("mail.admin_to_address is required")
	}
	if cfg.Database.Driver != "sqlite" {
		return fmt.Errorf("database.driver must be sqlite, got %q", cfg.Database.Driver)
	}
	if cfg.App.BaseURL == "" {
		return errors.New("app.base_url is required")
	}
	return nil
}
