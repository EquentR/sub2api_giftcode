package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadRequiresAdminKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
app:
  listen_addr: "127.0.0.1:8080"
  base_url: "http://127.0.0.1:8080"
database:
  driver: "sqlite"
  path: ":memory:"
sub2api:
  base_url: "http://127.0.0.1:8081"
mail:
  smtp_host: "smtp.example.com"
  smtp_port: 587
  smtp_username: "mailer@example.com"
  smtp_password: "change-me"
  from_address: "mailer@example.com"
  admin_to_address: "admin@example.com"
session:
  cookie_secret: "change-me"
`), 0o644))

	cfg, err := Load(path)
	require.Error(t, err)
	require.Nil(t, cfg)
}

func TestLoadAppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
sub2api:
  base_url: "http://127.0.0.1:8081"
  admin_api_key: "admin-xxxxxxxx"
mail:
  smtp_host: "smtp.example.com"
  smtp_port: 587
  smtp_username: "mailer@example.com"
  smtp_password: "change-me"
  from_address: "mailer@example.com"
  admin_to_address: "admin@example.com"
session:
  cookie_secret: "change-me"
`), 0o644))

	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:8080", cfg.App.ListenAddr)
	require.Equal(t, "http://127.0.0.1:8080", cfg.App.BaseURL)
	require.Empty(t, cfg.App.FrontendURL)
	require.Equal(t, "./public", cfg.App.StaticDir)
	require.Equal(t, "sqlite", cfg.Database.Driver)
	require.Equal(t, 300, cfg.Sync.IntervalSeconds)
	require.Equal(t, 30, cfg.AuxScheduler.IntervalSeconds)
	require.Equal(t, 72, cfg.Mail.ApprovalTTLHour)
}
