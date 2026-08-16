package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"sub2api-giftcode/backend/internal/app"
)

func (h *Handlers) ListAuxSchedulerRules(c *gin.Context) {
	items, err := h.service.ListAuxSchedulerRules(c.Request.Context())
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

func (h *Handlers) CreateAuxSchedulerRule(c *gin.Context) {
	var req AuxSchedulerRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	rule, err := h.service.CreateAuxSchedulerRule(c.Request.Context(), app.AuxSchedulerRuleInput{
		Name:              req.Name,
		Enabled:           req.Enabled,
		ModelNames:        req.ModelNames,
		Lanes:             req.Lanes,
		MaximumAutoLane:   req.MaximumAutoLane,
		PrimaryAccountIDs: req.PrimaryAccountIDs,
		BackupAccountIDs:  req.BackupAccountIDs,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writeCreated(c, rule)
}

func (h *Handlers) UpdateAuxSchedulerRule(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid id")
		return
	}
	var req AuxSchedulerRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	rule, err := h.service.UpdateAuxSchedulerRule(c.Request.Context(), id, app.AuxSchedulerRuleInput{
		Name:              req.Name,
		Enabled:           req.Enabled,
		ModelNames:        req.ModelNames,
		Lanes:             req.Lanes,
		MaximumAutoLane:   req.MaximumAutoLane,
		PrimaryAccountIDs: req.PrimaryAccountIDs,
		BackupAccountIDs:  req.BackupAccountIDs,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writeSuccess(c, rule)
}

func (h *Handlers) DeleteAuxSchedulerRule(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.service.DeleteAuxSchedulerRule(c.Request.Context(), id); err != nil {
		writeServiceError(c, err)
		return
	}
	writeSuccess(c, gin.H{"deleted": true})
}

func (h *Handlers) CheckAuxSchedulerRule(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid id")
		return
	}
	rule, err := h.service.CheckAuxSchedulerRule(c.Request.Context(), id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writeSuccess(c, rule)
}
