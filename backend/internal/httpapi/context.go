package httpapi

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"

	"sub2api-giftcode/backend/internal/app"
)

const sessionCookieName = "giftcode_session"

type contextKey string

const sessionUserKey contextKey = "session_user"

func signSessionID(sessionID, secret string) (string, error) {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(secret) == "" {
		return "", errors.New("missing session signing inputs")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(sessionID))
	sig := hex.EncodeToString(mac.Sum(nil))
	payload := sessionID + "." + sig
	return base64.RawURLEncoding.EncodeToString([]byte(payload)), nil
}

func sessionTokenFor(secret, sessionID string) (string, error) {
	return signSessionID(sessionID, secret)
}

func verifySessionCookie(rawCookie, secret string) (string, error) {
	if strings.TrimSpace(rawCookie) == "" {
		return "", errors.New("empty cookie")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(rawCookie)
	if err != nil {
		return "", err
	}
	parts := strings.Split(string(decoded), ".")
	if len(parts) != 2 {
		return "", errors.New("invalid cookie")
	}
	expected, err := signSessionID(parts[0], secret)
	if err != nil {
		return "", err
	}
	if subtleConstantTimeEqual(expected, rawCookie) != 1 {
		return "", errors.New("invalid signature")
	}
	return parts[0], nil
}

func extractSessionToken(c *gin.Context) string {
	if c == nil {
		return ""
	}
	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	if authHeader != "" {
		if len(authHeader) >= 7 && strings.EqualFold(authHeader[:6], "Bearer") {
			return strings.TrimSpace(authHeader[6:])
		}
		return authHeader
	}
	rawCookie, err := c.Request.Cookie(sessionCookieName)
	if err == nil && rawCookie != nil {
		return strings.TrimSpace(rawCookie.Value)
	}
	return ""
}

func subtleConstantTimeEqual(a, b string) int {
	if len(a) != len(b) {
		return 0
	}
	if hmac.Equal([]byte(a), []byte(b)) {
		return 1
	}
	return 0
}

func setSessionCookie(c *gin.Context, secret, sessionID string, secure bool) error {
	value, err := sessionTokenFor(secret, sessionID)
	if err != nil {
		return err
	}
	c.SetCookie(sessionCookieName, value, 60*60*24*30, "/", "", secure, true)
	return nil
}

func clearSessionCookie(c *gin.Context, secure bool) {
	c.SetCookie(sessionCookieName, "", -1, "/", "", secure, true)
}

func getSessionUser(c *gin.Context) (*app.SessionUser, bool) {
	raw, ok := c.Get(string(sessionUserKey))
	if !ok {
		return nil, false
	}
	su, ok := raw.(*app.SessionUser)
	return su, ok && su != nil
}

func withSessionUser(c *gin.Context, su *app.SessionUser) {
	c.Set(string(sessionUserKey), su)
}

func isSecureBaseURL(baseURL string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(baseURL)), "https://")
}

func randomSessionSecret() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func writeHTML(c *gin.Context, status int, html string) {
	if c != nil {
		c.Header("Content-Type", "text/html; charset=utf-8")
	}
	c.Data(status, "text/html; charset=utf-8", []byte(html))
}
