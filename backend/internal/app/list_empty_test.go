package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/db"
)

func TestListMethodsReturnEmptySlicesWhenNoRows(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	svc := &Service{store: store}

	accessRequests, err := svc.listAccessRequests(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, accessRequests)
	require.Empty(t, accessRequests)

	redeemRequests, err := svc.listRedeemRequests(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, redeemRequests)
	require.Empty(t, redeemRequests)

	redeemCodes, err := svc.listRedeemCodes(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, redeemCodes)
	require.Empty(t, redeemCodes)

	users, err := svc.ListUsers(context.Background())
	require.NoError(t, err)
	require.NotNil(t, users)
	require.Empty(t, users)
}
