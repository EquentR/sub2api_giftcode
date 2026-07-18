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
	Concurrency             int        `json:"concurrency"`
	ResetCount              int        `json:"reset_count"`
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

type SubscriptionConcurrencyGrant struct {
	ID                     int64      `json:"id"`
	AccessRequestID        int64      `json:"access_request_id"`
	UpstreamUserID         int64      `json:"upstream_user_id"`
	TierID                 int64      `json:"tier_id"`
	Sub2APIGroupID         int64      `json:"sub2api_group_id"`
	DesiredConcurrency     int        `json:"desired_concurrency"`
	UpstreamSubscriptionID *int64     `json:"upstream_subscription_id,omitempty"`
	Status                 string     `json:"status"`
	UpstreamExpiresAt      *time.Time `json:"upstream_expires_at,omitempty"`
	LastSyncedAt           *time.Time `json:"last_synced_at,omitempty"`
	LastError              string     `json:"last_error"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

type RedeemTier struct {
	ID                          int64     `json:"id"`
	CodeType                    string    `json:"code_type"`
	Amount                      float64   `json:"amount"`
	PayAmountCny                float64   `json:"pay_amount_cny"`
	OriginalPayAmountCny        *float64  `json:"original_pay_amount_cny,omitempty"`
	Label                       string    `json:"label"`
	Enabled                     bool      `json:"enabled"`
	SortOrder                   int       `json:"sort_order"`
	Sub2APIGroupID              *int64    `json:"sub2api_group_id,omitempty"`
	Sub2APIGroupName            string    `json:"sub2api_group_name"`
	Sub2APIGroupPlatform        string    `json:"sub2api_group_platform"`
	Sub2APIDailyLimitUSD        *float64  `json:"sub2api_daily_limit_usd,omitempty"`
	Sub2APIWeeklyLimitUSD       *float64  `json:"sub2api_weekly_limit_usd,omitempty"`
	Sub2APIMonthlyLimitUSD      *float64  `json:"sub2api_monthly_limit_usd,omitempty"`
	ValidityDays                int       `json:"validity_days"`
	Concurrency                 int       `json:"concurrency"`
	ResetCount                  int       `json:"reset_count"`
	LegacyResetBackfillEligible bool      `json:"-"`
	UpstreamAvailable           bool      `json:"upstream_available"`
	UpstreamError               string    `json:"upstream_error"`
	CreatedAt                   time.Time `json:"created_at"`
	UpdatedAt                   time.Time `json:"updated_at"`
}

type SubscriptionResetPeriod struct {
	ID                     int64      `json:"id"`
	AccessRequestID        int64      `json:"access_request_id"`
	UpstreamUserID         int64      `json:"upstream_user_id"`
	TierID                 int64      `json:"tier_id"`
	Sub2APIGroupID         int64      `json:"sub2api_group_id"`
	UpstreamSubscriptionID *int64     `json:"upstream_subscription_id,omitempty"`
	ValidityDays           int        `json:"validity_days"`
	ResetLimit             int        `json:"reset_limit"`
	ResetUsed              int        `json:"reset_used"`
	FulfilledAt            time.Time  `json:"fulfilled_at"`
	FulfillmentOrder       int64      `json:"fulfillment_order"`
	PeriodStart            *time.Time `json:"period_start,omitempty"`
	PeriodEnd              *time.Time `json:"period_end,omitempty"`
	Status                 string     `json:"status"`
	InferredFromLegacy     bool       `json:"inferred_from_legacy"`
	MigrationVersion       int        `json:"migration_version"`
	LegacyResetBackfilled  bool       `json:"legacy_reset_backfilled"`
	LegacyIgnored          bool       `json:"legacy_ignored"`
	LegacyIgnoredAt        *time.Time `json:"legacy_ignored_at,omitempty"`
	LegacyIgnoreReason     string     `json:"legacy_ignore_reason"`
	LastSyncedAt           *time.Time `json:"last_synced_at,omitempty"`
	LastError              string     `json:"last_error"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

type SubscriptionResetAttempt struct {
	ID                     int64      `json:"id"`
	RequestID              string     `json:"request_id"`
	PeriodID               *int64     `json:"period_id,omitempty"`
	EntitlementType        string     `json:"entitlement_type"`
	EntitlementID          int64      `json:"entitlement_id"`
	UpstreamUserID         int64      `json:"upstream_user_id"`
	UpstreamSubscriptionID int64      `json:"upstream_subscription_id"`
	ResetDaily             bool       `json:"reset_daily"`
	ResetWeekly            bool       `json:"reset_weekly"`
	ResetMonthly           bool       `json:"reset_monthly"`
	Status                 string     `json:"status"`
	BeforeSnapshotJSON     string     `json:"before_snapshot_json"`
	AfterSnapshotJSON      string     `json:"after_snapshot_json"`
	UpstreamStatus         *int       `json:"upstream_status,omitempty"`
	ResponseStatus         int        `json:"response_status"`
	ResponseReason         string     `json:"response_reason"`
	ErrorMessage           string     `json:"error_message"`
	Resolution             string     `json:"resolution"`
	ReservedAt             time.Time  `json:"reserved_at"`
	CompletedAt            *time.Time `json:"completed_at,omitempty"`
	ConfirmedAt            *time.Time `json:"confirmed_at,omitempty"`
	ConfirmedByUserID      *int64     `json:"confirmed_by_user_id,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

type SubscriptionResetBackfillRun struct {
	ID               int64      `json:"id"`
	TierID           int64      `json:"tier_id"`
	ResetLimit       int        `json:"reset_limit"`
	Status           string     `json:"status"`
	TotalRecords     int        `json:"total_records"`
	ProcessedRecords int        `json:"processed_records"`
	GrantedRecords   int        `json:"granted_records"`
	ErrorMessage     string     `json:"error_message"`
	RetryCount       int        `json:"retry_count"`
	LastErrorAt      *time.Time `json:"last_error_at,omitempty"`
	TriggeredAt      time.Time  `json:"triggered_at"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type SubscriptionResetBonusBatch struct {
	ID                     int64      `json:"id"`
	BatchKey               string     `json:"batch_key"`
	TargetScope            string     `json:"target_scope"`
	SelectedUserIDs        []int64    `json:"selected_user_ids"`
	GroupIDs               []int64    `json:"group_ids"`
	ResetCount             int        `json:"reset_count"`
	Note                   string     `json:"note"`
	PreviewDigest          string     `json:"preview_digest"`
	Status                 string     `json:"status"`
	TotalCandidates        int        `json:"total_candidates"`
	ProcessedCandidates    int        `json:"processed_candidates"`
	GrantedSubscriptions   int        `json:"granted_subscriptions"`
	SkippedSubscriptions   int        `json:"skipped_subscriptions"`
	FailedSubscriptions    int        `json:"failed_subscriptions"`
	OperatorUpstreamUserID int64      `json:"operator_upstream_user_id"`
	OperatorEmail          string     `json:"operator_email"`
	OperatorUsername       string     `json:"operator_username"`
	ErrorMessage           string     `json:"error_message"`
	CreatedAt              time.Time  `json:"created_at"`
	StartedAt              *time.Time `json:"started_at,omitempty"`
	CompletedAt            *time.Time `json:"completed_at,omitempty"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

type SubscriptionResetBonusBatchDetail struct {
	ID                       int64     `json:"id"`
	BatchID                  int64     `json:"batch_id"`
	UpstreamUserID           int64     `json:"upstream_user_id"`
	Sub2APIGroupID           int64     `json:"sub2api_group_id"`
	UpstreamSubscriptionID   int64     `json:"upstream_subscription_id"`
	SubscriptionStartsAt     time.Time `json:"subscription_starts_at"`
	SubscriptionExpiresAt    time.Time `json:"subscription_expires_at"`
	SubscriptionStatus       string    `json:"subscription_status"`
	SubscriptionSnapshotJSON string    `json:"subscription_snapshot_json"`
	Status                   string    `json:"status"`
	Reason                   string    `json:"reason"`
	ErrorMessage             string    `json:"error_message"`
	BonusGrantID             *int64    `json:"bonus_grant_id,omitempty"`
	CreatedAt                time.Time `json:"created_at"`
	UpdatedAt                time.Time `json:"updated_at"`
}

type SubscriptionResetBonusGrant struct {
	ID                       int64      `json:"id"`
	BatchID                  int64      `json:"batch_id"`
	BatchDetailID            int64      `json:"batch_detail_id"`
	UpstreamUserID           int64      `json:"upstream_user_id"`
	Sub2APIGroupID           int64      `json:"sub2api_group_id"`
	UpstreamSubscriptionID   int64      `json:"upstream_subscription_id"`
	ResetLimit               int        `json:"reset_limit"`
	ResetUsed                int        `json:"reset_used"`
	StartsAt                 time.Time  `json:"starts_at"`
	ExpiresAt                time.Time  `json:"expires_at"`
	Status                   string     `json:"status"`
	SubscriptionSnapshotJSON string     `json:"subscription_snapshot_json"`
	LastSyncedAt             *time.Time `json:"last_synced_at,omitempty"`
	LastError                string     `json:"last_error"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at"`
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

type CompensationBatch struct {
	ID                           int64      `json:"id"`
	BatchKey                     string     `json:"batch_key"`
	SubscriptionDays             int        `json:"subscription_days"`
	BalanceAmount                float64    `json:"balance_amount"`
	ExcludedDomains              []string   `json:"excluded_domains"`
	Note                         string     `json:"note"`
	OperatorUpstreamUserID       int64      `json:"operator_upstream_user_id"`
	OperatorEmail                string     `json:"operator_email"`
	OperatorUsername             string     `json:"operator_username"`
	Status                       string     `json:"status"`
	TotalUsers                   int        `json:"total_users"`
	ExcludedUsers                int        `json:"excluded_users"`
	SubscriptionCompensatedUsers int        `json:"subscription_compensated_users"`
	BalanceCompensatedUsers      int        `json:"balance_compensated_users"`
	SkippedZeroBalanceUsers      int        `json:"skipped_zero_balance_users"`
	FailedUsers                  int        `json:"failed_users"`
	DetailCount                  int        `json:"detail_count"`
	UpstreamError                string     `json:"upstream_error"`
	CreatedAt                    time.Time  `json:"created_at"`
	UpdatedAt                    time.Time  `json:"updated_at"`
	CompletedAt                  *time.Time `json:"completed_at,omitempty"`
}

type CompensationBatchDetail struct {
	ID                      int64     `json:"id"`
	BatchID                 int64     `json:"batch_id"`
	DetailKey               string    `json:"detail_key"`
	UpstreamUserID          int64     `json:"upstream_user_id"`
	UserEmail               string    `json:"user_email"`
	UserUsername            string    `json:"user_username"`
	UserBalance             float64   `json:"user_balance"`
	Excluded                bool      `json:"excluded"`
	ExcludedDomain          string    `json:"excluded_domain"`
	HasActiveSubscriptions  bool      `json:"has_active_subscriptions"`
	ActiveSubscriptionCount int       `json:"active_subscription_count"`
	ActiveSubscriptionIDs   []int64   `json:"active_subscription_ids"`
	DecisionType            string    `json:"decision_type"`
	ActionType              string    `json:"action_type"`
	SubscriptionDays        int       `json:"subscription_days"`
	BalanceAmount           float64   `json:"balance_amount"`
	Status                  string    `json:"status"`
	ResultReason            string    `json:"result_reason"`
	UpstreamReferenceJSON   string    `json:"upstream_reference_json"`
	RemarkRequested         bool      `json:"remark_requested"`
	RemarkApplied           bool      `json:"remark_applied"`
	RemarkError             string    `json:"remark_error"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type SiteBranding struct {
	Title             string `json:"title"`
	Subtitle          string `json:"subtitle"`
	MailSubjectPrefix string `json:"mail_subject_prefix"`
}
