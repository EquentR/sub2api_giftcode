package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"sub2api-giftcode/backend/internal/models"
)

func (h *Handlers) CreateAccessRequest(c *gin.Context) {
	sessionUser, ok := getSessionUser(c)
	if !ok || sessionUser == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req AccessRequestCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.service.CreateAccessRequest(c.Request.Context(), sessionUser.Session.ID, req.TierID, req.Note, req.FulfillmentMode)
	if err != nil {
		status, msg, reason := statusForError(err)
		if reason != "" {
			writeErrorReason(c, status, msg, reason)
		} else {
			writeError(c, status, msg)
		}
		return
	}
	writeCreated(c, result)
}

func (h *Handlers) ListMyAccessRequests(c *gin.Context) {
	sessionUser, ok := getSessionUser(c)
	if !ok || sessionUser == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	items, err := h.service.ListAccessRequestsForUser(c.Request.Context(), sessionUser.User.ID)
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

func (h *Handlers) ListAllAccessRequests(c *gin.Context) {
	items, err := h.service.ListAllAccessRequests(c.Request.Context())
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

func (h *Handlers) GetAccessRequest(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid id")
		return
	}
	req, err := h.service.GetAccessRequestByID(c.Request.Context(), id)
	if err != nil {
		status, msg, reason := statusForError(err)
		if reason != "" {
			writeErrorReason(c, status, msg, reason)
		} else {
			writeError(c, status, msg)
		}
		return
	}
	writeSuccess(c, req)
}

func (h *Handlers) ApproveAccessRequest(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid id")
		return
	}
	req, code, err := h.service.ApproveAccessRequestByID(c.Request.Context(), id)
	if err != nil {
		status, msg, reason := statusForError(err)
		if reason != "" {
			writeErrorReason(c, status, msg, reason)
		} else {
			writeError(c, status, msg)
		}
		return
	}
	writeSuccess(c, RedeemIssueResponse{Request: req, Code: code})
}

func (h *Handlers) RejectAccessRequest(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid id")
		return
	}
	req, err := h.service.RejectAccessRequestByID(c.Request.Context(), id)
	if err != nil {
		status, msg, reason := statusForError(err)
		if reason != "" {
			writeErrorReason(c, status, msg, reason)
		} else {
			writeError(c, status, msg)
		}
		return
	}
	writeSuccess(c, req)
}

func (h *Handlers) ListBalanceTiers(c *gin.Context) {
	items, err := h.service.ListBalanceTiers(c.Request.Context())
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

func (h *Handlers) ListRedeemTiers(c *gin.Context) {
	items, err := h.service.ListRedeemTiers(c.Request.Context(), false)
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

func (h *Handlers) ListEnabledRedeemTiers(c *gin.Context) {
	items, err := h.service.ListRedeemTiers(c.Request.Context(), true)
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

func (h *Handlers) UpdateRedeemTiers(c *gin.Context) {
	var req []RedeemTierRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	tiers := make([]models.RedeemTier, 0, len(req))
	for _, tier := range req {
		tiers = append(tiers, models.RedeemTier{
			ID:                   tier.ID,
			CodeType:             tier.CodeType,
			Amount:               tier.Amount,
			PayAmountCny:         tier.PayAmountCny,
			OriginalPayAmountCny: tier.OriginalPayAmountCny,
			Label:                tier.Label,
			Enabled:              tier.Enabled,
			SortOrder:            tier.SortOrder,
			Sub2APIGroupID:       tier.Sub2APIGroupID,
			ValidityDays:         tier.ValidityDays,
		})
	}
	items, err := h.service.ReplaceRedeemTiers(c.Request.Context(), tiers)
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

func (h *Handlers) ListSubscriptionGroups(c *gin.Context) {
	items, err := h.service.ListSubscriptionGroups(c.Request.Context())
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

func (h *Handlers) UpdateBalanceTiers(c *gin.Context) {
	var req []BalanceTierRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	tiers := make([]models.BalanceTier, 0, len(req))
	for _, tier := range req {
		tiers = append(tiers, models.BalanceTier{
			ID:                   tier.ID,
			Amount:               tier.Amount,
			PayAmountCny:         tier.PayAmountCny,
			OriginalPayAmountCny: tier.OriginalPayAmountCny,
			Label:                tier.Label,
			Enabled:              tier.Enabled,
			SortOrder:            tier.SortOrder,
		})
	}
	items, err := h.service.ReplaceBalanceTiers(c.Request.Context(), tiers)
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

func (h *Handlers) CreateRedeemRequest(c *gin.Context) {
	sessionUser, ok := getSessionUser(c)
	if !ok || sessionUser == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req RedeemRequestCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	redeemReq, code, err := h.service.CreateRedeemRequest(c.Request.Context(), sessionUser.Session.ID, req.TierID, req.Note)
	if err != nil {
		status, msg, reason := statusForError(err)
		if reason != "" {
			writeErrorReason(c, status, msg, reason)
		} else {
			writeError(c, status, msg)
		}
		return
	}
	writeCreated(c, RedeemIssueResponse{Request: redeemReq, Code: code})
}

func (h *Handlers) ListMyRedeemRequests(c *gin.Context) {
	sessionUser, ok := getSessionUser(c)
	if !ok || sessionUser == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	items, err := h.service.ListRedeemRequestsForUser(c.Request.Context(), sessionUser.User.ID)
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

func (h *Handlers) ListMyRedeemCodes(c *gin.Context) {
	sessionUser, ok := getSessionUser(c)
	if !ok || sessionUser == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	items, err := h.service.ListRedeemCodesForUser(c.Request.Context(), sessionUser.User.ID)
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
