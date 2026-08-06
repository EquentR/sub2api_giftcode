package sub2api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	BaseURL     string
	AdminAPIKey string
	HTTPClient  *http.Client
}

type User struct {
	ID          int64      `json:"id"`
	Email       string     `json:"email"`
	Username    string     `json:"username"`
	Role        string     `json:"role"`
	Status      string     `json:"status"`
	Balance     float64    `json:"balance"`
	Concurrency int        `json:"concurrency"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
}

type AuthLoginResult struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	User         User   `json:"user"`
}

type RedeemCode struct {
	ID           int64      `json:"id"`
	Code         string     `json:"code"`
	Type         string     `json:"type"`
	Value        float64    `json:"value"`
	Status       string     `json:"status"`
	UsedBy       *int64     `json:"used_by"`
	UsedAt       *time.Time `json:"used_at"`
	CreatedAt    time.Time  `json:"created_at"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	GroupID      *int64     `json:"group_id"`
	ValidityDays int        `json:"validity_days"`
	Notes        *string    `json:"notes,omitempty"`
	User         *User      `json:"user,omitempty"`
}

type Group struct {
	ID               int64     `json:"id"`
	Name             string    `json:"name"`
	Description      string    `json:"description"`
	Platform         string    `json:"platform"`
	RateMultiplier   float64   `json:"rate_multiplier"`
	Status           string    `json:"status"`
	SubscriptionType string    `json:"subscription_type"`
	DailyLimitUSD    *float64  `json:"daily_limit_usd"`
	WeeklyLimitUSD   *float64  `json:"weekly_limit_usd"`
	MonthlyLimitUSD  *float64  `json:"monthly_limit_usd"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Subscription struct {
	ID                 int64      `json:"id"`
	UserID             int64      `json:"user_id"`
	GroupID            int64      `json:"group_id"`
	StartsAt           time.Time  `json:"starts_at"`
	ExpiresAt          time.Time  `json:"expires_at"`
	Status             string     `json:"status"`
	DailyWindowStart   *time.Time `json:"daily_window_start"`
	WeeklyWindowStart  *time.Time `json:"weekly_window_start"`
	MonthlyWindowStart *time.Time `json:"monthly_window_start"`
	DailyUsageUSD      float64    `json:"daily_usage_usd"`
	WeeklyUsageUSD     float64    `json:"weekly_usage_usd"`
	MonthlyUsageUSD    float64    `json:"monthly_usage_usd"`
	AssignedBy         *int64     `json:"assigned_by,omitempty"`
	AssignedAt         *time.Time `json:"assigned_at,omitempty"`
	Notes              string     `json:"notes,omitempty"`
	RevokedAt          *time.Time `json:"revoked_at,omitempty"`
	User               *User      `json:"user,omitempty"`
	Group              *Group     `json:"group,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type SubscriptionProgress struct {
	ID            int64                `json:"id"`
	GroupName     string               `json:"group_name"`
	ExpiresAt     time.Time            `json:"expires_at"`
	ExpiresInDays int                  `json:"expires_in_days"`
	Daily         *UsageWindowProgress `json:"daily,omitempty"`
	Weekly        *UsageWindowProgress `json:"weekly,omitempty"`
	Monthly       *UsageWindowProgress `json:"monthly,omitempty"`
}

type UsageWindowProgress struct {
	LimitUSD        float64    `json:"limit_usd"`
	UsedUSD         float64    `json:"used_usd"`
	RemainingUSD    float64    `json:"remaining_usd"`
	Percentage      float64    `json:"percentage"`
	WindowStart     *time.Time `json:"window_start"`
	ResetsAt        *time.Time `json:"resets_at"`
	ResetsInSeconds int64      `json:"resets_in_seconds"`
}

type ResetQuotaInput struct {
	Daily   bool `json:"daily"`
	Weekly  bool `json:"weekly"`
	Monthly bool `json:"monthly"`
}

