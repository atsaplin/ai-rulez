package config

import (
	"os"
	"path/filepath"

	"github.com/samber/oops"
	"gopkg.in/yaml.v3"
)

const hooksYAMLFilename = "hooks.yaml"

// HooksConfigV3 represents the hooks.yaml configuration file.
// Event names use Claude's canonical names as YAML keys.
// Preset generators translate these into their native format.
type HooksConfigV3 struct {
	PreToolUse   []HookEntry `yaml:"pre_tool_use,omitempty" json:"pre_tool_use,omitempty"`
	PostToolUse  []HookEntry `yaml:"post_tool_use,omitempty" json:"post_tool_use,omitempty"`
	Stop         []HookEntry `yaml:"stop,omitempty" json:"stop,omitempty"`
	SessionStart []HookEntry `yaml:"session_start,omitempty" json:"session_start,omitempty"`
	PreCompact   []HookEntry `yaml:"pre_compact,omitempty" json:"pre_compact,omitempty"`
	Notification []HookEntry `yaml:"notification,omitempty" json:"notification,omitempty"`
}

// HookEntry represents a single hook command.
type HookEntry struct {
	Matcher string `yaml:"matcher,omitempty" json:"matcher,omitempty"` // Regex pattern for tool name matching (PreToolUse/PostToolUse only)
	Command string `yaml:"command" json:"command"`                    // Shell command to execute
	Timeout int    `yaml:"timeout,omitempty" json:"timeout,omitempty"` // Timeout in seconds (0 = default)
}

// HookEventGroup pairs an event name with its hook entries.
type HookEventGroup struct {
	Event   string
	Entries []HookEntry
}

// EventGroups returns all configured event groups in a stable order.
// This is the single source of truth for iterating over hook events.
func (h *HooksConfigV3) EventGroups() []HookEventGroup {
	if h == nil {
		return nil
	}
	return []HookEventGroup{
		{"pre_tool_use", h.PreToolUse},
		{"post_tool_use", h.PostToolUse},
		{"stop", h.Stop},
		{"session_start", h.SessionStart},
		{"pre_compact", h.PreCompact},
		{"notification", h.Notification},
	}
}

// IsEmpty returns true if no hooks are configured.
func (h *HooksConfigV3) IsEmpty() bool {
	for _, g := range h.EventGroups() {
		if len(g.Entries) > 0 {
			return false
		}
	}
	return true
}

// Validate checks that all hook entries have non-empty commands.
func (h *HooksConfigV3) Validate() error {
	for _, group := range h.EventGroups() {
		for i, entry := range group.Entries {
			if entry.Command == "" {
				return oops.
					With("event", group.Event).
					With("index", i).
					Errorf("hook entry has empty command")
			}
			if entry.Timeout < 0 {
				return oops.
					With("event", group.Event).
					With("index", i).
					Errorf("hook entry has negative timeout: %d", entry.Timeout)
			}
		}
	}
	return nil
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
