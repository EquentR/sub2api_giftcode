package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"sub2api-giftcode/backend/internal/app"
	"sub2api-giftcode/backend/internal/sub2api"
)

type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Reason  string `json:"reason,omitempty"`
	Data    any    `json:"data,omitempty"`
}

type PaginatedData struct {
	Items    any   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	Pages    int   `json:"pages"`
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type EmbeddedLoginRequest struct {
	Token  string `json:"token" binding:"required"`
	UserID *int64 `json:"user_id,omitempty"`
}

type AccessRequestCreateRequest struct {
	TierID          int64  `json:"tier_id" binding:"required,gt=0"`
	Note            string `json:"note"`
	FulfillmentMode string `json:"fulfillment_mode"`
}

type AccessRequestConfirmRequest struct {
	Token string `json:"token" binding:"required"`
}

type RedeemRequestCreateRequest struct {
	TierID int64  `json:"tier_id" binding:"required,gt=0"`
	Note   string `json:"note"`
}

type SubscriptionResetRequest struct {
	RequestID string `json:"request_id" binding:"required"`
}

type SubscriptionResetResolutionRequest struct {
	Resolution string `json:"resolution" binding:"required,oneof=consumed released"`
}

type SiteBrandingRequest struct {
	Title             string `json:"title" binding:"required"`
	Subtitle          string `json:"subtitle"`
	MailSubjectPrefix string `json:"mail_subject_prefix"`
}

type BalanceTierRequest struct {
	ID                   int64    `json:"id"`
	Amount               float64  `json:"amount" binding:"required,gt=0"`
	PayAmountCny         float64  `json:"pay_amount_cny" binding:"required,gt=0"`
	OriginalPayAmountCny *float64 `json:"original_pay_amount_cny,omitempty"`
	Label                string   `json:"label"`
	Enabled              bool     `json:"enabled"`
	SortOrder            int      `json:"sort_order"`
}

type RedeemTierRequest struct {
	ID                   int64    `json:"id"`
	CodeType             string   `json:"code_type" binding:"required"`
	Amount               float64  `json:"amount"`
	PayAmountCny         float64  `json:"pay_amount_cny" binding:"required,gt=0"`
	OriginalPayAmountCny *float64 `json:"original_pay_amount_cny,omitempty"`
	Label                string   `json:"label"`
	Enabled              bool     `json:"enabled"`
	SortOrder            int      `json:"sort_order"`
	Sub2APIGroupID       *int64   `json:"sub2api_group_id,omitempty"`
	ValidityDays         int      `json:"validity_days"`
	Concurrency          int      `json:"concurrency"`
	ResetCount           int      `json:"reset_count"`
}

type OpenAIAccountUserAgentRequest struct {
	UserAgent string `json:"user_agent"`
}

type AuxSchedulerRuleRequest struct {
	Name            string    `json:"name"`
	Enabled         bool      `json:"enabled"`
	ModelNames      []string  `json:"model_names"`
	Lanes           [][]int64 `json:"lanes"`
	MaximumAutoLane int       `json:"maximum_auto_lane"`
}

type CompensationBatchCreateRequest struct {
	SubscriptionDays int      `json:"subscription_days" binding:"required,gt=0"`
	BalanceAmount    float64  `json:"balance_amount" binding:"required,gt=0"`
	ExcludedDomains  []string `json:"excluded_domains"`
	Note             string   `json:"note"`
}

type SubscriptionResetBonusPreviewRequest struct {
	TargetScope     string  `json:"target_scope" binding:"required,oneof=all selected"`
	SelectedUserIDs []int64 `json:"selected_user_ids"`
	GroupIDs        []int64 `json:"group_ids" binding:"required,min=1"`
	ResetCount      int     `json:"reset_count" binding:"required,gt=0"`
	Note            string  `json:"note"`
}

type SubscriptionResetBonusCreateRequest struct {
	PreviewToken string `json:"preview_token" binding:"required"`
}

type SubscriptionExtensionResolutionRequest struct {
	Resolution string `json:"resolution" binding:"required,oneof=applied released"`
}

type LoginResponse struct {
	User             any    `json:"user"`
	IsAdmin          bool   `json:"is_admin"`
	SessionExpiresAt string `json:"session_expires_at"`
	SessionToken     string `json:"session_token,omitempty"`
}

type AuthMeResponse = LoginResponse

type EmbeddedLoginResponse = LoginResponse

type RedeemIssueResponse struct {
	Request any `json:"request"`
	Code    any `json:"code,omitempty"`
}

func writeSuccess(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Response{Code: 0, Message: "success", Data: data})
}

func writeCreated(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, Response{Code: 0, Message: "success", Data: data})
}

func writeError(c *gin.Context, status int, message string) {
	c.JSON(status, Response{Code: status, Message: message})
}

func writeErrorReason(c *gin.Context, status int, message, reason string) {
	c.JSON(status, Response{Code: status, Message: message, Reason: reason})
}

func statusForError(err error) (int, string, string) {
	if err == nil {
		return http.StatusOK, "", ""
	}
	switch {
	case errors.Is(err, app.ErrUnauthorized):
		return http.StatusUnauthorized, "unauthorized", ""
	case errors.Is(err, app.ErrForbidden):
		return http.StatusForbidden, "forbidden", ""
	case errors.Is(err, app.ErrNotFound):
		return http.StatusNotFound, "not found", ""
	case errors.Is(err, app.ErrConflict):
		return http.StatusConflict, "conflict", app.StableReason(err)
	case errors.Is(err, app.ErrBadRequest):
		var concurrencyConflict *app.TierConcurrencyConflictError
		if errors.As(err, &concurrencyConflict) {
			return http.StatusBadRequest, "bad request", concurrencyConflict.Error()
		}
		return http.StatusBadRequest, err.Error(), ""
	}
	var apiErr *sub2api.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status, apiErr.Message, apiErr.Reason
	}
	if errors.Is(err, app.ErrUpstreamFailed) {
		return http.StatusBadGateway, "upstream failed", ""
	}
	if errors.Is(err, app.ErrUpstreamUnavailable) {
		return http.StatusBadGateway, "upstream unavailable", app.StableReason(err)
	}
	return http.StatusInternalServerError, "internal server error", ""
}
