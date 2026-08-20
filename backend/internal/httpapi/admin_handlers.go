package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"sub2api-giftcode/backend/internal/app"
)

func (h *Handlers) Stats(c *gin.Context) {
	stats, err := h.service.Stats(c.Request.Context())
	if err != nil {
		status, msg, reason := statusForError(err)
		if reason != "" {
			writeErrorReason(c, status, msg, reason)
		} else {
			writeError(c, status, msg)
		}
		return
	}
	writeSuccess(c, stats)
}

func (h *Handlers) SubscriptionConcurrencyMonitorStatus(c *gin.Context) {
	status, err := h.service.SubscriptionConcurrencyMonitorStatus(c.Request.Context())
	if err != nil {
		statusCode, message, reason := statusForError(err)
		if reason != "" {
			writeErrorReason(c, statusCode, message, reason)
		} else {
			writeError(c, statusCode, message)
		}
		return
	}
	writeSuccess(c, status)
}

func (h *Handlers) SubscriptionConcurrencyMonitorDetails(c *gin.Context) {
	items, err := h.service.SubscriptionConcurrencyMonitorDetails(c.Request.Context())
	if err != nil {
		statusCode, message, reason := statusForError(err)
		if reason != "" {
			writeErrorReason(c, statusCode, message, reason)
		} else {
			writeError(c, statusCode, message)
		}
		return
	}
	writeSuccess(c, items)
}

func (h *Handlers) ListUsers(c *gin.Context) {
	items, err := h.service.ListUsers(c.Request.Context())
	if err != nil {
		status, msg, reason := statusForError(err)
		if reason != "" {
			writeErrorReason(c, status, msg, reason)
		} else {
			writeError(c, status, msg)
		}
		return
	}
	writeSuccess(c, items)
}

func (h *Handlers) ListUserRedeemCodes(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid id")
		return
	}
	items, err := h.service.ListRedeemCodesForUser(c.Request.Context(), id)
	if err != nil {
		status, msg, reason := statusForError(err)
		if reason != "" {
			writeErrorReason(c, status, msg, reason)
		} else {
			writeError(c, status, msg)
		}
		return
	}
	writeSuccess(c, items)
}

func (h *Handlers) ListAllRedeemCodes(c *gin.Context) {
	items, err := h.service.ListAllRedeemCodes(c.Request.Context())
	if err != nil {
		status, msg, reason := statusForError(err)
		if reason != "" {
			writeErrorReason(c, status, msg, reason)
		} else {
			writeError(c, status, msg)
		}
		return
	}
	writeSuccess(c, items)
}

func (h *Handlers) SyncRedeemCodes(c *gin.Context) {
	updated, err := h.service.SyncRedeemCodes(c.Request.Context())
	if err != nil {
		status, msg, reason := statusForError(err)
		if reason != "" {
			writeErrorReason(c, status, msg, reason)
		} else {
			writeError(c, status, msg)
		}
		return
	}
	writeSuccess(c, gin.H{"updated": updated})
}

func (h *Handlers) ListOpenAIAccounts(c *gin.Context) {
	items, err := h.service.ListOpenAIAccounts(c.Request.Context())
	if err != nil {
		status, msg, reason := statusForError(err)
		if reason != "" {
			writeErrorReason(c, status, msg, reason)
		} else {
			writeError(c, status, msg)
		}
		return
	}
	writeSuccess(c, items)
}

func (h *Handlers) UpdateOpenAIAccountUserAgent(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid id")
		return
	}
	var req OpenAIAccountUserAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	account, err := h.service.UpdateOpenAIAccountUserAgent(c.Request.Context(), id, req.UserAgent)
	if err != nil {
		status, msg, reason := statusForError(err)
		if reason != "" {
			writeErrorReason(c, status, msg, reason)
		} else {
			writeError(c, status, msg)
		}
		return
	}
	writeSuccess(c, account)
}

func (h *Handlers) CreateCompensationBatch(c *gin.Context) {
	sessionUser, ok := getSessionUser(c)
	if !ok || sessionUser == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req CompensationBatchCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	batch, err := h.service.RunCompensationBatch(c.Request.Context(), sessionUser, app.CompensationBatchInput{
		CompensateSubscriptions: req.CompensateSubscriptions,
		CompensateBalance:       req.CompensateBalance,
		SubscriptionDays:        req.SubscriptionDays,
		BalanceAmount:           req.BalanceAmount,
		ExcludedDomains:         req.ExcludedDomains,
		Note:                    req.Note,
	})
	if err != nil {
		status, msg, reason := statusForError(err)
		if reason != "" {
			writeErrorReason(c, status, msg, reason)
		} else {
			writeError(c, status, msg)
		}
		return
	}
	writeCreated(c, batch)
}

func (h *Handlers) ListCompensationBatches(c *gin.Context) {
	items, err := h.service.ListCompensationBatches(c.Request.Context())
	if err != nil {
		status, msg, reason := statusForError(err)
		if reason != "" {
			writeErrorReason(c, status, msg, reason)
		} else {
			writeError(c, status, msg)
		}
		return
	}
	writeSuccess(c, items)
}

func (h *Handlers) ListCompensationBatchDetails(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid id")
		return
	}
	items, err := h.service.ListCompensationBatchDetails(c.Request.Context(), id)
	if err != nil {
		status, msg, reason := statusForError(err)
		if reason != "" {
			writeErrorReason(c, status, msg, reason)
		} else {
			writeError(c, status, msg)
		}
		return
	}
	writeSuccess(c, items)
}

func (h *Handlers) PreviewSubscriptionResetBonusBatch(c *gin.Context) {
	sessionUser, ok := getSessionUser(c)
	if !ok || sessionUser == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req SubscriptionResetBonusPreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	preview, err := h.service.PreviewSubscriptionResetBonus(c.Request.Context(), sessionUser, app.SubscriptionResetBonusPreviewInput{
		TargetScope: req.TargetScope, SelectedUserIDs: req.SelectedUserIDs, GroupIDs: req.GroupIDs,
		ResetCount: req.ResetCount, Note: req.Note,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writeSuccess(c, preview)
}

func (h *Handlers) CreateSubscriptionResetBonusBatch(c *gin.Context) {
	sessionUser, ok := getSessionUser(c)
	if !ok || sessionUser == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req SubscriptionResetBonusCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	batch, err := h.service.CreateSubscriptionResetBonusBatch(c.Request.Context(), sessionUser, req.PreviewToken)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, Response{Code: 0, Message: "success", Data: batch})
}

func (h *Handlers) ListSubscriptionResetBonusBatches(c *gin.Context) {
	items, err := h.service.ListSubscriptionResetBonusBatches(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writeSuccess(c, items)
}

func (h *Handlers) ListSubscriptionResetBonusBatchDetails(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(c, http.StatusBadRequest, "invalid id")
		return
	}
	items, err := h.service.ListSubscriptionResetBonusBatchDetails(c.Request.Context(), id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writeSuccess(c, items)
}

func (h *Handlers) ListSubscriptionExtensionEvents(c *gin.Context) {
	items, err := h.service.ListSubscriptionExtensionEvents(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writeSuccess(c, items)
}

func (h *Handlers) ResolveSubscriptionExtensionEvent(c *gin.Context) {
	sessionUser, ok := getSessionUser(c)
	if !ok || sessionUser == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(c, http.StatusBadRequest, "invalid id")
		return
	}
	var req SubscriptionExtensionResolutionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	event, err := h.service.ResolveSubscriptionExtensionEvent(c.Request.Context(), id, sessionUser.User.ID, req.Resolution)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writeSuccess(c, event)
}
