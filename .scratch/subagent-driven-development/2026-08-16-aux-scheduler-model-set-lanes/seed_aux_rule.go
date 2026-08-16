package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"sub2api-giftcode/backend/internal/db"
)

func main() {
	path := os.Args[1]
	store, err := db.Open("sqlite", path)
	if err != nil {
		panic(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		panic(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	lanes := [][]int64{{1}, {2}}
	models := []string{"gpt-5", "o3"}
	lanesJSON, _ := json.Marshal(lanes)
	modelsJSON, _ := json.Marshal(models)
	since := now.Add(-90 * time.Second)
	_, err = store.DB.ExecContext(ctx, `
INSERT INTO aux_scheduler_rules (
  name, enabled, primary_account_ids_json, backup_account_ids_json,
  model_names_json, lanes_json, maximum_auto_lane, migration_status, migration_source_json,
  state, expected_open_through_lane, observed_open_through_lane, verified_open_through_lane,
  target_open_through_lane, transition_status, transition_generation,
  upgrade_evidence_json, missing_models_json,
  recovery_candidate_lane, recovery_candidate_since,
  last_observed_at, last_verified_at, blocked_reason, warnings,
  last_error, created_at, updated_at
) VALUES ('演示泳道规则', 1, '[1]', '[2]', ?, ?, 2, '', '', 'idle',
          2, 2, 2, 2, 'stable', 1, '{}', '[]',
          1, ?, ?, ?, '', '', ?,
          ?, ?)
`, string(modelsJSON), string(lanesJSON), formatTime(since), formatTime(now), formatTime(now),
		formatTime(now), formatTime(now), formatTime(now), formatTime(now))
	if err != nil {
		panic(err)
	}
	fmt.Println("seeded")
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
