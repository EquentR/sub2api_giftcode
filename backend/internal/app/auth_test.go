package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/config"
	"sub2api-giftcode/backend/internal/db"
	"sub2api-giftcode/backend/internal/mail"
	"sub2api-giftcode/backend/internal/models"
	"sub2api-giftcode/backend/internal/sub2api"
)

func TestCurrentSessionLoadsActiveUser(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/auth/me", r.URL.Path)
		require.Equal(t, "Bearer access-1", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"id":1,"email":"alice@example.com","username":"alice","role":"user","status":"active","balance":0,"created_at":"2026-06-10T00:00:00Z","updated_at":"2026-06-10T00:00:00Z"}}`))
	}))
	t.Cleanup(upstream.Close)

	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), mail.New(mail.Config{}))
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, svc.upsertSession(context.Background(), models.Session{
		ID:             "session-1",
		UpstreamUserID: 1,
		AccessToken:    "access-1",
		RefreshToken:   "",
		ExpiresAt:      now.Add(time.Hour),
		CreatedAt:      now,
		UpdatedAt:      now,
	}))

	sessionUser, err := svc.CurrentSession(context.Background(), "session-1")
	require.NoError(t, err)
	require.Equal(t, int64(1), sessionUser.User.ID)
	require.Equal(t, "alice", sessionUser.User.Username)
}
