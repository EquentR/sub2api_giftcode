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
	support := auxAccountModelSupportState(account, model)
	if support == supportUnknown {
		return availabilityUnknown
	}
	if support == supportUnsupported {
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
	for _, key := range auxAccountModelRateLimitKeys(account, model) {
		rawEntry, hasEntry := limits[key]
		if !hasEntry {
			continue
		}
		resetAt, known := parseModelRateLimitResetAtWithKnown(rawEntry)
		if !known {
			return availabilityUnknown
		}
		if resetAt != nil && now.Before(*resetAt) {
			return availabilityUnavailable
		}
	}
	return availabilityUsable
}

type auxModelSupportState int

const (
	supportUnknown auxModelSupportState = iota
	supportSupported
	supportUnsupported
)

func auxAccountModelSupportState(account sub2api.Account, model string) auxModelSupportState {
	credentials := account.Credentials
	metadataPresent := false
	metadataValid := false
	if raw, ok := credentials["upstream_supported_models"]; ok {
		metadataPresent = true
		if supported, ok := raw.([]any); ok {
			metadataValid = true
			for _, item := range supported {
				if item == model {
					return supportSupported
				}
			}
		}
	}
	if mapping, ok := credentials["model_mapping"]; ok {
		metadataPresent = true
		if parsed, ok := mapping.(map[string]any); ok {
			metadataValid = true
			if _, ok := parsed[model]; ok {
				return supportSupported
			}
		}
	}
	if !metadataPresent || !metadataValid {
		return supportUnknown
	}
	return supportUnsupported
}

func auxAccountModelRateLimitKeys(account sub2api.Account, model string) []string {
	keys := []string{model}
	if mapping, ok := account.Credentials["model_mapping"].(map[string]any); ok {
		raw, ok := mapping[model]
		if !ok {
			return keys
		}
		switch value := raw.(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				keys = append(keys, value)
			}
		case []any:
			for _, item := range value {
				if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
					keys = append(keys, text)
				}
			}
		}
	}
	return keys
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

func auxResetLaneCoverageEvidence(evidence map[string]any, models []string, lanes [][]int64, prefix int, observations map[int64]map[string]auxModelAvailability) {
	for _, model := range models {
		key := model + "_consecutive_unavailable"
		if _, ok := evidence[key]; !ok {
			continue
		}
		if auxLaneHasModelAvailability(lanes, prefix, observations, model, availabilityUnavailable) {
			continue
		}
		delete(evidence, key)
	}
}
