package httpapi

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/app"
	"sub2api-giftcode/backend/internal/config"
	"sub2api-giftcode/backend/internal/db"
	"sub2api-giftcode/backend/internal/mail"
	"sub2api-giftcode/backend/internal/sub2api"
)

func TestCreateAccessRequestReturnsTierID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := startTestSub2API(t)
	smtpAddr := startTestSMTPServer(t)

	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	cfg := &config.RuntimeConfig{
		Config: config.Config{},
	}
	cfg.App.FrontendURL = "https://front.example.com"
	cfg.Mail.SMTPHost = smtpAddr.host
	cfg.Mail.SMTPPort = smtpAddr.port
	cfg.Mail.FromAddress = "noreply@example.com"
	cfg.Mail.AdminToAddress = "admin@example.com"
	cfg.Session.CookieSecret = "secret"

	client := sub2api.NewClient(upstream.URL, "admin-key")
	me, err := client.Me(context.Background(), "access-1")
	require.NoError(t, err)
	require.Equal(t, int64(1), me.ID)

	svc := app.New(cfg, store, client, mail.New(mail.Config{
		SMTPHost:       smtpAddr.host,
		SMTPPort:       smtpAddr.port,
		FromAddress:    "noreply@example.com",
		AdminToAddress: "admin@example.com",
	}))

	reqBody := strings.NewReader(`{"tier_id":1,"note":"please approve","fulfillment_mode":"redeem_code"}`)
	sessionUser, err := svc.LoginWithAccessToken(context.Background(), "access-1", nil)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/redeem-access-requests", reqBody)
	req.Header.Set("Content-Type", "application/json")
	token, err := sessionTokenFor(cfg.Session.CookieSecret, sessionUser.Session.ID)
	require.NoError(t, err)
	_, err = verifySessionCookie(token, cfg.Session.CookieSecret)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	_, err = svc.CurrentSession(context.Background(), sessionUser.Session.ID)
	require.NoError(t, err)

	handlers := &Handlers{cfg: cfg, service: svc}
	r := gin.New()
	r.POST("/api/redeem-access-requests", authRequired(cfg, svc), handlers.CreateAccessRequest)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)

	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.NotNil(t, envelope.Data)

	var created struct {
		ID                int64   `json:"id"`
		TierID            int64   `json:"tier_id"`
		Amount            float64 `json:"amount"`
		PayAmountCny      float64 `json:"pay_amount_cny"`
		Note              string  `json:"note"`
		FulfillmentMode   string  `json:"fulfillment_mode"`
		FulfillmentResult string  `json:"fulfillment_result"`
		FulfilledVia      string  `json:"fulfilled_via"`
	}
	require.NoError(t, json.Unmarshal(envelope.Data, &created))
	require.Equal(t, int64(1), created.TierID)
	require.Equal(t, 120.0, created.Amount)
	require.Equal(t, 120.0, created.PayAmountCny)
	require.Equal(t, "please approve", created.Note)
	require.Equal(t, "redeem_code", created.FulfillmentMode)
	require.Equal(t, "", created.FulfillmentResult)
	require.Equal(t, "", created.FulfilledVia)
}

