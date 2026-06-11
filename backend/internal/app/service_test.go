package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/config"
)

func TestApprovalConfirmURLUsesFrontendBase(t *testing.T) {
	svc := &Service{
		cfg: &config.RuntimeConfig{},
	}
	svc.cfg.App.BaseURL = "https://api.example.com/"
	svc.cfg.App.FrontendURL = "https://app.example.com/sub2api/"

	got := svc.approvalConfirmURL("abc/123")

	require.Equal(t, "https://app.example.com/sub2api/approval/confirm?token=abc%2F123", got)
}

func TestApprovalConfirmURLFallsBackToBackendBaseWhenFrontendBaseMissing(t *testing.T) {
	svc := &Service{
		cfg: &config.RuntimeConfig{},
	}
	svc.cfg.App.BaseURL = "https://api.example.com/"

	got := svc.approvalConfirmURL("abc/123")

	require.Equal(t, "https://api.example.com/api/admin/redeem-access-requests/confirm?token=abc%2F123", got)
}

func TestApprovalTTLDefaultsWhenRuntimeConfigHasNoLoadedDefaults(t *testing.T) {
	svc := &Service{
		cfg: &config.RuntimeConfig{},
	}

	require.Equal(t, 72*time.Hour, svc.approvalTTL())
}
