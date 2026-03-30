package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadHooksConfig_parses_yaml(t *testing.T) {
	dir := t.TempDir()
	hooksContent := `pre_tool_use:
  - matcher: "Write|Edit"
    command: "ccf require-contract"
post_tool_use:
  - matcher: "TaskCreate|TaskUpdate"
    command: "ccf task-sync"
stop:
  - command: "ccf verify-stop"
    timeout: 300
session_start:
  - command: "ccf session-start"
pre_compact:
  - command: "ccf pre-compact"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, hooksYAMLFilename), []byte(hooksContent), 0o644))

	hooks, err := LoadHooksConfig(dir)
	require.NoError(t, err)
	require.NotNil(t, hooks)

	assert.Len(t, hooks.PreToolUse, 1)
	assert.Equal(t, "Write|Edit", hooks.PreToolUse[0].Matcher)
	assert.Equal(t, "ccf require-contract", hooks.PreToolUse[0].Command)

	assert.Len(t, hooks.PostToolUse, 1)
	assert.Equal(t, "TaskCreate|TaskUpdate", hooks.PostToolUse[0].Matcher)

	assert.Len(t, hooks.Stop, 1)
	assert.Equal(t, 300, hooks.Stop[0].Timeout)

	assert.Len(t, hooks.SessionStart, 1)
	assert.Len(t, hooks.PreCompact, 1)
}

func TestLoadHooksConfig_returns_nil_when_missing(t *testing.T) {
	dir := t.TempDir()
	hooks, err := LoadHooksConfig(dir)
	require.NoError(t, err)
	assert.Nil(t, hooks)
}

func TestLoadHooksConfig_returns_error_on_invalid_yaml(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, hooksYAMLFilename), []byte("not: [valid: yaml"), 0o644))

	_, err := LoadHooksConfig(dir)
	require.Error(t, err)
}

func TestHooksConfigV3_IsEmpty(t *testing.T) {
	assert.True(t, (*HooksConfigV3)(nil).IsEmpty())
	assert.True(t, (&HooksConfigV3{}).IsEmpty())
	assert.False(t, (&HooksConfigV3{
		Stop: []HookEntry{{Command: "echo"}},
	}).IsEmpty())
}

func TestToClaudeSettingsHooks_format(t *testing.T) {
	hooks := &HooksConfigV3{
		PreToolUse: []HookEntry{
			{Matcher: "Write|Edit", Command: "ccf require-contract"},
		},
		Stop: []HookEntry{
			{Command: "ccf verify-stop", Timeout: 300},
		},
	}

	result := hooks.ToClaudeSettingsHooks()
	require.NotNil(t, result)

	// Verify PreToolUse
	preToolUse, ok := result["PreToolUse"].([]interface{})
	require.True(t, ok)
	require.Len(t, preToolUse, 1)

	group := preToolUse[0].(map[string]interface{})
	assert.Equal(t, "Write|Edit", group["matcher"])
	hooksList := group["hooks"].([]interface{})
	hook := hooksList[0].(map[string]interface{})
	assert.Equal(t, "command", hook["type"])
	assert.Equal(t, "ccf require-contract", hook["command"])

	// Verify Stop
	stop, ok := result["Stop"].([]interface{})
	require.True(t, ok)
	require.Len(t, stop, 1)

	stopGroup := stop[0].(map[string]interface{})
	assert.Nil(t, stopGroup["matcher"]) // No matcher for stop hooks
	stopHooks := stopGroup["hooks"].([]interface{})
	stopHook := stopHooks[0].(map[string]interface{})
	assert.Equal(t, 300, stopHook["timeout"])
}

func TestToClaudeSettingsHooks_empty(t *testing.T) {
	assert.Nil(t, (&HooksConfigV3{}).ToClaudeSettingsHooks())
}