type Account struct {
	ID                      int64          `json:"id"`
	Name                    string         `json:"name"`
	Platform                string         `json:"platform"`
	Type                    string         `json:"type"`
	Status                  string         `json:"status"`
	Credentials             map[string]any `json:"credentials"`
	Extra                   map[string]any `json:"extra"`
	Schedulable             bool           `json:"schedulable"`
	RateLimitedAt           *time.Time     `json:"rate_limited_at"`
	RateLimitResetAt        *time.Time     `json:"rate_limit_reset_at"`
	OverloadUntil           *time.Time     `json:"overload_until"`
	TempUnschedulableUntil  *time.Time     `json:"temp_unschedulable_until"`
	TempUnschedulableReason string         `json:"temp_unschedulable_reason"`
	LastUsedAt              *time.Time     `json:"last_used_at"`
	CreatedAt               time.Time      `json:"created_at"`
	UpdatedAt               time.Time      `json:"updated_at"`
}

func (a Account) UserAgent() string {
	if a.Credentials == nil {
		return ""
	}
	value, ok := a.Credentials["user_agent"]
	if !ok {
		return ""
	}
	ua, ok := value.(string)
	if !ok {
		return ""
	}
	return ua
}

type GenerateRedeemCodesInput struct {
	Type         string
	Value        float64
	GroupID      *int64
	ValidityDays int
}

type CreateAndRedeemCodeInput struct {
	Code         string
	Type         string
	Value        float64
	UserID       int64
	GroupID      *int64
	ValidityDays int
	Notes        string
}

