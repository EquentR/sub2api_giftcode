package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/config"
	"sub2api-giftcode/backend/internal/db"
	"sub2api-giftcode/backend/internal/sub2api"
)

type auxSchedulableCall struct {
	AccountID int64
	Value     bool
	Verified  bool
	Order     int
}

type auxLaneUpstreamState struct {
	mu        sync.Mutex
	accounts  map[int64]sub2api.Account
	calls     []auxSchedulableCall
	nextOrder int

	postMode        string
	readMode        string
	readMismatch    bool
	readFailAccount int64
}

func newAuxLaneUpstreamState(accounts ...sub2api.Account) *auxLaneUpstreamState {
	state := &auxLaneUpstreamState{accounts: make(map[int64]sub2api.Account)}
	for _, account := range accounts {
		state.accounts[account.ID] = account
	}
	return state
}

func (s *auxLaneUpstreamState) setSchedulable(id int64, value bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	account := s.accounts[id]
	account.Schedulable = value
	s.accounts[id] = account
}

func (s *auxLaneUpstreamState) serve(t *testing.T, accountsPath string) http.HandlerFunc {
	t.Helper()
	pendingReadbacks := make(map[int64]bool)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == accountsPath:
			items := make([]sub2api.Account, 0, len(s.accounts))
			for _, account := range s.accounts {
				items = append(items, account)
			}
			writeAuxTestEnvelope(w, map[string]any{
				"items": items, "total": len(items), "page": 1, "page_size": 200, "pages": 1,
			})
		case r.Method == http.MethodGet:
			id, ok := accountIDFromPath(r.URL.Path)
			if !ok {
				http.NotFound(w, r)
				return
			}
			account, ok := s.accounts[id]
			if !ok {
				http.NotFound(w, r)
				return
			}
			if s.readFailAccount == id {
				http.Error(w, "account read failed", http.StatusBadGateway)
				return
			}
			readback := pendingReadbacks[id]
			if readback {
				delete(pendingReadbacks, id)
			}
			if readback && s.readMode == "missing" {
				writeAuxTestEnvelope(w, nil)
				return
			}
			if readback && s.readMode == "wrong_id" {
				account.ID = id + 1000
				writeAuxTestEnvelope(w, account)
				return
			}
			if readback && (s.readMode == "mismatch" || s.readMismatch) {
				account.Schedulable = !account.Schedulable
				writeAuxTestEnvelope(w, account)
				return
			}
			if readback {
				for i := len(s.calls) - 1; i >= 0; i-- {
					if s.calls[i].AccountID == id && !s.calls[i].Verified {
						s.calls[i].Verified = true
						break
					}
				}
			}
			writeAuxTestEnvelope(w, account)
		case r.Method == http.MethodPost:
			id, ok := accountIDFromPath(r.URL.Path)
			if !ok {
				http.NotFound(w, r)
				return
			}
			var body struct {
				Schedulable bool `json:"schedulable"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			s.nextOrder++
			s.calls = append(s.calls, auxSchedulableCall{AccountID: id, Value: body.Schedulable, Order: s.nextOrder})
			if s.postMode == "timeout" {
				time.Sleep(500 * time.Millisecond)
				return
			}
			account, ok := s.accounts[id]
			if !ok {
				http.NotFound(w, r)
				return
			}
			account.Schedulable = body.Schedulable
			s.accounts[id] = account
			pendingReadbacks[id] = true
			writeAuxTestEnvelope(w, account)
		default:
			http.NotFound(w, r)
		}
	})
}

func accountIDFromPath(path string) (int64, bool) {
	trimmed := strings.TrimPrefix(path, "/api/v1/admin/accounts/")
	if trimmed == "" {
		return 0, false
	}
	trimmed = strings.TrimSuffix(trimmed, "/schedulable")
	id, err := strconv.ParseInt(trimmed, 10, 64)
	return id, err == nil && id > 0
}

func insertAuxLaneRuleRaw(t *testing.T, store *db.Store, name string, enabled bool, lanes [][]int64, models []string, expected int, maximumAutoLane int) int64 {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	primary := []int64{}
	backup := []int64{}
	if len(lanes) >= 1 {
		primary = lanes[0]
	}
	if len(lanes) >= 2 {
		backup = lanes[1]
	}
	lanesJSON, err := json.Marshal(lanes)
	require.NoError(t, err)
	modelsJSON, err := json.Marshal(models)
	require.NoError(t, err)
	primaryJSON, err := json.Marshal(primary)
	require.NoError(t, err)
	backupJSON, err := json.Marshal(backup)
	require.NoError(t, err)
	result, err := store.DB.ExecContext(context.Background(), `
INSERT INTO aux_scheduler_rules (
  name, enabled, primary_account_ids_json, backup_account_ids_json,
  model_names_json, lanes_json, maximum_auto_lane, migration_status, migration_source_json,
  state, expected_open_through_lane, observed_open_through_lane, verified_open_through_lane,
  target_open_through_lane, transition_status, transition_generation,
  upgrade_evidence_json, missing_models_json,
  created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, '', '', 'idle', ?, 1, 1, ?, 'stable', 0, '{}', '[]', ?, ?)
`, name, boolToInt(enabled), string(primaryJSON), string(backupJSON), string(modelsJSON), string(lanesJSON), maximumAutoLane,
		expected, expected, formatTime(now), formatTime(now))
	require.NoError(t, err)
	id, err := result.LastInsertId()
	require.NoError(t, err)
	return id
}

func TestAuxSchedulerLaneExecutorWritesThenReadsBackAndConverges(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	state := newAuxLaneUpstreamState(
		auxModelAccount(1, "one", "gpt-5"),
		auxModelAccount(2, "two", "o3"),
		auxModelAccount(3, "three", "gpt-5", "o3"),
	)
	state.setSchedulable(1, true)
	state.setSchedulable(2, true)
	state.setSchedulable(3, false)
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	id := insertAuxLaneRuleRaw(t, store, "lane executor", true, [][]int64{{1, 2}, {3}}, []string{"gpt-5", "o3"}, 2, 2)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, "stable", rule.TransitionStatus)
	require.Equal(t, 2, rule.ExpectedOpenThroughLane)
	require.Equal(t, 2, rule.VerifiedOpenThroughLane)
	state.mu.Lock()
	require.Len(t, state.calls, 1)
	require.Equal(t, int64(3), state.calls[0].AccountID)
	require.True(t, state.calls[0].Value)
	require.True(t, state.calls[0].Verified)
	require.True(t, state.accounts[3].Schedulable)
	state.mu.Unlock()
}

func TestAuxSchedulerLaneExecutorClosesHighToLowAndRepairsDrift(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	state := newAuxLaneUpstreamState(
		auxModelAccount(1, "one", "gpt-5"),
		auxModelAccount(2, "two", "o3"),
		auxModelAccount(3, "three", "gpt-5", "o3"),
	)
	state.setSchedulable(1, true)
	state.setSchedulable(2, true)
	state.setSchedulable(3, true)
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	id := insertAuxLaneRuleRaw(t, store, "drift repair", true, [][]int64{{1}, {2}, {3}}, []string{"gpt-5"}, 1, 2)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, "stable", rule.TransitionStatus)
	require.Contains(t, rule.Warnings, "人工漂移")
	state.mu.Lock()
	require.Len(t, state.calls, 2)
	require.Equal(t, int64(3), state.calls[0].AccountID)
	require.False(t, state.calls[0].Value)
	require.True(t, state.calls[0].Verified)
	require.Equal(t, int64(2), state.calls[1].AccountID)
	require.False(t, state.calls[1].Value)
	require.True(t, state.calls[1].Verified)
	require.False(t, state.accounts[2].Schedulable)
	require.False(t, state.accounts[3].Schedulable)
	state.mu.Unlock()
}

func TestAuxSchedulerLaneExecutorReadbackMismatchIsUncertainThenRetries(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	state := newAuxLaneUpstreamState(
		auxModelAccount(1, "one", "gpt-5"),
		auxModelAccount(2, "two", "o3"),
	)
	state.setSchedulable(1, true)
	state.setSchedulable(2, false)
	state.readMismatch = true
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	id := insertAuxLaneRuleRaw(t, store, "mismatch", true, [][]int64{{1}, {2}}, []string{"gpt-5"}, 2, 2)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, "uncertain", rule.TransitionStatus)
	state.mu.Lock()
	require.Len(t, state.calls, 1)
	require.Equal(t, int64(2), state.calls[0].AccountID)
	require.False(t, state.calls[0].Verified)
	state.mu.Unlock()

	state.mu.Lock()
	state.readMismatch = false
	state.mu.Unlock()
	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule = currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, "stable", rule.TransitionStatus)
}

func TestAuxSchedulerLaneExecutorMissingReadbackIsUncertainAndBlocksOtherWrites(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	state := newAuxLaneUpstreamState(
		auxModelAccount(1, "one", "gpt-5"),
		auxModelAccount(2, "two", "o3"),
		auxModelAccount(3, "three", "gpt-5", "o3"),
	)
	state.setSchedulable(1, true)
	state.setSchedulable(2, false)
	state.setSchedulable(3, false)
	state.readMode = "missing"
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	id := insertAuxLaneRuleRaw(t, store, "missing readback", true, [][]int64{{1}, {2}, {3}}, []string{"gpt-5"}, 3, 3)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, "uncertain", rule.TransitionStatus)
	state.mu.Lock()
	require.Len(t, state.calls, 1)
	require.Equal(t, int64(2), state.calls[0].AccountID)
	require.False(t, state.calls[0].Verified)
	state.mu.Unlock()
}

func TestAuxSchedulerLaneExecutorWrongAccountReadbackIsUncertain(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	state := newAuxLaneUpstreamState(auxModelAccount(1, "one", "gpt-5"), auxModelAccount(2, "two", "o3"))
	state.setSchedulable(1, true)
	state.setSchedulable(2, false)
	state.readMode = "wrong_id"
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	id := insertAuxLaneRuleRaw(t, store, "wrong id", true, [][]int64{{1}, {2}}, []string{"gpt-5"}, 2, 2)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, "uncertain", rule.TransitionStatus)
	require.Contains(t, rule.LastError, "账号身份")
}

func TestAuxSchedulerLaneExecutorTimeoutIsUncertain(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	state := newAuxLaneUpstreamState(auxModelAccount(1, "one", "gpt-5"), auxModelAccount(2, "two", "o3"))
	state.setSchedulable(1, true)
	state.setSchedulable(2, false)
	state.postMode = "timeout"
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	client := sub2api.NewClient(upstream.URL, "admin-key")
	client.HTTPClient.Timeout = 100 * time.Millisecond
	svc := New(&config.RuntimeConfig{}, store, client, nil)
	id := insertAuxLaneRuleRaw(t, store, "timeout", true, [][]int64{{1}, {2}}, []string{"gpt-5"}, 2, 2)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, "uncertain", rule.TransitionStatus)
	state.mu.Lock()
	require.Len(t, state.calls, 1)
	require.False(t, state.calls[0].Verified)
	state.mu.Unlock()
}

func TestAuxSchedulerLaneExecutorReadFailureBeforeUnreadLaneBlocksWrites(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	state := newAuxLaneUpstreamState(
		auxModelAccount(1, "one", "gpt-5"),
		auxModelAccount(2, "two", "o3"),
		auxModelAccount(3, "three", "gpt-5", "o3"),
	)
	state.setSchedulable(1, false)
	state.setSchedulable(2, false)
	state.setSchedulable(3, false)
	state.readFailAccount = 3
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	id := insertAuxLaneRuleRaw(t, store, "read failure", true, [][]int64{{1}, {2}, {3}}, []string{"gpt-5"}, 2, 2)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, "failed", rule.TransitionStatus)
	state.mu.Lock()
	require.Empty(t, state.calls)
	state.mu.Unlock()
}

func TestAuxSchedulerLaneExecutorRepairsDriftInUnobservedLane(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	state := newAuxLaneUpstreamState(
		auxModelAccount(1, "one", "gpt-5"),
		auxModelAccount(2, "two", "o3"),
	)
	state.setSchedulable(1, false)
	state.setSchedulable(2, true)
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	id := insertAuxLaneRuleRaw(t, store, "unobserved drift", true, [][]int64{{1}, {2}}, []string{"gpt-5"}, 1, 1)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, "stable", rule.TransitionStatus)
	require.Equal(t, 1, rule.ExpectedOpenThroughLane)
	require.Contains(t, rule.Warnings, "人工漂移")
	state.mu.Lock()
	require.Len(t, state.calls, 2)
	require.Equal(t, int64(1), state.calls[0].AccountID)
	require.True(t, state.calls[0].Value)
	require.True(t, state.calls[0].Verified)
	require.Equal(t, int64(2), state.calls[1].AccountID)
	require.False(t, state.calls[1].Value)
	require.True(t, state.calls[1].Verified)
	require.True(t, state.accounts[1].Schedulable)
	require.False(t, state.accounts[2].Schedulable)
	state.mu.Unlock()
}

func TestAuxSchedulerLaneExecutorRecoversAfterRestartFromPersistedExpectation(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "giftcode.db")
	store, err := db.Open("sqlite", dbPath)
	require.NoError(t, err)
	require.NoError(t, store.Migrate(ctx))
	state := newAuxLaneUpstreamState(auxModelAccount(1, "one", "gpt-5"), auxModelAccount(2, "two", "o3"))
	state.setSchedulable(1, true)
	state.setSchedulable(2, false)
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()

	// Simulate a process interruption after the expectation was persisted but
	// before the account write could be applied or verified.
	now := time.Now().UTC().Truncate(time.Second)
	lanesJSON, err := json.Marshal([][]int64{{1}, {2}})
	require.NoError(t, err)
	modelsJSON, err := json.Marshal([]string{"gpt-5"})
	require.NoError(t, err)
	result, err := store.DB.ExecContext(ctx, `
INSERT INTO aux_scheduler_rules (
  name, enabled, primary_account_ids_json, backup_account_ids_json,
  model_names_json, lanes_json, maximum_auto_lane, migration_status, migration_source_json,
  state, expected_open_through_lane, observed_open_through_lane, verified_open_through_lane,
  target_open_through_lane, transition_status, transition_generation,
  upgrade_evidence_json, missing_models_json, last_error,
  created_at, updated_at
) VALUES ('restart', 1, '[1]', '[2]', ?, ?, 2, '', '', 'idle',
          2, 1, 1, 2, 'uncertain', 3, '{}', '[]', 'partial write', ?, ?)
`, string(modelsJSON), string(lanesJSON), formatTime(now), formatTime(now))
	require.NoError(t, err)
	id, err := result.LastInsertId()
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store2, err := db.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store2.Close() })
	require.NoError(t, store2.Migrate(ctx))
	svc2 := New(&config.RuntimeConfig{}, store2, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	require.NoError(t, svc2.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc2, id)
	require.Equal(t, "stable", rule.TransitionStatus)
	require.Equal(t, 2, rule.ExpectedOpenThroughLane)
	require.Equal(t, 2, rule.VerifiedOpenThroughLane)
	state.mu.Lock()
	require.Len(t, state.calls, 1)
	require.Equal(t, int64(2), state.calls[0].AccountID)
	require.True(t, state.calls[0].Value)
	require.True(t, state.calls[0].Verified)
	require.True(t, state.accounts[2].Schedulable)
	state.mu.Unlock()
}

func TestAuxSchedulerLaneExecutorManualCheckSharesReconcilePath(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	state := newAuxLaneUpstreamState(auxModelAccount(1, "one", "gpt-5"), auxModelAccount(2, "two", "o3"))
	state.setSchedulable(1, true)
	state.setSchedulable(2, false)
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	id := insertAuxLaneRuleRaw(t, store, "manual check", true, [][]int64{{1}, {2}}, []string{"gpt-5"}, 2, 2)

	view, err := svc.CheckAuxSchedulerRule(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "stable", view.TransitionStatus)
	state.mu.Lock()
	require.True(t, state.accounts[2].Schedulable)
	state.mu.Unlock()
}
