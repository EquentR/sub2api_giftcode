package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"sub2api-giftcode/backend/internal/models"
	"sub2api-giftcode/backend/internal/sub2api"
)

func (s *Service) Login(ctx context.Context, email, password string) (*SessionUser, error) {
	if err := s.requireUpstreamClient(); err != nil {
		return nil, err
	}
	resp, err := s.upstream.Login(ctx, strings.TrimSpace(email), password)
	if err != nil {
		return nil, err
	}
	return s.saveLoginSession(ctx, resp)
}

func (s *Service) saveLoginSession(ctx context.Context, resp *sub2api.AuthLoginResult) (*SessionUser, error) {
	now := s.now()
	expiresAt := now.Add(2 * time.Hour)
	if resp.ExpiresIn > 0 {
		expiresAt = now.Add(time.Duration(resp.ExpiresIn) * time.Second)
	}
	return s.saveSessionForUser(ctx, resp.User, resp.AccessToken, resp.RefreshToken, expiresAt)
}

func (s *Service) saveSessionForUser(ctx context.Context, user sub2api.User, accessToken, refreshToken string, expiresAt time.Time) (*SessionUser, error) {
	now := s.now()
	sessionID, err := newRandomToken(32)
	if err != nil {
		return nil, err
	}
	if err := s.upsertUpstreamUser(ctx, user); err != nil {
		return nil, err
	}
	session := models.Session{
		ID:             sessionID,
		UpstreamUserID: user.ID,
		AccessToken:    accessToken,
		RefreshToken:   refreshToken,
		ExpiresAt:      expiresAt,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.upsertSession(ctx, session); err != nil {
		return nil, err
	}
	return &SessionUser{
		Session: session,
		User:    user,
		IsAdmin: s.isAdminRole(user.Role),
	}, nil
}

func (s *Service) LoginWithAccessToken(ctx context.Context, accessToken string, expectedUserID *int64) (*SessionUser, error) {
	if err := s.requireUpstreamClient(); err != nil {
		return nil, err
	}
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return nil, ErrUnauthorized
	}
	user, err := s.upstream.Me(ctx, accessToken)
	if err != nil {
		return nil, ErrUnauthorized
	}
	if expectedUserID != nil && *expectedUserID > 0 && user.ID != *expectedUserID {
		return nil, ErrUnauthorized
	}
	if !strings.EqualFold(strings.TrimSpace(user.Status), "active") {
		return nil, ErrUnauthorized
	}
	return s.saveSessionForUser(ctx, *user, accessToken, "", s.now().Add(30*24*time.Hour))
}

func (s *Service) CurrentSession(ctx context.Context, sessionID string) (*SessionUser, error) {
	if err := s.requireUpstreamClient(); err != nil {
		return nil, err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, ErrUnauthorized
	}
	session, err := s.getSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUnauthorized
		}
		return nil, err
	}
	if session.RefreshToken == "" && session.ExpiresAt.Before(s.now()) {
		_ = s.deleteSession(ctx, sessionID)
		return nil, ErrUnauthorized
	}
	if session.ExpiresAt.Before(s.now().Add(2 * time.Minute)) {
		refreshed, err := s.refreshSession(ctx, &session)
		if err != nil {
			_ = s.deleteSession(ctx, sessionID)
			return nil, ErrUnauthorized
		}
		session = *refreshed
	}
	user, err := s.upstream.Me(ctx, session.AccessToken)
	if err != nil {
		// Try one refresh before bailing out.
		refreshed, refreshErr := s.refreshSession(ctx, &session)
		if refreshErr != nil {
			_ = s.deleteSession(ctx, sessionID)
			return nil, ErrUnauthorized
		}
		session = *refreshed
		user, err = s.upstream.Me(ctx, session.AccessToken)
		if err != nil {
			_ = s.deleteSession(ctx, sessionID)
			return nil, ErrUnauthorized
		}
	}
	if err := s.upsertUpstreamUser(ctx, *user); err != nil {
		return nil, err
	}
	return &SessionUser{Session: session, User: *user, IsAdmin: s.isAdminRole(user.Role)}, nil
}

