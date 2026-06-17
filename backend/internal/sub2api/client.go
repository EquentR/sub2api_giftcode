package sub2api

import (
	"bytes"
	"context"
	"encoding/json"
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
	ID        int64      `json:"id"`
	Email     string     `json:"email"`
	Username  string     `json:"username"`
	Role      string     `json:"role"`
	Status    string     `json:"status"`
	Balance   float64    `json:"balance"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
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

func (c *Client) postJSON(ctx context.Context, path string, headers map[string]string, body any, out any) error {
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(payload))
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
