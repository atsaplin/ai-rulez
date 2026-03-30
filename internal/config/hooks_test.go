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
notification:
  - command: "ccf notify"
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
	assert.Len(t, hooks.Notification, 1)
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

func TestHooksConfigV3_Validate_empty_command(t *testing.T) {
	hooks := &HooksConfigV3{
		Stop: []HookEntry{{Command: ""}},
	}
	err := hooks.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty command")
}

func TestHooksConfigV3_Validate_negative_timeout(t *testing.T) {
	hooks := &HooksConfigV3{
		Stop: []HookEntry{{Command: "echo", Timeout: -1}},
	}
	err := hooks.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "negative timeout")
}

func TestHooksConfigV3_Validate_valid(t *testing.T) {
	hooks := &HooksConfigV3{
		PreToolUse: []HookEntry{{Matcher: "Write", Command: "ccf check", Timeout: 30}},
		Stop:       []HookEntry{{Command: "ccf verify"}},
	}
	require.NoError(t, hooks.Validate())
}

func TestHooksConfigV3_Validate_nil(t *testing.T) {
	require.NoError(t, (*HooksConfigV3)(nil).Validate())
}

func TestLoadHooksConfig_loads_empty_command_without_error(t *testing.T) {
	// Validation is deferred to ValidateV3, not LoadHooksConfig
	dir := t.TempDir()
	hooksContent := `stop:
  - command: ""
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, hooksYAMLFilename), []byte(hooksContent), 0o644))

	hooks, err := LoadHooksConfig(dir)
	require.NoError(t, err)
	require.NotNil(t, hooks)

	// But Validate catches it
	require.Error(t, hooks.Validate())
}
