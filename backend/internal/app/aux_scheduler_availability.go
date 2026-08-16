package app

import (
	"strings"
	"time"

	"sub2api-giftcode/backend/internal/sub2api"
)

type auxModelAvailability string

const (
	availabilityUsable      auxModelAvailability = "usable"
	availabilityUnavailable auxModelAvailability = "unavailable"
	availabilityUnknown     auxModelAvailability = "unknown"
)

func auxAccountModelAvailability(account sub2api.Account, model string, now time.Time) auxModelAvailability {
	status := strings.ToLower(strings.TrimSpace(account.Status))
	switch status {
	case "":
		return availabilityUnknown
	case "error":
		return availabilityUnavailable
	case "active":
	default:
		return availabilityUnknown
	}
	for _, until := range []*time.Time{account.TempUnschedulableUntil, account.RateLimitResetAt, account.OverloadUntil} {
		if until != nil && now.Before(*until) {
			return availabilityUnavailable
		}
	}
	supported := false
	hasMetadata := false
	credentials := account.Credentials
	if raw, ok := credentials["upstream_supported_models"].([]any); ok {
		hasMetadata = true
		for _, item := range raw {
			if item == model {
				supported = true
				break
			}
		}
	}
	if mapping, ok := credentials["model_mapping"].(map[string]any); ok {
		hasMetadata = true
		if _, ok := mapping[model]; ok {
			supported = true
		}
	}
	if !hasMetadata {
		return availabilityUnknown
	}
	if !supported {
		return availabilityUnavailable
	}
	rawLimits, hasLimits := account.Extra["model_rate_limits"]
	if !hasLimits {
		return availabilityUsable
	}
	limits, ok := rawLimits.(map[string]any)
	if !ok {
		return availabilityUnknown
	}
	rawEntry, hasEntry := limits[model]
	if !hasEntry {
		return availabilityUsable
	}
	resetAt, known := parseModelRateLimitResetAtWithKnown(rawEntry)
	if !known {
		return availabilityUnknown
	}
	if resetAt != nil && now.Before(*resetAt) {
		return availabilityUnavailable
	}
	return availabilityUsable
}

func parseModelRateLimitResetAtWithKnown(raw any) (*time.Time, bool) {
	switch value := raw.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return nil, true
		}
		parsed, err := parseTime(value)
		if err != nil {
			return nil, false
		}
		return parsed, true
	case map[string]any:
		resetAt, ok := value["rate_limit_reset_at"].(string)
		if !ok {
			return nil, false
		}
		if strings.TrimSpace(resetAt) == "" {
			return nil, true
		}
		parsed, err := parseTime(resetAt)
		if err != nil {
			return nil, false
		}
		return parsed, true
	case map[string]string:
		resetAt := value["rate_limit_reset_at"]
		if strings.TrimSpace(resetAt) == "" {
			return nil, true
		}
		parsed, err := parseTime(resetAt)
		if err != nil {
			return nil, false
		}
		return parsed, true
	default:
		return nil, false
	}
}

func auxMissingModelsForPrefix(lanes [][]int64, observations map[int64]map[string]auxModelAvailability, models []string, prefix int) []string {
	missing := make([]string, 0, len(models))
	for _, model := range models {
		covered := false
		for laneIndex := 0; laneIndex < prefix && laneIndex < len(lanes); laneIndex++ {
			for _, id := range lanes[laneIndex] {
				if observations[id][model] == availabilityUsable {
					covered = true
					break
				}
			}
			if covered {
				break
			}
		}
		if !covered {
			missing = append(missing, model)
		}
	}
	return missing
}

func auxAccountWholeUnavailable(account sub2api.Account, now time.Time) bool {
	status := strings.ToLower(strings.TrimSpace(account.Status))
	if status == "error" {
		return true
	}
	for _, until := range []*time.Time{account.TempUnschedulableUntil, account.RateLimitResetAt, account.OverloadUntil} {
		if until != nil && now.Before(*until) {
			return true
		}
	}
	return false
}

func auxLaneHasModelAvailability(lanes [][]int64, prefix int, observations map[int64]map[string]auxModelAvailability, model string, expected auxModelAvailability) bool {
	for laneIndex := 0; laneIndex < prefix && laneIndex < len(lanes); laneIndex++ {
		for _, id := range lanes[laneIndex] {
			if observations[id][model] == expected {
				return true
			}
		}
	}
	return false
}

func auxLaneHasModelSupport(accounts map[int64]sub2api.Account, lanes [][]int64, prefix int, model string) bool {
	for laneIndex := 0; laneIndex < prefix && laneIndex < len(lanes); laneIndex++ {
		for _, id := range lanes[laneIndex] {
			if account, ok := accounts[id]; ok && auxAccountSupportsModel(account, model) {
				return true
			}
		}
	}
	return false
}

func auxLaneHasModelCapability(lanes [][]int64, prefix int, observations map[int64]map[string]auxModelAvailability, model string) bool {
	for laneIndex := 0; laneIndex < prefix && laneIndex < len(lanes); laneIndex++ {
		for _, id := range lanes[laneIndex] {
			switch observations[id][model] {
			case availabilityUsable, availabilityUnavailable:
				return true
			}
		}
	}
	return false
}

func auxAccountSupportsModel(account sub2api.Account, model string) bool {
	for _, supported := range auxAccountSupportedModels(account) {
		if supported == model {
			return true
		}
	}
	return false
}
