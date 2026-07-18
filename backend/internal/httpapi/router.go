package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"sub2api-giftcode/backend/internal/app"
	"sub2api-giftcode/backend/internal/config"
)

func NewRouter(cfg *config.RuntimeConfig, service *app.Service) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.Logger())

	handlers := &Handlers{cfg: cfg, service: service}

	api := r.Group("/api")
	{
		api.GET("/site-branding", handlers.GetSiteBranding)
		api.POST("/auth/login", handlers.Login)
		api.POST("/auth/embedded/login", handlers.EmbeddedLogin)
		api.POST("/auth/logout", authRequired(cfg, service), handlers.Logout)
		api.GET("/auth/me", authRequired(cfg, service), handlers.Me)

		api.GET("/admin/redeem-access-requests/confirm", handlers.ShowAccessRequestConfirmation)
		api.POST("/admin/redeem-access-requests/confirm", handlers.ConfirmAccessRequest)
		api.GET("/redeem-access-requests/confirm/preview", handlers.PreviewAccessRequestJSON)
		api.POST("/redeem-access-requests/confirm", handlers.ConfirmAccessRequestJSON)

		user := api.Group("", authRequired(cfg, service))
		{
			user.POST("/redeem-access-requests", handlers.CreateAccessRequest)
			user.GET("/redeem-access-requests", handlers.ListMyAccessRequests)
			user.POST("/redeem-requests", handlers.CreateRedeemRequest)
			user.GET("/redeem-requests", handlers.ListMyRedeemRequests)
			user.GET("/redeem-codes", handlers.ListMyRedeemCodes)
			user.GET("/redeem-tiers", handlers.ListEnabledRedeemTiers)
			user.GET("/redeem-balance-tiers", handlers.ListBalanceTiers)
			user.GET("/subscriptions", handlers.ListSubscriptions)
			user.POST("/subscriptions/:id/reset-quota", handlers.ResetSubscriptionQuota)
		}

		admin := api.Group("/admin", authRequired(cfg, service), adminRequired())
		{
			admin.PUT("/site-branding", handlers.UpdateSiteBranding)
			admin.GET("/stats", handlers.Stats)
			admin.GET("/subscription-concurrency/status", handlers.SubscriptionConcurrencyMonitorStatus)
			admin.GET("/subscription-concurrency/details", handlers.SubscriptionConcurrencyMonitorDetails)
			admin.GET("/subscription-reset-entitlements", handlers.ListSubscriptionResetEntitlements)
			admin.GET("/users", handlers.ListUsers)
			admin.GET("/users/:id/redeem-codes", handlers.ListUserRedeemCodes)
			admin.GET("/redeem-access-requests", handlers.ListAllAccessRequests)
			admin.GET("/redeem-access-requests/:id", handlers.GetAccessRequest)
			admin.POST("/redeem-access-requests/:id/approve", handlers.ApproveAccessRequest)
			admin.POST("/redeem-access-requests/:id/reject", handlers.RejectAccessRequest)
			admin.GET("/redeem-codes", handlers.ListAllRedeemCodes)
			admin.POST("/sync/redeem-codes", handlers.SyncRedeemCodes)
			admin.GET("/openai-accounts", handlers.ListOpenAIAccounts)
			admin.PUT("/openai-accounts/:id/user-agent", handlers.UpdateOpenAIAccountUserAgent)
			admin.GET("/redeem-tiers", handlers.ListRedeemTiers)
			admin.PUT("/redeem-tiers", handlers.UpdateRedeemTiers)
			admin.GET("/sub2api-subscription-groups", handlers.ListSubscriptionGroups)
			admin.GET("/redeem-balance-tiers", handlers.ListBalanceTiers)
			admin.PUT("/redeem-balance-tiers", handlers.UpdateBalanceTiers)
			admin.POST("/compensation-batches", handlers.CreateCompensationBatch)
			admin.GET("/compensation-batches", handlers.ListCompensationBatches)
			admin.GET("/compensation-batches/:id/details", handlers.ListCompensationBatchDetails)
			admin.POST("/subscription-reset-bonus-batches/preview", handlers.PreviewSubscriptionResetBonusBatch)
			admin.POST("/subscription-reset-bonus-batches", handlers.CreateSubscriptionResetBonusBatch)
			admin.GET("/subscription-reset-bonus-batches", handlers.ListSubscriptionResetBonusBatches)
			admin.GET("/subscription-reset-bonus-batches/:id/details", handlers.ListSubscriptionResetBonusBatchDetails)
			admin.GET("/subscription-extension-events", handlers.ListSubscriptionExtensionEvents)
			admin.POST("/subscription-extension-events/:id/resolve", handlers.ResolveSubscriptionExtensionEvent)
			admin.GET("/subscription-reset-attempts", handlers.ListPendingSubscriptionResetAttempts)
			admin.POST("/subscription-reset-attempts/:id/resolve", handlers.ResolveSubscriptionResetAttempt)
			admin.GET("/subscription-reset-backfills", handlers.ListSubscriptionResetBackfillRuns)
		}
	}

	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			writeError(c, http.StatusNotFound, "not found")
			return
		}
		if serveStaticFile(c, cfg.App.StaticDir) {
			return
		}
		c.String(http.StatusNotFound, "not found")
	})

	return r
}

func serveStaticFile(c *gin.Context, staticDir string) bool {
	staticDir = strings.TrimSpace(staticDir)
	if staticDir == "" {
		return false
	}

	requestPath := strings.TrimPrefix(c.Request.URL.Path, "/")
	if requestPath != "" {
		candidate := filepath.Join(staticDir, filepath.FromSlash(requestPath))
		if !isPathInside(staticDir, candidate) {
			return false
		}
		if isRegularFile(candidate) {
			c.File(candidate)
			return true
		}
	}

	indexPath := filepath.Join(staticDir, "index.html")
	if isRegularFile(indexPath) {
		c.File(indexPath)
		return true
	}
	return false
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func isPathInside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func authRequired(cfg *config.RuntimeConfig, service *app.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		rawToken := extractSessionToken(c)
		if rawToken == "" {
			writeError(c, http.StatusUnauthorized, "unauthorized")
			c.Abort()
			return
		}
		sessionID, err := verifySessionCookie(rawToken, cfg.Session.CookieSecret)
		if err != nil {
			writeError(c, http.StatusUnauthorized, "unauthorized")
			c.Abort()
			return
		}
		sessionUser, err := service.CurrentSession(c.Request.Context(), sessionID)
		if err != nil {
			status, msg, reason := statusForError(err)
			if reason != "" {
				writeErrorReason(c, status, msg, reason)
			} else {
				writeError(c, status, msg)
			}
			c.Abort()
			return
		}
		withSessionUser(c, sessionUser)
		c.Next()
	}
}

func adminRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionUser, ok := getSessionUser(c)
		if !ok || sessionUser == nil || !sessionUser.IsAdmin {
			writeError(c, http.StatusForbidden, "forbidden")
			c.Abort()
			return
		}
		c.Next()
	}
}
