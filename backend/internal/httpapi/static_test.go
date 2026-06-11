package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/config"
)

func TestRouterServesFrontendIndexForSPARoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	staticDir := t.TempDir()
	writeStaticFile(t, staticDir, "index.html", `<div id="app">sub2api giftcode</div>`)
	writeStaticFile(t, staticDir, "assets/app.js", `console.log("ok")`)

	cfg := &config.RuntimeConfig{Config: config.Config{}}
	cfg.App.StaticDir = staticDir

	router := NewRouter(cfg, nil)

	for _, path := range []string{"/", "/admin/redeem-access-requests/confirm"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		recorder := httptest.NewRecorder()

		router.ServeHTTP(recorder, req)

		require.Equal(t, http.StatusOK, recorder.Code)
		require.Contains(t, recorder.Body.String(), "sub2api giftcode")
		require.Contains(t, recorder.Header().Get("Content-Type"), "text/html")
	}
}

func TestRouterServesStaticAssets(t *testing.T) {
	gin.SetMode(gin.TestMode)

	staticDir := t.TempDir()
	writeStaticFile(t, staticDir, "index.html", `<div id="app"></div>`)
	writeStaticFile(t, staticDir, "assets/app.js", `console.log("asset")`)

	cfg := &config.RuntimeConfig{Config: config.Config{}}
	cfg.App.StaticDir = staticDir

	router := NewRouter(cfg, nil)
	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, `console.log("asset")`, recorder.Body.String())
}

func TestRouterKeepsAPINotFoundAsJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	staticDir := t.TempDir()
	writeStaticFile(t, staticDir, "index.html", `<div id="app"></div>`)

	cfg := &config.RuntimeConfig{Config: config.Config{}}
	cfg.App.StaticDir = staticDir

	router := NewRouter(cfg, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/missing", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
	require.JSONEq(t, `{"code":404,"message":"not found"}`, recorder.Body.String())
}

func TestServeStaticFileDoesNotServeFilesOutsideStaticDir(t *testing.T) {
	gin.SetMode(gin.TestMode)

	root := t.TempDir()
	staticDir := filepath.Join(root, "public")
	require.NoError(t, os.MkdirAll(staticDir, 0o755))
	writeStaticFile(t, staticDir, "index.html", `<div id="app"></div>`)
	require.NoError(t, os.WriteFile(filepath.Join(root, "secret.txt"), []byte("secret"), 0o644))

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = &http.Request{
		Method: http.MethodGet,
		URL:    &url.URL{Path: "/../secret.txt"},
	}

	served := serveStaticFile(c, staticDir)

	require.False(t, served)
	require.Empty(t, recorder.Body.String())
}

func writeStaticFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
