package app

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/config"
	"sub2api-giftcode/backend/internal/db"
)

func TestListUsersReturnsSummaries(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.Migrate(context.Background()))

	now := time.Now().UTC().Truncate(time.Second)
	_, err = store.DB.ExecContext(context.Background(), `
INSERT INTO upstream_users (
  upstream_user_id, email, username, role, status, profile_json, last_seen_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`, 1, "alice@example.com", "alice", "user", "active", `{"id":1}`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	require.NoError(t, err)

	svc := New(&config.RuntimeConfig{}, store, nil, nil)
	users, err := svc.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.Equal(t, int64(1), users[0].UpstreamUserID)
	require.Equal(t, "alice@example.com", users[0].Email)
}

func TestReplaceBalanceTiersPersistsPaidAmount(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.Migrate(context.Background()))

	svc := New(&config.RuntimeConfig{}, store, nil, nil)
	tiers, err := svc.ListBalanceTiers(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, tiers)

	tiers[0].PayAmountCny = 95.25
	updated, err := svc.ReplaceBalanceTiers(context.Background(), tiers)
	require.NoError(t, err)
	require.InDelta(t, 95.25, updated[0].PayAmountCny, 0.0001)

	reloaded, err := svc.ListBalanceTiers(context.Background())
	require.NoError(t, err)
	require.InDelta(t, 95.25, reloaded[0].PayAmountCny, 0.0001)
}