func TestApproveAccessRequestReturnsIssuedCode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := startTestSub2API(t)
	smtpAddr := startTestSMTPServer(t)

	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	cfg := &config.RuntimeConfig{Config: config.Config{}}
	cfg.App.FrontendURL = "https://front.example.com"
	cfg.Mail.SMTPHost = smtpAddr.host
	cfg.Mail.SMTPPort = smtpAddr.port
	cfg.Mail.FromAddress = "noreply@example.com"
	cfg.Mail.AdminToAddress = "admin@example.com"
	cfg.Session.CookieSecret = "secret"

	client := sub2api.NewClient(upstream.URL, "admin-key")
	svc := app.New(cfg, store, client, mail.New(mail.Config{
		SMTPHost:       smtpAddr.host,
		SMTPPort:       smtpAddr.port,
		FromAddress:    "noreply@example.com",
		AdminToAddress: "admin@example.com",
	}))

	userSession, err := svc.LoginWithAccessToken(context.Background(), "access-1", nil)
	require.NoError(t, err)

	req, err := svc.CreateAccessRequest(context.Background(), userSession.Session.ID, 1, "please approve", "redeem_code")
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/redeem-access-requests/1/approve", nil)
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", req.ID)}}

	handlers := &Handlers{cfg: cfg, service: svc}
	handlers.ApproveAccessRequest(c)

	require.Equal(t, http.StatusOK, recorder.Code)

	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))

	var resp struct {
		Request struct {
			ID                int64   `json:"id"`
			Status            string  `json:"status"`
			TierID            int64   `json:"tier_id"`
			Amount            float64 `json:"amount"`
			PayAmountCny      float64 `json:"pay_amount_cny"`
			UpstreamCode      string  `json:"upstream_code"`
			FulfillmentMode   string  `json:"fulfillment_mode"`
			FulfillmentResult string  `json:"fulfillment_result"`
			FulfilledVia      string  `json:"fulfilled_via"`
		} `json:"request"`
		Code struct {
			ID      int64   `json:"id"`
			Code    string  `json:"code"`
			Value   float64 `json:"value"`
			Status  string  `json:"status"`
			Request int64   `json:"request_id"`
		} `json:"code"`
	}
	require.NoError(t, json.Unmarshal(envelope.Data, &resp))
	require.Equal(t, int64(1), resp.Request.TierID)
	require.Equal(t, 120.0, resp.Request.Amount)
	require.Equal(t, 120.0, resp.Request.PayAmountCny)
	require.Equal(t, "consumed", resp.Request.Status)
	require.Equal(t, "redeem_code", resp.Request.FulfillmentMode)
	require.Equal(t, "redeem_code_issued", resp.Request.FulfillmentResult)
	require.Equal(t, "redeem_code", resp.Request.FulfilledVia)
	require.Equal(t, "code-99", resp.Code.Code)
	require.Equal(t, 120.0, resp.Code.Value)
	require.Equal(t, "unused", resp.Code.Status)
}