type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Reason  string          `json:"reason,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type paginatedEnvelope[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	Pages    int   `json:"pages"`
}

type APIError struct {
	Status  int
	Message string
	Reason  string
}

var (
	ErrAuthoritativeReadFailed = errors.New("authoritative upstream read failed")
	ErrUpstreamRejected        = errors.New("upstream rejected request")
	ErrResultUnknown           = errors.New("upstream result unknown")
)

type OperationErrorKind string

const (
	OperationErrorAuthoritativeRead OperationErrorKind = "authoritative_read_failed"
	OperationErrorRejected          OperationErrorKind = "upstream_rejected"
	OperationErrorResultUnknown     OperationErrorKind = "result_unknown"
)

type OperationError struct {
	Kind      OperationErrorKind
	Operation string
	Status    int
	Message   string
	Reason    string
}

func (e *OperationError) Error() string {
	if e == nil {
		return ""
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = string(e.Kind)
	}
	if e.Reason != "" {
		message += " (" + e.Reason + ")"
	}
	if e.Operation == "" {
		return message
	}
	return e.Operation + ": " + message
}

func (e *OperationError) Is(target error) bool {
	if e == nil {
		return false
	}
	switch target {
	case ErrAuthoritativeReadFailed:
		return e.Kind == OperationErrorAuthoritativeRead
	case ErrUpstreamRejected:
		return e.Kind == OperationErrorRejected
	case ErrResultUnknown:
		return e.Kind == OperationErrorResultUnknown
	default:
		return false
	}
}

var sensitiveCredentialKeys = map[string]struct{}{
	"access_token":          {},
	"refresh_token":         {},
	"id_token":              {},
	"api_key":               {},
	"session_key":           {},
	"cookie":                {},
	"aws_secret_access_key": {},
	"aws_session_token":     {},
	"service_account_json":  {},
	"service_account":       {},
	"private_key":           {},
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	if e.Reason != "" {
		return fmt.Sprintf("%s (%s)", e.Message, e.Reason)
	}
	return e.Message
}

func NewClient(baseURL, adminAPIKey string) *Client {
	return &Client{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		AdminAPIKey: strings.TrimSpace(adminAPIKey),
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) Login(ctx context.Context, email, password string) (*AuthLoginResult, error) {
	var out AuthLoginResult
	err := c.postJSON(ctx, "/api/v1/auth/login", nil, map[string]string{
		"email":    email,
		"password": password,
	}, &out)
	return &out, err
}

func (c *Client) Refresh(ctx context.Context, refreshToken string) (*AuthLoginResult, error) {
	var out AuthLoginResult
	err := c.postJSON(ctx, "/api/v1/auth/refresh", nil, map[string]string{
		"refresh_token": refreshToken,
	}, &out)
	return &out, err
}

func (c *Client) Me(ctx context.Context, accessToken string) (*User, error) {
	var out User
	if err := c.getJSON(ctx, "/api/v1/auth/me", accessToken, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GenerateRedeemCodes(ctx context.Context, idempotencyKey string, input GenerateRedeemCodesInput) ([]RedeemCode, error) {
	var out []RedeemCode
	reqBody := map[string]any{
		"count": 1,
		"type":  input.Type,
		"value": input.Value,
	}
	if input.GroupID != nil {
		reqBody["group_id"] = *input.GroupID
	}
	if input.ValidityDays != 0 {
		reqBody["validity_days"] = input.ValidityDays
	}
	if err := c.postJSON(ctx, "/api/v1/admin/redeem-codes/generate", map[string]string{
		"x-api-key":       c.AdminAPIKey,
		"Idempotency-Key": idempotencyKey,
	}, reqBody, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CreateAndRedeemCode(ctx context.Context, idempotencyKey string, input CreateAndRedeemCodeInput) (*RedeemCode, error) {
	var out struct {
		RedeemCode RedeemCode `json:"redeem_code"`
	}
	reqBody := map[string]any{
		"code":    input.Code,
		"type":    input.Type,
		"value":   input.Value,
		"user_id": input.UserID,
	}
	if input.GroupID != nil {
		reqBody["group_id"] = *input.GroupID
	}
	if input.ValidityDays != 0 {
		reqBody["validity_days"] = input.ValidityDays
	}
	if strings.TrimSpace(input.Notes) != "" {
		reqBody["notes"] = strings.TrimSpace(input.Notes)
	}
	if err := c.postJSON(ctx, "/api/v1/admin/redeem-codes/create-and-redeem", map[string]string{
		"x-api-key":       c.AdminAPIKey,
		"Idempotency-Key": idempotencyKey,
	}, reqBody, &out); err != nil {
		return nil, err
	}
	return &out.RedeemCode, nil
}

func (c *Client) ListGroupsAll(ctx context.Context) ([]Group, error) {
	var out []Group
	if err := c.getJSONWithHeaders(ctx, "/api/v1/admin/groups/all", map[string]string{"x-api-key": c.AdminAPIKey}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) ListUsersAll(ctx context.Context) ([]User, error) {
	const pageSize = 100
	page := 1
	out := make([]User, 0)
	for {
		var pageData paginatedEnvelope[User]
		path := fmt.Sprintf("/api/v1/admin/users?page=%d&page_size=%d&include_subscriptions=false", page, pageSize)
		if err := c.getJSONWithHeaders(ctx, path, map[string]string{"x-api-key": c.AdminAPIKey}, &pageData); err != nil {
			return nil, err
		}
		out = append(out, pageData.Items...)
		if len(pageData.Items) == 0 || int64(page*pageSize) >= pageData.Total || page >= pageData.Pages {
			break
		}
		page++
	}
	return out, nil
}

func (c *Client) GetDefaultConcurrency(ctx context.Context) (int, error) {
	var out struct {
		DefaultConcurrency int `json:"default_concurrency"`
	}
	if err := c.getJSONWithHeaders(ctx, "/api/v1/admin/settings", map[string]string{"x-api-key": c.AdminAPIKey}, &out); err != nil {
		return 0, err
	}
	if out.DefaultConcurrency <= 0 {
		return 0, fmt.Errorf("invalid default concurrency: %d", out.DefaultConcurrency)
	}
	return out.DefaultConcurrency, nil
}

func (c *Client) GetUser(ctx context.Context, userID int64) (*User, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("user ID must be positive")
	}
	var out User
	if err := c.getJSONWithHeaders(ctx, fmt.Sprintf("/api/v1/admin/users/%d", userID), map[string]string{"x-api-key": c.AdminAPIKey}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateUserConcurrency(ctx context.Context, userID int64, concurrency int) (*User, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("user ID must be positive")
	}
	if concurrency <= 0 {
		return nil, fmt.Errorf("concurrency must be positive")
	}
	var out User
	if err := c.putJSON(ctx, fmt.Sprintf("/api/v1/admin/users/%d", userID), map[string]string{"x-api-key": c.AdminAPIKey}, map[string]int{
		"concurrency": concurrency,
	}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListActiveUserSubscriptions(ctx context.Context, userID int64) ([]Subscription, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("user ID must be positive")
	}
	const pageSize = 100
	page := 1
	out := make([]Subscription, 0)
	for {
		var pageData paginatedEnvelope[Subscription]
		path := fmt.Sprintf("/api/v1/admin/subscriptions?user_id=%d&status=active&page=%d&page_size=%d", userID, page, pageSize)
		if err := c.getJSONWithHeaders(ctx, path, map[string]string{"x-api-key": c.AdminAPIKey}, &pageData); err != nil {
			return nil, c.operationError(OperationErrorAuthoritativeRead, "list subscriptions", err)
		}
		if pageData.Items == nil || pageData.Page != page || pageData.PageSize <= 0 || pageData.Total < 0 || pageData.Pages < 0 {
			return nil, c.operationError(OperationErrorAuthoritativeRead, "list subscriptions", fmt.Errorf("unverifiable subscriptions page"))
		}
		out = append(out, pageData.Items...)
		if len(pageData.Items) == 0 || int64(page*pageSize) >= pageData.Total || page >= pageData.Pages {
			break
		}
		page++
	}
	return out, nil
}

func (c *Client) ListAllActiveSubscriptions(ctx context.Context) ([]Subscription, error) {
	const pageSize = 100
	page := 1
	out := make([]Subscription, 0)
	seenIDs := make(map[int64]struct{})
	var expectedTotal int64 = -1
	var expectedPages int
	for {
		var pageData paginatedEnvelope[Subscription]
		path := fmt.Sprintf("/api/v1/admin/subscriptions?status=active&page=%d&page_size=%d", page, pageSize)
		if err := c.getJSONWithHeaders(ctx, path, map[string]string{"x-api-key": c.AdminAPIKey}, &pageData); err != nil {
			return nil, c.operationError(OperationErrorAuthoritativeRead, "list all active subscriptions", err)
		}
		if pageData.Items == nil || pageData.Page != page || pageData.PageSize <= 0 || pageData.Total < 0 || pageData.Pages < 0 {
			return nil, c.operationError(OperationErrorAuthoritativeRead, "list all active subscriptions", fmt.Errorf("unverifiable subscriptions page"))
		}
		if pageData.Total == 0 {
			if page != 1 || len(pageData.Items) != 0 || pageData.Pages > 1 {
				return nil, c.operationError(OperationErrorAuthoritativeRead, "list all active subscriptions", fmt.Errorf("inconsistent empty subscriptions page"))
			}
			return out, nil
		}
		calculatedPages := int((pageData.Total + int64(pageData.PageSize) - 1) / int64(pageData.PageSize))
		if pageData.Pages != calculatedPages || pageData.Page > pageData.Pages || len(pageData.Items) == 0 || len(pageData.Items) > pageData.PageSize {
			return nil, c.operationError(OperationErrorAuthoritativeRead, "list all active subscriptions", fmt.Errorf("inconsistent subscriptions pagination"))
		}
		if expectedTotal < 0 {
			expectedTotal = pageData.Total
			expectedPages = pageData.Pages
		} else if pageData.Total != expectedTotal || pageData.Pages != expectedPages {
			return nil, c.operationError(OperationErrorAuthoritativeRead, "list all active subscriptions", fmt.Errorf("subscriptions pagination changed during read"))
		}
		for _, subscription := range pageData.Items {
			if subscription.ID <= 0 {
				return nil, c.operationError(OperationErrorAuthoritativeRead, "list all active subscriptions", fmt.Errorf("invalid subscription ID in page"))
			}
			if _, exists := seenIDs[subscription.ID]; exists {
				return nil, c.operationError(OperationErrorAuthoritativeRead, "list all active subscriptions", fmt.Errorf("duplicate subscription in pages"))
			}
			seenIDs[subscription.ID] = struct{}{}
			out = append(out, subscription)
		}
		if int64(len(out)) > expectedTotal {
			return nil, c.operationError(OperationErrorAuthoritativeRead, "list all active subscriptions", fmt.Errorf("subscriptions page exceeded total"))
		}
		if page == expectedPages {
			if int64(len(out)) != expectedTotal {
				return nil, c.operationError(OperationErrorAuthoritativeRead, "list all active subscriptions", fmt.Errorf("incomplete subscriptions result"))
			}
			return out, nil
		}
		page++
	}
}

func (c *Client) GetSubscription(ctx context.Context, subscriptionID int64) (*Subscription, error) {
	if subscriptionID <= 0 {
		return nil, fmt.Errorf("subscription ID must be positive")
	}
	var out Subscription
	path := fmt.Sprintf("/api/v1/admin/subscriptions/%d", subscriptionID)
	if err := c.getJSONWithHeaders(ctx, path, map[string]string{"x-api-key": c.AdminAPIKey}, &out); err != nil {
		return nil, c.operationError(OperationErrorAuthoritativeRead, "get subscription", err)
	}
	if out.ID != subscriptionID {
		return nil, c.operationError(OperationErrorAuthoritativeRead, "get subscription", fmt.Errorf("unverifiable subscription response"))
	}
	return &out, nil
}

func (c *Client) GetSubscriptionProgress(ctx context.Context, subscriptionID int64) (*SubscriptionProgress, error) {
	if subscriptionID <= 0 {
		return nil, fmt.Errorf("subscription ID must be positive")
	}
	var out SubscriptionProgress
	path := fmt.Sprintf("/api/v1/admin/subscriptions/%d/progress", subscriptionID)
	if err := c.getJSONWithHeaders(ctx, path, map[string]string{"x-api-key": c.AdminAPIKey}, &out); err != nil {
		return nil, c.operationError(OperationErrorAuthoritativeRead, "get subscription progress", err)
	}
	if out.ID != subscriptionID {
		return nil, c.operationError(OperationErrorAuthoritativeRead, "get subscription progress", fmt.Errorf("unverifiable subscription progress response"))
	}
	return &out, nil
}

func (c *Client) ResetSubscriptionQuota(ctx context.Context, subscriptionID int64, group Group) (*Subscription, error) {
	if subscriptionID <= 0 {
		return nil, fmt.Errorf("subscription ID must be positive")
	}
	input := ResetQuotaInput{
		Daily:   positiveLimit(group.DailyLimitUSD),
		Weekly:  positiveLimit(group.WeeklyLimitUSD),
		Monthly: positiveLimit(group.MonthlyLimitUSD),
	}
	if !input.Daily && !input.Weekly && !input.Monthly {
		return nil, fmt.Errorf("subscription group has no configured quota window")
	}
	var out Subscription
	path := fmt.Sprintf("/api/v1/admin/subscriptions/%d/reset-quota", subscriptionID)
	err := c.postJSON(ctx, path, map[string]string{"x-api-key": c.AdminAPIKey}, input, &out)
	if err == nil {
		if out.ID != subscriptionID {
			return nil, c.operationError(OperationErrorResultUnknown, "reset subscription quota", fmt.Errorf("unverifiable reset response"))
		}
		return &out, nil
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return nil, c.operationError(OperationErrorRejected, "reset subscription quota", err)
	}
	return nil, c.operationError(OperationErrorResultUnknown, "reset subscription quota", err)
}

func positiveLimit(limit *float64) bool {
	return limit != nil && *limit > 0
}

func (c *Client) AddUserBalance(ctx context.Context, idempotencyKey string, userID int64, amount float64, notes string) (*User, error) {
	var out User
	body := map[string]any{
		"balance":   amount,
		"operation": "add",
	}
	if strings.TrimSpace(notes) != "" {
		body["notes"] = strings.TrimSpace(notes)
	}
	if err := c.postJSON(ctx, fmt.Sprintf("/api/v1/admin/users/%d/balance", userID), map[string]string{
		"x-api-key":       c.AdminAPIKey,
		"Idempotency-Key": idempotencyKey,
	}, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ExtendSubscription(ctx context.Context, idempotencyKey string, subscriptionID int64, days int) (*Subscription, error) {
	if subscriptionID <= 0 || days <= 0 {
		return nil, fmt.Errorf("subscription ID and days must be positive")
	}
	var out Subscription
	err := c.postJSON(ctx, fmt.Sprintf("/api/v1/admin/subscriptions/%d/extend", subscriptionID), map[string]string{
		"x-api-key":       c.AdminAPIKey,
		"Idempotency-Key": idempotencyKey,
	}, map[string]int{"days": days}, &out)
	if err == nil {
		if out.ID != subscriptionID || out.ExpiresAt.IsZero() {
			return nil, c.operationError(OperationErrorResultUnknown, "extend subscription", fmt.Errorf("unverifiable extension response"))
		}
		return &out, nil
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return nil, c.operationError(OperationErrorRejected, "extend subscription", err)
	}
	return nil, c.operationError(OperationErrorResultUnknown, "extend subscription", err)
}

func (c *Client) ListRedeemCodes(ctx context.Context, search string, pageSize int) ([]RedeemCode, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	var out paginatedEnvelope[RedeemCode]
	path := fmt.Sprintf("/api/v1/admin/redeem-codes?search=%s&page=1&page_size=%d", url.QueryEscape(search), pageSize)
	if err := c.getJSONWithHeaders(ctx, path, map[string]string{"x-api-key": c.AdminAPIKey}, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *Client) ListOpenAIAccounts(ctx context.Context) ([]Account, error) {
	var out paginatedEnvelope[Account]
	path := "/api/v1/admin/accounts?platform=openai&page=1&page_size=200"
	if err := c.getJSONWithHeaders(ctx, path, map[string]string{"x-api-key": c.AdminAPIKey}, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *Client) GetAccount(ctx context.Context, accountID int64) (*Account, error) {
	var out Account
	if err := c.getJSONWithHeaders(ctx, fmt.Sprintf("/api/v1/admin/accounts/%d", accountID), map[string]string{"x-api-key": c.AdminAPIKey}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateOpenAIAccountUserAgent(ctx context.Context, accountID int64, userAgent string) (*Account, error) {
	existing, err := c.GetAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	credentials := cloneCredentials(existing.Credentials)
	credentials["user_agent"] = strings.TrimSpace(userAgent)
	var out Account
	body := map[string]any{
		"credentials": credentials,
	}
	if err := c.putJSON(ctx, fmt.Sprintf("/api/v1/admin/accounts/%d", accountID), map[string]string{"x-api-key": c.AdminAPIKey}, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateOpenAIAccountStatus(ctx context.Context, accountID int64, status string) (*Account, error) {
	if accountID <= 0 {
		return nil, fmt.Errorf("account ID must be positive")
	}
	var out Account
	if err := c.putJSON(ctx, fmt.Sprintf("/api/v1/admin/accounts/%d", accountID), map[string]string{
		"x-api-key": c.AdminAPIKey,
	}, map[string]string{"status": status}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) SetOpenAIAccountSchedulable(ctx context.Context, accountID int64, schedulable bool) (*Account, error) {
	if accountID <= 0 {
		return nil, fmt.Errorf("account ID must be positive")
	}
	var out Account
	if err := c.postJSON(ctx, fmt.Sprintf("/api/v1/admin/accounts/%d/schedulable", accountID), map[string]string{
		"x-api-key": c.AdminAPIKey,
	}, map[string]bool{"schedulable": schedulable}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type usageLogListItem struct {
	CreatedAt time.Time `json:"created_at"`
}

func (c *Client) HasOpenAIUsageLogAfter(ctx context.Context, accountID int64, since time.Time) (bool, error) {
	if accountID <= 0 {
		return false, fmt.Errorf("account ID must be positive")
	}
	startDate := since.UTC().Format("2006-01-02")
	endDate := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	var out paginatedEnvelope[usageLogListItem]
	path := fmt.Sprintf("/api/v1/admin/usage?account_id=%d&start_date=%s&end_date=%s&timezone=UTC&page=1&page_size=1&sort_by=created_at&sort_order=desc&exact_total=false", accountID, startDate, endDate)
	if err := c.getJSONWithHeaders(ctx, path, map[string]string{"x-api-key": c.AdminAPIKey}, &out); err != nil {
		return false, err
	}
	if len(out.Items) == 0 {
		return false, nil
	}
	return out.Items[0].CreatedAt.After(since), nil
}

func cloneCredentials(credentials map[string]any) map[string]any {
	out := make(map[string]any, len(credentials)+1)
	for key, value := range credentials {
		if _, sensitive := sensitiveCredentialKeys[key]; sensitive {
			continue
		}
		out[key] = value
	}
	return out
}

func (c *Client) getJSON(ctx context.Context, path string, accessToken string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	return c.do(req, out)
}

func (c *Client) getJSONWithHeaders(ctx context.Context, path string, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	for k, v := range headers {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			continue
		}
		req.Header.Set(k, v)
	}
	return c.do(req, out)
}

func (c *Client) putJSON(ctx context.Context, path string, headers map[string]string, body any, out any) error {
	return c.jsonWithBody(ctx, http.MethodPut, path, headers, body, out)
}

func (c *Client) postJSON(ctx context.Context, path string, headers map[string]string, body any, out any) error {
	return c.jsonWithBody(ctx, http.MethodPost, path, headers, body, out)
}

func (c *Client) jsonWithBody(ctx context.Context, method string, path string, headers map[string]string, body any, out any) error {
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			continue
		}
		req.Header.Set(k, v)
	}
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		if resp.StatusCode >= http.StatusMultipleChoices {
			return &APIError{Status: resp.StatusCode, Message: resp.Status}
		}
		return fmt.Errorf("decode sub2api response: %w", err)
	}
	if resp.StatusCode >= 300 || env.Code != 0 {
		msg := env.Message
		if msg == "" {
			msg = resp.Status
		}
		return &APIError{Status: resp.StatusCode, Message: msg, Reason: env.Reason}
	}
	if out == nil {
		return nil
	}
	if len(env.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("decode sub2api data: %w", err)
	}
	return nil
}

func (c *Client) operationError(kind OperationErrorKind, operation string, err error) error {
	if err == nil {
		return nil
	}
	status := 0
	message := err.Error()
	reason := ""
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		status = apiErr.Status
		message = apiErr.Message
		reason = apiErr.Reason
	}
	return &OperationError{
		Kind:      kind,
		Operation: operation,
		Status:    status,
		Message:   redactSecret(message, c.AdminAPIKey),
		Reason:    redactSecret(reason, c.AdminAPIKey),
	}
}

func redactSecret(value string, secrets ...string) string {
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if secret == "" {
			continue
		}
		value = strings.ReplaceAll(value, secret, "[REDACTED]")
	}
	return value
}
