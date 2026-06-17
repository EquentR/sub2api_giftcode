package models

import "time"

type UpstreamUser struct {
	UpstreamUserID int64     `json:"upstream_user_id"`
	Email          string    `json:"email"`
	Username       string    `json:"username"`
	Role           string    `json:"role"`
	Status         string    `json:"status"`
	ProfileJSON    string    `json:"profile_json"`
	LastSeenAt     time.Time `json:"last_seen_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Session struct {
	ID             string    `json:"id"`
	UpstreamUserID int64     `json:"upstream_user_id"`
	AccessToken    string    `json:"access_token"`
	RefreshToken   string    `json:"refresh_token"`
	ExpiresAt      time.Time `json:"expires_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type AccessRequest struct {
	ID                      int64      `json:"id"`
	RequestorUpstreamUserID int64      `json:"requestor_upstream_user_id"`
	RequestorEmail          string     `json:"requestor_email"`
	RequestorUsername       string     `json:"requestor_username"`
	TierID                  int64      `json:"tier_id"`
	CodeType                string     `json:"code_type"`
	TierLabel               string     `json:"tier_label"`
	Amount                  float64    `json:"amount"`
	PayAmountCny            float64    `json:"pay_amount_cny"`
	Sub2APIGroupID          *int64     `json:"sub2api_group_id,omitempty"`
	Sub2APIGroupName        string     `json:"sub2api_group_name"`
	Sub2APIGroupPlatform    string     `json:"sub2api_group_platform"`
	Sub2APIDailyLimitUSD    *float64   `json:"sub2api_daily_limit_usd,omitempty"`
	Sub2APIWeeklyLimitUSD   *float64   `json:"sub2api_weekly_limit_usd,omitempty"`
	Sub2APIMonthlyLimitUSD  *float64   `json:"sub2api_monthly_limit_usd,omitempty"`
	ValidityDays            int        `json:"validity_days"`
	Note                    string     `json:"note"`
	FulfillmentMode         string     `json:"fulfillment_mode"`
	FulfillmentResult       string     `json:"fulfillment_result"`
	FulfilledVia            string     `json:"fulfilled_via"`
	FulfillmentError        string     `json:"fulfillment_error"`
	Status                  string     `json:"status"`
	ApprovalTokenHash       string     `json:"approval_token_hash"`
	ApprovalTokenExpiresAt  time.Time  `json:"approval_token_expires_at"`
	ApprovedAt              *time.Time `json:"approved_at,omitempty"`
	RejectedAt              *time.Time `json:"rejected_at,omitempty"`
	ConsumedAt              *time.Time `json:"consumed_at,omitempty"`
	NotificationStatus      string     `json:"notification_status"`
	NotificationError       string     `json:"notification_error"`
	NotificationSentAt      *time.Time `json:"notification_sent_at,omitempty"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

type RedeemRequest struct {
	ID                      int64     `json:"id"`
	AccessRequestID         int64     `json:"access_request_id"`
	RequestorUpstreamUserID int64     `json:"requestor_upstream_user_id"`
	RequestorEmail          string    `json:"requestor_email"`
	RequestorUsername       string    `json:"requestor_username"`
	CodeType                string    `json:"code_type"`
	TierID                  int64     `json:"tier_id"`
	Value                   float64   `json:"value"`
	Sub2APIGroupID          *int64    `json:"sub2api_group_id,omitempty"`
	ValidityDays            int       `json:"validity_days"`
	Status                  string    `json:"status"`
	Note                    string    `json:"note"`
	UpstreamCode            string    `json:"upstream_code"`
	UpstreamCodeID          *int64    `json:"upstream_code_id,omitempty"`
	ErrorMessage            string    `json:"error_message"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type RedeemCode struct {
	ID                   int64      `json:"id"`
	RequestID            int64      `json:"request_id"`
	Code                 string     `json:"code"`
	CodeType             string     `json:"code_type"`
	Value                float64    `json:"value"`
	Status               string     `json:"status"`
	UsedByUpstreamUserID *int64     `json:"used_by_upstream_user_id,omitempty"`
	UsedAt               *time.Time `json:"used_at,omitempty"`
	ExpiresAt            *time.Time `json:"expires_at,omitempty"`
	Sub2APICodeID        *int64     `json:"sub2api_code_id,omitempty"`
	Sub2APIGroupID       *int64     `json:"sub2api_group_id,omitempty"`
	ValidityDays         int        `json:"validity_days"`
	LastSyncedAt         *time.Time `json:"last_synced_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type RedeemTier struct {
	ID                     int64     `json:"id"`
	CodeType               string    `json:"code_type"`
	Amount                 float64   `json:"amount"`
	PayAmountCny           float64   `json:"pay_amount_cny"`
	OriginalPayAmountCny   *float64  `json:"original_pay_amount_cny,omitempty"`
	Label                  string    `json:"label"`
	Enabled                bool      `json:"enabled"`
	SortOrder              int       `json:"sort_order"`
	Sub2APIGroupID         *int64    `json:"sub2api_group_id,omitempty"`
	Sub2APIGroupName       string    `json:"sub2api_group_name"`
	Sub2APIGroupPlatform   string    `json:"sub2api_group_platform"`
	Sub2APIDailyLimitUSD   *float64  `json:"sub2api_daily_limit_usd,omitempty"`
	Sub2APIWeeklyLimitUSD  *float64  `json:"sub2api_weekly_limit_usd,omitempty"`
	Sub2APIMonthlyLimitUSD *float64  `json:"sub2api_monthly_limit_usd,omitempty"`
	ValidityDays           int       `json:"validity_days"`
	UpstreamAvailable      bool      `json:"upstream_available"`
	UpstreamError          string    `json:"upstream_error"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type BalanceTier struct {
	ID                   int64     `json:"id"`
	Amount               float64   `json:"amount"`
	PayAmountCny         float64   `json:"pay_amount_cny"`
	OriginalPayAmountCny *float64  `json:"original_pay_amount_cny,omitempty"`
	Label                string    `json:"label"`
	Enabled              bool      `json:"enabled"`
	SortOrder            int       `json:"sort_order"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type SyncState struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SiteBranding struct {
	Title             string `json:"title"`
	Subtitle          string `json:"subtitle"`
	MailSubjectPrefix string `json:"mail_subject_prefix"`
}
