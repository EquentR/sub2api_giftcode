package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

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

func bindAuxSchedulerRuleRequest(c *gin.Context) (*AuxSchedulerRuleRequest, error) {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, err
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	for _, legacyKey := range []string{"primary_account_ids", "backup_account_ids"} {
		if _, ok := body[legacyKey]; ok {
			return nil, fmt.Errorf("旧版 %s 契约已移除，请使用 lanes", legacyKey)
		}
	}
	var req AuxSchedulerRuleRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("规则名称不能为空")
	}
	return &req, nil
}

func (h *Handlers) CreateAuxSchedulerRule(c *gin.Context) {
	req, err := bindAuxSchedulerRuleRequest(c)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	rule, err := h.service.CreateAuxSchedulerRule(c.Request.Context(), app.AuxSchedulerRuleInput{
		Name:            req.Name,
		Enabled:         req.Enabled,
		ModelNames:      req.ModelNames,
		Lanes:           req.Lanes,
		MaximumAutoLane: req.MaximumAutoLane,
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
	req, err := bindAuxSchedulerRuleRequest(c)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	rule, err := h.service.UpdateAuxSchedulerRule(c.Request.Context(), id, app.AuxSchedulerRuleInput{
		Name:            req.Name,
		Enabled:         req.Enabled,
		ModelNames:      req.ModelNames,
		Lanes:           req.Lanes,
		MaximumAutoLane: req.MaximumAutoLane,
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
