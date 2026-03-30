package generator

import (
	"encoding/json"
	"fmt"
	"sort"
)

// shallowMergeJSON merges two JSON objects at the top level.
// Keys from generated overwrite keys in existing. Keys only in existing survive.
// Both inputs must be valid JSON objects. If existing is empty or invalid,
// generated is returned as-is.
func shallowMergeJSON(existing, generated []byte) ([]byte, error) {
	if len(existing) == 0 {
		return generated, nil
	}

	var existingMap map[string]json.RawMessage
	if err := json.Unmarshal(existing, &existingMap); err != nil {
		// Existing file is not valid JSON; treat as overwrite
		return generated, nil
	}

	var generatedMap map[string]json.RawMessage
	if err := json.Unmarshal(generated, &generatedMap); err != nil {
		return nil, fmt.Errorf("unmarshal generated JSON: %w", err)
	}

	// Merge: generated keys overwrite existing keys
	for key, value := range generatedMap {
		existingMap[key] = value
	}

	// Sort keys for deterministic output
	keys := make([]string, 0, len(existingMap))
	for k := range existingMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build ordered map manually for stable output
	orderedMap := make(map[string]json.RawMessage, len(existingMap))
	for _, k := range keys {
		orderedMap[k] = existingMap[k]
	}

	result, err := json.MarshalIndent(orderedMap, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal merged JSON: %w", err)
	}

	// Append trailing newline for POSIX compliance
	result = append(result, '\n')
	return result, nil
}
