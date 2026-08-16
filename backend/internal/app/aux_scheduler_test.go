package app

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/models"
)

func currentAuxSchedulerRule(t *testing.T, svc *Service, id int64) models.AuxSchedulerRule {
	t.Helper()
	views, err := svc.ListAuxSchedulerRules(context.Background())
	require.NoError(t, err)
	for _, view := range views {
		if view.ID == id {
			return view.AuxSchedulerRule
		}
	}
	require.FailNowf(t, "aux scheduler rule not found", "rule %d", id)
	return models.AuxSchedulerRule{}
}

func writeAuxTestEnvelope(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "success", "data": data})
}

func timePtr(value time.Time) *time.Time {
	return &value
}
