package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/config"
	"sub2api-giftcode/backend/internal/db"
	"sub2api-giftcode/backend/internal/models"
)

func TestSiteBrandingDefaultsAndPersists(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.Migrate(context.Background()))

	svc := New(&config.RuntimeConfig{}, store, nil, nil)

	branding, err := svc.GetSiteBranding(context.Background())
	require.NoError(t, err)
	require.Equal(t, "sub2api", branding.Title)
	require.Equal(t, "兑换码系统", branding.Subtitle)
	require.Equal(t, "", branding.MailSubjectPrefix)

	updated, err := svc.ReplaceSiteBranding(context.Background(), models.SiteBranding{
		Title:             "Acme Billing",
		Subtitle:          "充值和兑换",
		MailSubjectPrefix: "[Acme]",
	})
	require.NoError(t, err)
	require.Equal(t, "Acme Billing", updated.Title)
	require.Equal(t, "充值和兑换", updated.Subtitle)
	require.Equal(t, "[Acme]", updated.MailSubjectPrefix)

	reloaded, err := svc.GetSiteBranding(context.Background())
	require.NoError(t, err)
	require.Equal(t, "Acme Billing", reloaded.Title)
	require.Equal(t, "充值和兑换", reloaded.Subtitle)
	require.Equal(t, "[Acme]", reloaded.MailSubjectPrefix)
}