func TestEmailApprovalLinkRequiresExplicitConfirmation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/redeem-codes/generate":
			upstreamCalls++
			writeTestEnvelope(w, []sub2api.RedeemCode{{
				ID:        99,
				Code:      "code-99",
				Type:      "balance",
				Value:     120,
				Status:    "unused",
				CreatedAt: time.Now().UTC(),
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	now := time.Now().UTC().Truncate(time.Second)
	token := "approval-token-1"
	_, err = store.DB.ExecContext(context.Background(), `
INSERT INTO redeem_access_requests (
  requestor_upstream_user_id, requestor_email, requestor_username, tier_id, amount, pay_amount_cny, note, status,
  approval_token_hash, approval_token_expires_at, notification_status, notification_error,
  created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, 1, "alice@example.com", "alice", 1, 120.0, 95.0, "team reimbursement", "pending", hashApprovalToken(token), now.Add(time.Hour).Format(time.RFC3339Nano), "sent", "", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	require.NoError(t, err)

	cfg := &config.RuntimeConfig{Config: config.Config{}}
	cfg.App.FrontendURL = "https://front.example.com"
	svc := app.New(cfg, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	handlers := &Handlers{cfg: cfg, service: svc}
	r := gin.New()
	r.GET("/api/admin/redeem-access-requests/confirm", handlers.ShowAccessRequestConfirmation)
	r.POST("/api/admin/redeem-access-requests/confirm", handlers.ConfirmAccessRequest)

	getReq := httptest.NewRequest(http.MethodGet, "/api/admin/redeem-access-requests/confirm?token="+token, nil)
	getRecorder := httptest.NewRecorder()
	r.ServeHTTP(getRecorder, getReq)

	require.Equal(t, http.StatusOK, getRecorder.Code)
	require.Contains(t, getRecorder.Body.String(), "alice@example.com")
	require.Contains(t, getRecorder.Body.String(), "team reimbursement")
	require.Contains(t, getRecorder.Body.String(), "120")
	require.Contains(t, getRecorder.Body.String(), "95")
	require.Contains(t, getRecorder.Body.String(), "确认处理申请")
	require.Equal(t, 0, upstreamCalls)

	var status string
	require.NoError(t, store.DB.QueryRowContext(context.Background(), `SELECT status FROM redeem_access_requests WHERE id = 1`).Scan(&status))
	require.Equal(t, "pending", status)

	postReq := httptest.NewRequest(http.MethodPost, "/api/admin/redeem-access-requests/confirm", strings.NewReader("token="+token))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRecorder := httptest.NewRecorder()
	r.ServeHTTP(postRecorder, postReq)

	require.Equal(t, http.StatusOK, postRecorder.Code)
	require.Contains(t, postRecorder.Body.String(), "申请已处理")
	require.Equal(t, 1, upstreamCalls)
	require.NoError(t, store.DB.QueryRowContext(context.Background(), `SELECT status FROM redeem_access_requests WHERE id = 1`).Scan(&status))
	require.Equal(t, "consumed", status)
}

func TestEmailApprovalLinkShowsSubscriptionDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	now := time.Now().UTC().Truncate(time.Second)
	token := "approval-token-subscription"
	_, err = store.DB.ExecContext(context.Background(), `
INSERT INTO redeem_access_requests (
  requestor_upstream_user_id, requestor_email, requestor_username, tier_id, code_type,
  tier_label, amount, pay_amount_cny, sub2api_group_id, sub2api_group_name, sub2api_group_platform,
  sub2api_daily_limit_usd, sub2api_weekly_limit_usd, sub2api_monthly_limit_usd, validity_days,
  note, status, approval_token_hash, approval_token_expires_at, notification_status, notification_error,
  created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, 1, "alice@example.com", "alice", 3, "subscription", "Claude 30 days", 0.0, 88.0, 2, "Claude monthly", "anthropic", 0.0, 50.0, 120.0, 30, "need sub", "pending", hashApprovalToken(token), now.Add(time.Hour).Format(time.RFC3339Nano), "sent", "", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	require.NoError(t, err)

	cfg := &config.RuntimeConfig{Config: config.Config{}}
	cfg.App.FrontendURL = "https://front.example.com"
	svc := app.New(cfg, store, nil, nil)
	handlers := &Handlers{cfg: cfg, service: svc}
	r := gin.New()
	r.GET("/api/admin/redeem-access-requests/confirm", handlers.ShowAccessRequestConfirmation)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/redeem-access-requests/confirm?token="+token, nil)
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	page := recorder.Body.String()
	require.Contains(t, page, "订阅")
	require.Contains(t, page, "Claude 30 days")
	require.Contains(t, page, "Claude monthly")
	require.Contains(t, page, "anthropic")
	require.Contains(t, page, "30 天")
	require.Contains(t, page, "日限")
	require.Contains(t, page, "无限制")
	require.Contains(t, page, "周限")
	require.Contains(t, page, "50 USD")
	require.Contains(t, page, "月限")
	require.Contains(t, page, "120 USD")
	require.Contains(t, page, "88")
}

func TestFrontendApprovalConfirmEndpointReturnsJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/redeem-codes/generate":
			upstreamCalls++
			writeTestEnvelope(w, []sub2api.RedeemCode{{
				ID:        99,
				Code:      "code-99",
				Type:      "balance",
				Value:     120,
				Status:    "unused",
				CreatedAt: time.Now().UTC(),
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	now := time.Now().UTC().Truncate(time.Second)
	token := "approval-token-frontend"
	_, err = store.DB.ExecContext(context.Background(), `
INSERT INTO redeem_access_requests (
  requestor_upstream_user_id, requestor_email, requestor_username, tier_id, amount, pay_amount_cny, note, status,
  approval_token_hash, approval_token_expires_at, notification_status, notification_error,
  created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, 1, "alice@example.com", "alice", 1, 120.0, 95.0, "team reimbursement", "pending", hashApprovalToken(token), now.Add(time.Hour).Format(time.RFC3339Nano), "sent", "", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	require.NoError(t, err)

	cfg := &config.RuntimeConfig{Config: config.Config{}}
	svc := app.New(cfg, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	handlers := &Handlers{cfg: cfg, service: svc}
	r := gin.New()
	r.POST("/api/redeem-access-requests/confirm", handlers.ConfirmAccessRequestJSON)

	body := strings.NewReader(`{"token":"` + token + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/redeem-access-requests/confirm", body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
	require.Equal(t, 1, upstreamCalls)

	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, 0, envelope.Code)
	require.Equal(t, "success", envelope.Message)

	var confirmed struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(envelope.Data, &confirmed))
	require.Equal(t, int64(1), confirmed.ID)
	require.Equal(t, "consumed", confirmed.Status)
}

func TestFrontendApprovalPreviewEndpointDoesNotApprove(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/admin/redeem-codes/generate" {
			upstreamCalls++
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(upstream.Close)

	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	now := time.Now().UTC().Truncate(time.Second)
	token := "approval-token-preview"
	_, err = store.DB.ExecContext(context.Background(), `
INSERT INTO redeem_access_requests (
  requestor_upstream_user_id, requestor_email, requestor_username, tier_id, amount, pay_amount_cny, note, status,
  approval_token_hash, approval_token_expires_at, notification_status, notification_error,
  created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, 1, "alice@example.com", "alice", 1, 120.0, 95.0, "team reimbursement", "pending", hashApprovalToken(token), now.Add(time.Hour).Format(time.RFC3339Nano), "sent", "", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	require.NoError(t, err)

	cfg := &config.RuntimeConfig{Config: config.Config{}}
	svc := app.New(cfg, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	handlers := &Handlers{cfg: cfg, service: svc}
	r := gin.New()
	r.GET("/api/redeem-access-requests/confirm/preview", handlers.PreviewAccessRequestJSON)

	req := httptest.NewRequest(http.MethodGet, "/api/redeem-access-requests/confirm/preview?token="+token, nil)
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
	require.Equal(t, 0, upstreamCalls)

	var status string
	require.NoError(t, store.DB.QueryRowContext(context.Background(), `SELECT status FROM redeem_access_requests WHERE id = 1`).Scan(&status))
	require.Equal(t, "pending", status)

	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, 0, envelope.Code)

	var preview struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	require.NoError(t, json.Unmarshal(envelope.Data, &preview))
	require.Equal(t, int64(1), preview.ID)
	require.Equal(t, "pending", preview.Status)
	require.Equal(t, "team reimbursement", preview.Note)
}

func hashApprovalToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

type smtpServerAddr struct {
	host string
	port int
}

func startTestSMTPServer(t *testing.T) smtpServerAddr {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleSMTPConn(conn)
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	return smtpServerAddr{host: "127.0.0.1", port: addr.Port}
}

func handleSMTPConn(conn net.Conn) {
	defer conn.Close()
	_, _ = fmt.Fprint(conn, "220 localhost ESMTP\r\n")

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	inData := false

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.TrimSpace(line)
		upper := strings.ToUpper(cmd)

		switch {
		case inData:
			if cmd == "." {
				_, _ = fmt.Fprint(writer, "250 OK\r\n")
				_ = writer.Flush()
				inData = false
				continue
			}
			continue
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			_, _ = fmt.Fprint(writer, "250-localhost\r\n250 OK\r\n")
		case strings.HasPrefix(upper, "MAIL FROM"):
			_, _ = fmt.Fprint(writer, "250 OK\r\n")
		case strings.HasPrefix(upper, "RCPT TO"):
			_, _ = fmt.Fprint(writer, "250 OK\r\n")
		case strings.HasPrefix(upper, "DATA"):
			_, _ = fmt.Fprint(writer, "354 End data with <CR><LF>.<CR><LF>\r\n")
			inData = true
		case strings.HasPrefix(upper, "QUIT"):
			_, _ = fmt.Fprint(writer, "221 Bye\r\n")
			_ = writer.Flush()
			return
		default:
			_, _ = fmt.Fprint(writer, "250 OK\r\n")
		}
		_ = writer.Flush()
	}
}

func startTestSub2API(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/me":
			require.Equal(t, "Bearer access-1", r.Header.Get("Authorization"))
			writeTestEnvelope(w, sub2api.User{
				ID:        1,
				Email:     "alice@example.com",
				Username:  "alice",
				Role:      "user",
				Status:    "active",
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
			})
		case "/api/v1/admin/redeem-codes/generate":
			require.Equal(t, "admin-key", r.Header.Get("x-api-key"))
			require.NotEmpty(t, r.Header.Get("Idempotency-Key"))
			var payload map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			require.Equal(t, "balance", payload["type"])
			writeTestEnvelope(w, []sub2api.RedeemCode{{
				ID:        99,
				Code:      "code-99",
				Type:      "balance",
				Value:     payload["value"].(float64),
				Status:    "unused",
				CreatedAt: time.Now().UTC(),
			}})
		default:
			http.NotFound(w, r)
		}
	}))
}

func writeTestEnvelope(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sub2apiEnvelope{Code: 0, Message: "success", Data: mustJSON(data)})
}

type sub2apiEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Reason  string          `json:"reason,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func mustJSON(data any) json.RawMessage {
	b, err := json.Marshal(data)
	if err != nil {
		panic(err)
	}
	return b
}

func mustJSONRaw(t *testing.T, raw any) []byte {
	t.Helper()
	b, err := json.Marshal(raw)
	require.NoError(t, err)
	return b
}
