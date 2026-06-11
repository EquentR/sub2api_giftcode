package config

import "time"

type Config struct {
	App struct {
		ListenAddr  string `yaml:"listen_addr"`
		BaseURL     string `yaml:"base_url"`
		FrontendURL string `yaml:"frontend_url"`
		StaticDir   string `yaml:"static_dir"`
	} `yaml:"app"`

	Database struct {
		Driver string `yaml:"driver"`
		Path   string `yaml:"path"`
	} `yaml:"database"`

	Sub2API struct {
		BaseURL     string `yaml:"base_url"`
		AdminAPIKey string `yaml:"admin_api_key"`
	} `yaml:"sub2api"`

	Mail struct {
		SMTPHost        string `yaml:"smtp_host"`
		SMTPPort        int    `yaml:"smtp_port"`
		SMTPUsername    string `yaml:"smtp_username"`
		SMTPPassword    string `yaml:"smtp_password"`
		FromAddress     string `yaml:"from_address"`
		AdminToAddress  string `yaml:"admin_to_address"`
		SubjectPrefix   string `yaml:"subject_prefix"`
		ApprovalTTLHour int    `yaml:"approval_ttl_hours"`
	} `yaml:"mail"`

	Session struct {
		CookieSecret string `yaml:"cookie_secret"`
	} `yaml:"session"`

	Sync struct {
		IntervalSeconds int `yaml:"interval_seconds"`
	} `yaml:"sync"`
}

type RuntimeConfig struct {
	Config
	ApprovalTTL time.Duration
}
