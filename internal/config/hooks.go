package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/samber/oops"
	"gopkg.in/yaml.v3"
)

const hooksYAMLFilename = "hooks.yaml"

// HooksConfigV3 represents the hooks.yaml configuration file.
// Event names use Claude's canonical names (PreToolUse, PostToolUse, Stop, etc.).
// Preset generators translate these into their native format.
type HooksConfigV3 struct {
	PreToolUse  []HookEntry `yaml:"pre_tool_use,omitempty" json:"pre_tool_use,omitempty"`
	PostToolUse []HookEntry `yaml:"post_tool_use,omitempty" json:"post_tool_use,omitempty"`
	Stop        []HookEntry `yaml:"stop,omitempty" json:"stop,omitempty"`
	SessionStart []HookEntry `yaml:"session_start,omitempty" json:"session_start,omitempty"`
	PreCompact  []HookEntry `yaml:"pre_compact,omitempty" json:"pre_compact,omitempty"`
	Notification []HookEntry `yaml:"notification,omitempty" json:"notification,omitempty"`
}

// HookEntry represents a single hook command.
type HookEntry struct {
	Matcher string `yaml:"matcher,omitempty" json:"matcher,omitempty"` // Regex pattern for tool name matching (PreToolUse/PostToolUse only)
	Command string `yaml:"command" json:"command"`                    // Shell command to execute
	Timeout int    `yaml:"timeout,omitempty" json:"timeout,omitempty"` // Timeout in seconds (0 = default)
}

// IsEmpty returns true if no hooks are configured.
func (h *HooksConfigV3) IsEmpty() bool {
	if h == nil {
		return true
	}
	return len(h.PreToolUse) == 0 &&
		len(h.PostToolUse) == 0 &&
		len(h.Stop) == 0 &&
		len(h.SessionStart) == 0 &&
		len(h.PreCompact) == 0 &&
		len(h.Notification) == 0
}

// ToClaudeSettingsHooks converts the hooks config to Claude's native settings.json hooks format.
// Returns a JSON-serializable structure matching Claude's hook schema.
func (h *HooksConfigV3) ToClaudeSettingsHooks() map[string]interface{} {
	if h.IsEmpty() {
		return nil
	}

	hooks := make(map[string]interface{})

	if len(h.PreToolUse) > 0 {
		hooks["PreToolUse"] = toClaudeHookEntries(h.PreToolUse)
	}
	if len(h.PostToolUse) > 0 {
		hooks["PostToolUse"] = toClaudeHookEntries(h.PostToolUse)
	}
	if len(h.Stop) > 0 {
		hooks["Stop"] = toClaudeHookEntries(h.Stop)
	}
	if len(h.SessionStart) > 0 {
		hooks["SessionStart"] = toClaudeHookEntries(h.SessionStart)
	}
	if len(h.PreCompact) > 0 {
		hooks["PreCompact"] = toClaudeHookEntries(h.PreCompact)
	}
	if len(h.Notification) > 0 {
		hooks["Notification"] = toClaudeHookEntries(h.Notification)
	}

	return hooks
}

// toClaudeHookEntries converts HookEntry slice to Claude's native format.
// Claude expects: [{"matcher": "...", "hooks": [{"type": "command", "command": "..."}]}]
func toClaudeHookEntries(entries []HookEntry) []interface{} {
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

// LoadHooksConfig loads hooks.yaml from the given config directory.
// Returns nil (not an error) if the file does not exist.
func LoadHooksConfig(configDir string) (*HooksConfigV3, error) {
	hooksPath := filepath.Join(configDir, hooksYAMLFilename)

	data, err := os.ReadFile(hooksPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, oops.
			With("path", hooksPath).
			Wrapf(err, "read hooks config")
	}

	var hooks HooksConfigV3
	if err := yaml.Unmarshal(data, &hooks); err != nil {
		return nil, oops.
			With("path", hooksPath).
			Wrapf(err, "parse hooks YAML")
	}

	return &hooks, nil
}

// MarshalHooksToJSON marshals the hooks config to pretty-printed JSON for embedding in settings.json.
func MarshalHooksToJSON(hooks *HooksConfigV3) ([]byte, error) {
	claudeHooks := hooks.ToClaudeSettingsHooks()
	if claudeHooks == nil {
		return nil, nil
	}

	wrapper := map[string]interface{}{
		"hooks": claudeHooks,
	}

	return json.MarshalIndent(wrapper, "", "  ")
}
