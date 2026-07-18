package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (h *Handlers) ListSubscriptions(c *gin.Context) {
	sessionUser, ok := getSessionUser(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	items, err := h.service.ListSubscriptions(c.Request.Context(), sessionUser.User.ID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writeSuccess(c, items)
}

func (h *Handlers) ResetSubscriptionQuota(c *gin.Context) {
	sessionUser, ok := getSessionUser(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	subscriptionID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || subscriptionID <= 0 {
		writeError(c, http.StatusBadRequest, "invalid id")
		return
	}
	var request SubscriptionResetRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "bad request")
		return
	}
	result, err := h.service.ResetSubscriptionQuota(c.Request.Context(), sessionUser.User.ID, subscriptionID, request.RequestID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	status := http.StatusOK
	if result.Operation.Status == "reserved" || result.Operation.Status == "uncertain" {
		status = http.StatusAccepted
	} else if result.Operation.Status == "failed" {
		status = http.StatusConflict
	}
	c.JSON(status, Response{Code: 0, Message: "success", Reason: result.Operation.ResponseReason, Data: result})
}

func (h *Handlers) ListPendingSubscriptionResetAttempts(c *gin.Context) {
	items, err := h.service.ListPendingSubscriptionResetAttempts(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writeSuccess(c, items)
}

func (h *Handlers) ListSubscriptionResetBackfillRuns(c *gin.Context) {
	items, err := h.service.ListSubscriptionResetBackfillRuns(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writeSuccess(c, items)
}

func (h *Handlers) ResolveSubscriptionResetAttempt(c *gin.Context) {
	sessionUser, ok := getSessionUser(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	attemptID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || attemptID <= 0 {
		writeError(c, http.StatusBadRequest, "invalid id")
		return
	}
	var request SubscriptionResetResolutionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "bad request")
		return
	}
	attempt, err := h.service.ResolveSubscriptionResetAttempt(c.Request.Context(), attemptID, sessionUser.User.ID, request.Resolution)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writeSuccess(c, attempt)
}

func writeServiceError(c *gin.Context, err error) {
	status, message, reason := statusForError(err)
	if reason != "" {
		writeErrorReason(c, status, message, reason)
		return
	}
	writeError(c, status, message)
}
