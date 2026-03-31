package presets

import (
	"encoding/json"
	"path/filepath"

	"github.com/Goldziher/ai-rulez/internal/config"
	"github.com/samber/oops"
)

// renderHookEntries converts HookEntry slice to the shared hook JSON format.
// All three tools (Claude, Codex, Gemini) use the same structure:
// [{"matcher": "...", "hooks": [{"type": "command", "command": "...", "timeout": N}]}]
func renderHookEntries(entries []config.HookEntry) []interface{} {
	var result []interface{}
	for _, entry := range entries {
		hookObj := map[string]interface{}{
			"type":    "command",
			"command": entry.Command,
		}
		if entry.Timeout > 0 {
			hookObj["timeout"] = entry.Timeout
		}

		group := map[string]interface{}{
			"hooks": []interface{}{hookObj},
		}
		if entry.Matcher != "" {
			group["matcher"] = entry.Matcher
		}

		result = append(result, group)
	}
	return result
}

// generateHooksJSON builds a hooks JSON file from hook event groups and an event name mapping.
// Returns nil outputs if no events match the mapping.
func generateHooksJSON(hooks *config.HooksConfigV3, eventNames map[string]string, outputPath string, merge bool) ([]config.OutputFileV3, error) {
	if hooks.IsEmpty() {
		return nil, nil
	}

	nativeHooks := make(map[string]interface{})
	for _, group := range hooks.EventGroups() {
		if len(group.Entries) == 0 {
			continue
		}
		nativeName, ok := eventNames[group.Event]
		if !ok {
			continue
		}
		nativeHooks[nativeName] = renderHookEntries(group.Entries)
	}

	if len(nativeHooks) == 0 {
		return nil, nil
	}

	wrapper := map[string]interface{}{
		"hooks": nativeHooks,
	}

	data, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		return nil, oops.Wrapf(err, "marshal hooks JSON")
	}
	data = append(data, '\n')

	return []config.OutputFileV3{
		{
			Path:    outputPath,
			Content: string(data),
			Merge:   merge,
		},
	}, nil
}

// generateHooksFile is a convenience wrapper that resolves the output path relative to baseDir.
func generateHooksFile(hooks *config.HooksConfigV3, eventNames map[string]string, baseDir string, relPath string, merge bool) ([]config.OutputFileV3, error) {
	return generateHooksJSON(hooks, eventNames, filepath.Join(baseDir, relPath), merge)
}
