package app

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	"sub2api-giftcode/backend/internal/config"
	"sub2api-giftcode/backend/internal/db"
	"sub2api-giftcode/backend/internal/mail"
	"sub2api-giftcode/backend/internal/models"
	"sub2api-giftcode/backend/internal/sub2api"
)

type Service struct {
	cfg                  *config.RuntimeConfig
	store                *db.Store
	upstream             *sub2api.Client
	mailer               *mail.Mailer
	concurrencyMu        sync.Mutex
	resetMu              sync.Mutex
	resetWake            chan struct{}
	bonusWake            chan struct{}
	nowFunc              func() time.Time
	resetBoundaryLocksMu sync.Mutex
	resetBoundaryLocks   map[subscriptionResetBoundaryKey]*subscriptionResetBoundaryLock
}

type SessionUser struct {
	Session models.Session
	User    sub2api.User
	IsAdmin bool
}

type UserSummary struct {
	models.UpstreamUser
	AccessRequestCount int        `json:"access_request_count"`
	RedeemRequestCount int        `json:"redeem_request_count"`
	RedeemCodeCount    int        `json:"redeem_code_count"`
	UsedCodeCount      int        `json:"used_code_count"`
	UnusedCodeCount    int        `json:"unused_code_count"`
	LatestRequestAt    *time.Time `json:"latest_request_at,omitempty"`
	LatestCodeAt       *time.Time `json:"latest_code_at,omitempty"`
}

type DashboardStats struct {
	TotalUsers                 int        `json:"total_users"`
	PendingAccessRequests      int        `json:"pending_access_requests"`
	ApprovedAccessRequests     int        `json:"approved_access_requests"`
	RejectedAccessRequests     int        `json:"rejected_access_requests"`
	ConsumedAccessRequests     int        `json:"consumed_access_requests"`
	DirectChargeAccessRequests int        `json:"direct_charge_access_requests"`
	RedeemRequests             int        `json:"redeem_requests"`
	RedeemCodesTotal           int        `json:"redeem_codes_total"`
	RedeemCodesUnused          int        `json:"redeem_codes_unused"`
	RedeemCodesUsed            int        `json:"redeem_codes_used"`
	ActiveTiers                int        `json:"active_tiers"`
	LastSyncAt                 *time.Time `json:"last_sync_at,omitempty"`
}

func New(cfg *config.RuntimeConfig, store *db.Store, upstream *sub2api.Client, mailer *mail.Mailer) *Service {
	return &Service{
		cfg:                cfg,
		store:              store,
		upstream:           upstream,
		mailer:             mailer,
		resetWake:          make(chan struct{}, 1),
		bonusWake:          make(chan struct{}, 1),
		resetBoundaryLocks: make(map[subscriptionResetBoundaryKey]*subscriptionResetBoundaryLock),
	}
}

func (s *Service) now() time.Time {
	if s != nil && s.nowFunc != nil {
		return s.nowFunc().UTC().Truncate(time.Second)
	}
	if s != nil && s.store != nil {
		return s.store.NowUTC()
	}
	return time.Now().UTC().Truncate(time.Second)
}

func (s *Service) db() *sql.DB {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.DB
}

func (s *Service) approvalTTL() time.Duration {
	if s == nil || s.cfg == nil {
		return 72 * time.Hour
	}
	if s.cfg.ApprovalTTL > 0 {
		return s.cfg.ApprovalTTL
	}
	if s.cfg.Mail.ApprovalTTLHour > 0 {
		return time.Duration(s.cfg.Mail.ApprovalTTLHour) * time.Hour
	}
	return 72 * time.Hour
}

func (s *Service) approvalConfirmURL(token string) string {
	escapedToken := url.QueryEscape(token)
	if strings.TrimSpace(s.cfg.App.FrontendURL) != "" {
		return s.frontendURL("/approval/confirm?token=" + escapedToken)
	}
	return s.publicURL("/approval/confirm?token=" + escapedToken)
}

func (s *Service) publicURL(path string) string {
	base := strings.TrimRight(s.cfg.App.BaseURL, "/")
	return base + path
}

func (s *Service) frontendURL(path string) string {
	base := strings.TrimRight(s.cfg.App.FrontendURL, "/")
	return base + path
}

func newRandomToken(bytesLen int) (string, error) {
	if bytesLen <= 0 {
		bytesLen = 32
	}
	raw := make([]byte, bytesLen)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, err
	}
	utc := t.UTC()
	return &utc, nil
}

func parseNonNullTime(raw string) (time.Time, error) {
	t, err := parseTime(raw)
	if err != nil {
		return time.Time{}, err
	}
	if t == nil {
		return time.Time{}, errors.New("time is null")
	}
	return *t, nil
}

func marshalJSON(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func scanMaybeTime(raw sql.NullString) (*time.Time, error) {
	if !raw.Valid {
		return nil, nil
	}
	return parseTime(raw.String)
}

func formatNullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

func parseNullableInt64(raw sql.NullInt64) *int64 {
	if !raw.Valid {
		return nil
	}
	v := raw.Int64
	return &v
}

func parseNullableFloat64(raw sql.NullFloat64) *float64 {
	if !raw.Valid {
		return nil
	}
	v := raw.Float64
	return &v
}

func (s *Service) requireUpstreamClient() error {
	if s == nil || s.upstream == nil {
		return errors.New("sub2api client not configured")
	}
	return nil
}

func (s *Service) requireMailer() error {
	if s == nil || s.mailer == nil {
		return errors.New("mailer not configured")
	}
	return nil
}

func (s *Service) isAdminRole(role string) bool {
	return strings.EqualFold(strings.TrimSpace(role), "admin")
}
