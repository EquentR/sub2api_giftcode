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
	TierID int64  `json:"tier_id" binding:"required,gt=0"`
	Note   string `json:"note"`
}

type AccessRequestConfirmRequest struct {
	Token string `json:"token" binding:"required"`
}

type RedeemRequestCreateRequest struct {
	TierID int64  `json:"tier_id" binding:"required,gt=0"`
	Note   string `json:"note"`
}

type SiteBrandingRequest struct {
	Title             string `json:"title" binding:"required"`
	Subtitle          string `json:"subtitle"`
	MailSubjectPrefix string `json:"mail_subject_prefix"`
}

type BalanceTierRequest struct {
	ID           int64   `json:"id"`
	Amount       float64 `json:"amount" binding:"required,gt=0"`
	PayAmountCny float64 `json:"pay_amount_cny" binding:"required,gt=0"`
	Label        string  `json:"label"`
	Enabled      bool    `json:"enabled"`
	SortOrder    int     `json:"sort_order"`
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
		return http.StatusConflict, "conflict", ""
	case errors.Is(err, app.ErrBadRequest):
		return http.StatusBadRequest, "bad request", ""
	}
	var apiErr *sub2api.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status, apiErr.Message, apiErr.Reason
	}
	if errors.Is(err, app.ErrUpstreamFailed) {
		return http.StatusBadGateway, "upstream failed", ""
	}
	return http.StatusInternalServerError, "internal server error", ""
}
