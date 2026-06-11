package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/app"
	"sub2api-giftcode/backend/internal/config"
	"sub2api-giftcode/backend/internal/db"
)

func TestGetSiteBrandingReturnsDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	svc := app.New(&config.RuntimeConfig{}, store, nil, nil)
	handlers := &Handlers{cfg: &config.RuntimeConfig{}, service: svc}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/site-branding", nil)

	handlers.GetSiteBranding(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, 0, envelope.Code)

	var branding struct {
		Title             string `json:"title"`
		Subtitle          string `json:"subtitle"`
		MailSubjectPrefix string `json:"mail_subject_prefix"`
	}
	require.NoError(t, json.Unmarshal(envelope.Data, &branding))
	require.Equal(t, "sub2api", branding.Title)
	require.Equal(t, "兑换码系统", branding.Subtitle)
	require.Equal(t, "", branding.MailSubjectPrefix)
}

func TestUpdateSiteBrandingPersistsValues(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	svc := app.New(&config.RuntimeConfig{}, store, nil, nil)
	handlers := &Handlers{cfg: &config.RuntimeConfig{}, service: svc}

	body := strings.NewReader(`{"title":"Acme Billing","subtitle":"充值和兑换","mail_subject_prefix":"[Acme]"}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/admin/site-branding", body)
	c.Request.Header.Set("Content-Type", "application/json")

	handlers.UpdateSiteBranding(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, 0, envelope.Code)

	var branding struct {
		Title             string `json:"title"`
		Subtitle          string `json:"subtitle"`
		MailSubjectPrefix string `json:"mail_subject_prefix"`
	}
	require.NoError(t, json.Unmarshal(envelope.Data, &branding))
	require.Equal(t, "Acme Billing", branding.Title)
	require.Equal(t, "充值和兑换", branding.Subtitle)
	require.Equal(t, "[Acme]", branding.MailSubjectPrefix)
}
