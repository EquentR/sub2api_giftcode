package httpapi

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"sub2api-giftcode/backend/internal/models"
)

func (h *Handlers) GetSiteBranding(c *gin.Context) {
	branding, err := h.service.GetSiteBranding(c.Request.Context())
	if err != nil {
		status, msg, reason := statusForError(err)
		if reason != "" {
			writeErrorReason(c, status, msg, reason)
		} else {
			writeError(c, status, msg)
		}
		return
	}
	writeSuccess(c, branding)
}

func (h *Handlers) UpdateSiteBranding(c *gin.Context) {
	var req SiteBrandingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	branding, err := h.service.ReplaceSiteBranding(c.Request.Context(), models.SiteBranding{
		Title:             strings.TrimSpace(req.Title),
		Subtitle:          strings.TrimSpace(req.Subtitle),
		MailSubjectPrefix: strings.TrimSpace(req.MailSubjectPrefix),
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
	writeSuccess(c, branding)
}