func (s *Service) Logout(ctx context.Context, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	return s.deleteSession(ctx, sessionID)
}

func (s *Service) refreshSession(ctx context.Context, session *models.Session) (*models.Session, error) {
	if session == nil {
		return nil, ErrUnauthorized
	}
	if session.RefreshToken == "" {
		return nil, ErrUnauthorized
	}
	resp, err := s.upstream.Refresh(ctx, session.RefreshToken)
	if err != nil {
		return nil, err
	}
	now := s.now()
	expiresAt := now.Add(2 * time.Hour)
	if resp.ExpiresIn > 0 {
		expiresAt = now.Add(time.Duration(resp.ExpiresIn) * time.Second)
	}
	session.AccessToken = resp.AccessToken
	if strings.TrimSpace(resp.RefreshToken) != "" {
		session.RefreshToken = resp.RefreshToken
	}
	session.ExpiresAt = expiresAt
	session.UpdatedAt = now
	if err := s.upsertSession(ctx, *session); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *Service) upsertUpstreamUser(ctx context.Context, user sub2api.User) error {
	if s.db() == nil {
		return fmt.Errorf("database not configured")
	}
	now := s.now()
	profileJSON, _ := json.Marshal(user)
	_, err := s.db().ExecContext(ctx, `
INSERT INTO upstream_users (
  upstream_user_id, email, username, role, status, profile_json, last_seen_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(upstream_user_id) DO UPDATE SET
  email=excluded.email,
  username=excluded.username,
  role=excluded.role,
  status=excluded.status,
  profile_json=excluded.profile_json,
  last_seen_at=excluded.last_seen_at,
  updated_at=excluded.updated_at
`,
		user.ID,
		user.Email,
		user.Username,
		user.Role,
		user.Status,
		string(profileJSON),
		formatTime(now),
		formatTime(now),
		formatTime(now),
	)
	return err
}

func (s *Service) upsertSession(ctx context.Context, session models.Session) error {
	_, err := s.db().ExecContext(ctx, `
INSERT INTO sessions (
  id, upstream_user_id, access_token, refresh_token, expires_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  upstream_user_id=excluded.upstream_user_id,
  access_token=excluded.access_token,
  refresh_token=excluded.refresh_token,
  expires_at=excluded.expires_at,
  updated_at=excluded.updated_at
`,
		session.ID,
		session.UpstreamUserID,
		session.AccessToken,
		session.RefreshToken,
		formatTime(session.ExpiresAt),
		formatTime(session.CreatedAt),
		formatTime(session.UpdatedAt),
	)
	return err
}

func (s *Service) getSession(ctx context.Context, sessionID string) (models.Session, error) {
	var out models.Session
	var expiresAt string
	var createdAt string
	var updatedAt string
	err := s.db().QueryRowContext(ctx, `
SELECT id, upstream_user_id, access_token, refresh_token, expires_at, created_at, updated_at
FROM sessions
WHERE id = ?
`, sessionID).Scan(
		&out.ID,
		&out.UpstreamUserID,
		&out.AccessToken,
		&out.RefreshToken,
		&expiresAt,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return models.Session{}, err
	}
	expiry, err := parseNonNullTime(expiresAt)
	if err != nil {
		return models.Session{}, err
	}
	created, err := parseNonNullTime(createdAt)
	if err != nil {
		return models.Session{}, err
	}
	updated, err := parseNonNullTime(updatedAt)
	if err != nil {
		return models.Session{}, err
	}
	out.ExpiresAt = expiry
	out.CreatedAt = created
	out.UpdatedAt = updated
	return out, nil
}

func (s *Service) deleteSession(ctx context.Context, sessionID string) error {
	_, err := s.db().ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID)
	return err
}
