package generator

import (
	"encoding/json"
	"fmt"

	"github.com/Goldziher/ai-rulez/internal/logger"
)

// shallowMergeJSON merges two JSON objects at the top level.
// Keys from generated overwrite keys in existing. Keys only in existing survive.
// Both inputs must be valid JSON objects (not arrays or primitives).
// If existing is empty or not a valid JSON object, generated is returned as-is.
func shallowMergeJSON(existing, generated []byte) ([]byte, error) {
	if len(existing) == 0 {
		return generated, nil
	}

	var existingMap map[string]json.RawMessage
	if err := json.Unmarshal(existing, &existingMap); err != nil {
		// Existing file is not a valid JSON object (could be malformed, an array, or a primitive).
		// Fall back to overwrite, but warn so the user knows their content was replaced.
		logger.Warn("Existing file is not a JSON object; overwriting instead of merging", "error", err)
		return generated, nil
	}

	var generatedMap map[string]json.RawMessage
	if err := json.Unmarshal(generated, &generatedMap); err != nil {
		return nil, fmt.Errorf("unmarshal generated JSON: %w", err)
	}

	for key, value := range generatedMap {
		existingMap[key] = value
	}

	// encoding/json sorts map keys alphabetically, so output is deterministic
	result, err := json.MarshalIndent(existingMap, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal merged JSON: %w", err)
	}

	result = append(result, '\n')
	return result, nil
}
