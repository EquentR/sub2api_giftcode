package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
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
