package httpapi

import (
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"sub2api-giftcode/backend/internal/app"
	"sub2api-giftcode/backend/internal/config"
	"sub2api-giftcode/backend/internal/models"
)

type Handlers struct {
	cfg     *config.RuntimeConfig
	service *app.Service
}

func (h *Handlers) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	sessionUser, err := h.service.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		status, msg, reason := statusForError(err)
		if reason != "" {
			writeErrorReason(c, status, msg, reason)
		} else {
			writeError(c, status, msg)
		}
		return
	}
	if err := setSessionCookie(c, h.cfg.Session.CookieSecret, sessionUser.Session.ID, isSecureBaseURL(h.cfg.App.BaseURL)); err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	sessionToken, err := sessionTokenFor(h.cfg.Session.CookieSecret, sessionUser.Session.ID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(c, LoginResponse{
		User:             sessionUser.User,
		IsAdmin:          sessionUser.IsAdmin,
		SessionExpiresAt: sessionUser.Session.ExpiresAt.UTC().Format(time.RFC3339Nano),
		SessionToken:     sessionToken,
	})
}

func (h *Handlers) EmbeddedLogin(c *gin.Context) {
	var req EmbeddedLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	sessionUser, err := h.service.LoginWithAccessToken(c.Request.Context(), req.Token, req.UserID)
	if err != nil {
		status, msg, reason := statusForError(err)
		if reason != "" {
			writeErrorReason(c, status, msg, reason)
		} else {
			writeError(c, status, msg)
		}
		return
	}
	if err := setSessionCookie(c, h.cfg.Session.CookieSecret, sessionUser.Session.ID, isSecureBaseURL(h.cfg.App.BaseURL)); err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	sessionToken, err := sessionTokenFor(h.cfg.Session.CookieSecret, sessionUser.Session.ID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(c, EmbeddedLoginResponse{
		User:             sessionUser.User,
		IsAdmin:          sessionUser.IsAdmin,
		SessionExpiresAt: sessionUser.Session.ExpiresAt.UTC().Format(time.RFC3339Nano),
		SessionToken:     sessionToken,
	})
}

func (h *Handlers) Logout(c *gin.Context) {
	sessionUser, ok := getSessionUser(c)
	if ok && sessionUser != nil {
		_ = h.service.Logout(c.Request.Context(), sessionUser.Session.ID)
	}
	clearSessionCookie(c, isSecureBaseURL(h.cfg.App.BaseURL))
	writeSuccess(c, gin.H{"message": "logged out"})
}

func (h *Handlers) Me(c *gin.Context) {
	sessionUser, ok := getSessionUser(c)
	if !ok || sessionUser == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	sessionToken, err := sessionTokenFor(h.cfg.Session.CookieSecret, sessionUser.Session.ID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(c, AuthMeResponse{
		User:             sessionUser.User,
		IsAdmin:          sessionUser.IsAdmin,
		SessionExpiresAt: sessionUser.Session.ExpiresAt.UTC().Format(time.RFC3339Nano),
		SessionToken:     sessionToken,
	})
}

func (h *Handlers) ConfirmAccessRequest(c *gin.Context) {
	token := approvalTokenFromRequest(c)
	req, err := h.service.ConfirmAccessRequest(c.Request.Context(), token)
	if err != nil {
		h.writeApprovalResultPage(c, req, err)
		return
	}
	frontend := h.appReturnURL()
	html := "<html><body style=\"font-family: sans-serif; padding: 24px;\">" +
		"<h2>已批准</h2>" +
		"<p>申请 #" + fmtInt64(req.ID) + " 已批准，兑换码已经下发。</p>" +
		"<p><a href=\"" + htmlEscape(frontend) + "\">打开应用</a></p>" +
		"</body></html>"
	writeHTML(c, http.StatusOK, html)
}

func (h *Handlers) ConfirmAccessRequestJSON(c *gin.Context) {
	var reqBody AccessRequestConfirmRequest
	if err := c.ShouldBindJSON(&reqBody); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	req, err := h.service.ConfirmAccessRequest(c.Request.Context(), reqBody.Token)
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

func (h *Handlers) PreviewAccessRequestJSON(c *gin.Context) {
	token := approvalTokenFromRequest(c)
	req, err := h.service.PreviewAccessRequestByToken(c.Request.Context(), token)
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

func (h *Handlers) ShowAccessRequestConfirmation(c *gin.Context) {
	token := approvalTokenFromRequest(c)
	req, err := h.service.PreviewAccessRequestByToken(c.Request.Context(), token)
	if err != nil {
		h.writeApprovalResultPage(c, req, err)
		return
	}
	if req.Status != "pending" {
		h.writeApprovalResultPage(c, req, app.ErrConflict)
		return
	}
	page := "<html><body style=\"font-family: sans-serif; padding: 24px; max-width: 720px;\">" +
		"<h2>确认审批并发码</h2>" +
		"<p style=\"color:#b45309;\">请核对申请人、理由和档位。点击下方按钮后会立即审批并下发兑换码。</p>" +
		approvalRequestDetailsHTML(req) +
		"<form method=\"post\" action=\"/api/admin/redeem-access-requests/confirm\" style=\"margin-top: 20px;\">" +
		"<input type=\"hidden\" name=\"token\" value=\"" + htmlEscape(token) + "\">" +
		"<button type=\"submit\" style=\"background:#2563eb;color:white;border:0;border-radius:6px;padding:10px 16px;cursor:pointer;\">确认审批并发码</button>" +
		"</form>" +
		"</body></html>"
	writeHTML(c, http.StatusOK, page)
}

func (h *Handlers) writeApprovalResultPage(c *gin.Context, req *models.AccessRequest, err error) {
	_ = err
	title := "审批失败"
	detail := "这次审批没有完成，请返回应用后重试。"
	if req != nil {
		switch req.Status {
		case "expired":
			title = "链接已过期"
			detail = "这条审批链接已经过期，请联系管理员重新发送。"
		case "approved", "consumed":
			title = "已经处理"
			detail = "这条审批已经完成。"
		case "rejected":
			title = "已经拒绝"
			detail = "这条申请已经被拒绝。"
		}
	}
	frontend := h.appReturnURL()
	page := "<html><body style=\"font-family: sans-serif; padding: 24px;\">" +
		"<h2>" + htmlEscape(title) + "</h2>" +
		"<p>" + htmlEscape(detail) + "</p>" +
		"<p><a href=\"" + htmlEscape(frontend) + "\">打开应用</a></p>" +
		"</body></html>"
	writeHTML(c, http.StatusOK, page)
}

func fmtInt64(v int64) string {
	return fmt.Sprintf("%d", v)
}

func approvalTokenFromRequest(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if token := strings.TrimSpace(c.PostForm("token")); token != "" {
		return token
	}
	return strings.TrimSpace(c.Query("token"))
}

func (h *Handlers) appReturnURL() string {
	frontend := strings.TrimSpace(h.cfg.App.FrontendURL)
	if frontend != "" {
		return strings.TrimRight(frontend, "/")
	}
	return strings.TrimRight(h.cfg.App.BaseURL, "/")
}

func approvalRequestDetailsHTML(req *models.AccessRequest) string {
	if req == nil {
		return ""
	}
	note := strings.TrimSpace(req.Note)
	if note == "" {
		note = "-"
	}
	return "<table style=\"border-collapse:collapse;width:100%;margin-top:16px;\">" +
		approvalDetailRow("申请编号", fmt.Sprintf("#%d", req.ID)) +
		approvalDetailRow("申请人", fmt.Sprintf("%s (%s)", req.RequestorUsername, req.RequestorEmail)) +
		approvalDetailRow("档位", fmt.Sprintf("#%d", req.TierID)) +
		approvalDetailRow("到账金额", fmt.Sprintf("%.0f 美元", req.Amount)) +
		approvalDetailRow("实付金额", fmt.Sprintf("%.0f 人民币", req.PayAmountCny)) +
		approvalDetailRow("申请理由", note) +
		"</table>"
}

func approvalDetailRow(label, value string) string {
	return "<tr>" +
		"<th style=\"text-align:left;border:1px solid #e5e7eb;background:#f9fafb;padding:8px;width:140px;\">" + htmlEscape(label) + "</th>" +
		"<td style=\"border:1px solid #e5e7eb;padding:8px;\">" + htmlEscape(value) + "</td>" +
		"</tr>"
}

func htmlEscape(raw string) string {
	return html.EscapeString(raw)
}
