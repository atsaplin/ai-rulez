package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Goldziher/ai-rulez/internal/config"
	"github.com/Goldziher/ai-rulez/internal/generator"
	_ "github.com/Goldziher/ai-rulez/internal/generator/presets" // register all presets
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupMinimalConfig creates a .ai-rulez/config.yaml in the given directory.
func setupMinimalConfig(t *testing.T, configDir string, presets string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	yaml := "version: \"3.0\"\nname: test-project\npresets:\n" + presets
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(yaml), 0o644))
}

// --- Global mode integration tests ---

func TestGlobalMode_generates_files_in_baseDir(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupMinimalConfig(t, configDir, "  - claude\n")

	// Add a rule
	rulesDir := filepath.Join(configDir, "rules")
	require.NoError(t, os.MkdirAll(rulesDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(rulesDir, "test-rule.md"),
		[]byte("# Test rule\nDo the thing."), 0o644,
	))

	cfg, err := config.LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateV3())

	gen := generator.NewGeneratorV3(cfg, false)
	require.NoError(t, gen.Generate(""))

	// CLAUDE.md should be in baseDir, not configDir
	claudeMD := filepath.Join(baseDir, "CLAUDE.md")
	assert.FileExists(t, claudeMD)

	content, err := os.ReadFile(claudeMD)
	require.NoError(t, err)
	assert.Contains(t, string(content), "Do the thing.")

	// .claude/ directory should be in baseDir
	assert.DirExists(t, filepath.Join(baseDir, ".claude"))
	assert.DirExists(t, filepath.Join(baseDir, ".claude", "skills"))
	assert.DirExists(t, filepath.Join(baseDir, ".claude", "agents"))

	// configDir should have NO generated files
	assert.NoFileExists(t, filepath.Join(configDir, "CLAUDE.md"))
}

func TestGlobalMode_with_multiple_presets(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupMinimalConfig(t, configDir, "  - claude\n  - codex\n  - cursor\n")

	rulesDir := filepath.Join(configDir, "rules")
	require.NoError(t, os.MkdirAll(rulesDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(rulesDir, "shared-rule.md"),
		[]byte("# Shared rule\nApplies everywhere."), 0o644,
	))

	cfg, err := config.LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateV3())

	gen := generator.NewGeneratorV3(cfg, false)
	require.NoError(t, gen.Generate(""))

	// All presets should generate in baseDir
	assert.FileExists(t, filepath.Join(baseDir, "CLAUDE.md"))
	assert.FileExists(t, filepath.Join(baseDir, "AGENTS.md")) // Codex
	assert.DirExists(t, filepath.Join(baseDir, ".cursor", "rules"))
}

// --- Hooks integration tests ---

func TestHooks_generates_claude_settings_json(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupMinimalConfig(t, configDir, "  - claude\n")

	// Write hooks.yaml
	hooksYAML := `pre_tool_use:
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
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "hooks.yaml"), []byte(hooksYAML), 0o644))

	cfg, err := config.LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateV3())

	gen := generator.NewGeneratorV3(cfg, false)
	require.NoError(t, gen.Generate(""))

	// settings.json should exist
	settingsPath := filepath.Join(baseDir, ".claude", "settings.json")
	assert.FileExists(t, settingsPath)

	// Parse and verify the JSON structure
	data, err := os.ReadFile(settingsPath)
	require.NoError(t, err)

	var settings map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &settings))

	// Must have "hooks" top-level key
	hooks, ok := settings["hooks"].(map[string]interface{})
	require.True(t, ok, "settings.json must have a 'hooks' object")

	// Verify all 5 event types are present
	assert.Contains(t, hooks, "PreToolUse")
	assert.Contains(t, hooks, "PostToolUse")
	assert.Contains(t, hooks, "Stop")
	assert.Contains(t, hooks, "SessionStart")
	assert.Contains(t, hooks, "PreCompact")

	// Verify PreToolUse structure matches Claude's expected format
	preToolUse, ok := hooks["PreToolUse"].([]interface{})
	require.True(t, ok)
	require.Len(t, preToolUse, 1)

	group := preToolUse[0].(map[string]interface{})
	assert.Equal(t, "Write|Edit", group["matcher"])

	hooksList := group["hooks"].([]interface{})
	require.Len(t, hooksList, 1)

	hook := hooksList[0].(map[string]interface{})
	assert.Equal(t, "command", hook["type"])
	assert.Equal(t, "ccf require-contract", hook["command"])

	// Verify Stop has timeout
	stopEntries := hooks["Stop"].([]interface{})
	stopGroup := stopEntries[0].(map[string]interface{})
	stopHooks := stopGroup["hooks"].([]interface{})
	stopHook := stopHooks[0].(map[string]interface{})
	assert.Equal(t, float64(300), stopHook["timeout"]) // JSON numbers are float64
	assert.Nil(t, stopGroup["matcher"], "Stop hooks should not have a matcher")
}

func TestHooks_no_settings_json_when_no_hooks(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupMinimalConfig(t, configDir, "  - claude\n")
	// No hooks.yaml

	cfg, err := config.LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateV3())

	gen := generator.NewGeneratorV3(cfg, false)
	require.NoError(t, gen.Generate(""))

	// settings.json should NOT be created
	settingsPath := filepath.Join(baseDir, ".claude", "settings.json")
	assert.NoFileExists(t, settingsPath)
}

func TestHooks_only_affect_claude_preset(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupMinimalConfig(t, configDir, "  - claude\n  - codex\n  - cursor\n")

	hooksYAML := `stop:
  - command: "ccf verify-stop"
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "hooks.yaml"), []byte(hooksYAML), 0o644))

	cfg, err := config.LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateV3())

	gen := generator.NewGeneratorV3(cfg, false)
	require.NoError(t, gen.Generate(""))

	// Claude gets hooks in settings.json
	claudeSettings := filepath.Join(baseDir, ".claude", "settings.json")
	assert.FileExists(t, claudeSettings)

	// Codex does not get a settings.json
	codexSettings := filepath.Join(baseDir, ".codex", "settings.json")
	assert.NoFileExists(t, codexSettings)

	// Cursor does not get a settings.json
	cursorSettings := filepath.Join(baseDir, ".cursor", "settings.json")
	assert.NoFileExists(t, cursorSettings)
}

// --- Settings merge integration tests ---

func TestMerge_preserves_existing_user_keys(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupMinimalConfig(t, configDir, "  - claude\n")

	hooksYAML := `stop:
  - command: "ccf verify-stop"
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "hooks.yaml"), []byte(hooksYAML), 0o644))

	// Pre-create a settings.json with user-managed keys
	claudeDir := filepath.Join(baseDir, ".claude")
	require.NoError(t, os.MkdirAll(claudeDir, 0o755))
	existingSettings := `{
  "permissions": {
    "allow": ["Bash(npm test)", "Read"]
  },
  "plugins": ["my-plugin"],
  "model": "opus"
}`
	require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(existingSettings), 0o644))

	cfg, err := config.LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateV3())

	gen := generator.NewGeneratorV3(cfg, false)
	require.NoError(t, gen.Generate(""))

	// Read merged settings
	data, err := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	require.NoError(t, err)

	var settings map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &settings))

	// User keys must survive
	assert.Contains(t, settings, "permissions", "permissions key must survive merge")
	assert.Contains(t, settings, "plugins", "plugins key must survive merge")
	assert.Contains(t, settings, "model", "model key must survive merge")

	// Generated hooks must be present
	assert.Contains(t, settings, "hooks", "hooks key must be added by merge")

	hooks := settings["hooks"].(map[string]interface{})
	assert.Contains(t, hooks, "Stop")
}

func TestMerge_overwrites_existing_hooks(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupMinimalConfig(t, configDir, "  - claude\n")

	hooksYAML := `stop:
  - command: "new-command"
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "hooks.yaml"), []byte(hooksYAML), 0o644))

	// Pre-create settings with OLD hooks
	claudeDir := filepath.Join(baseDir, ".claude")
	require.NoError(t, os.MkdirAll(claudeDir, 0o755))
	existingSettings := `{
  "hooks": {
    "Stop": [{"hooks": [{"type": "command", "command": "old-command"}]}]
  }
}`
	require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(existingSettings), 0o644))

	cfg, err := config.LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateV3())

	gen := generator.NewGeneratorV3(cfg, false)
	require.NoError(t, gen.Generate(""))

	data, err := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	require.NoError(t, err)

	// Old hooks should be replaced
	assert.Contains(t, string(data), "new-command")
	assert.NotContains(t, string(data), "old-command")
}

func TestMerge_creates_file_when_none_exists(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupMinimalConfig(t, configDir, "  - claude\n")

	hooksYAML := `stop:
  - command: "ccf verify"
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "hooks.yaml"), []byte(hooksYAML), 0o644))

	// Do NOT pre-create settings.json
	cfg, err := config.LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateV3())

	gen := generator.NewGeneratorV3(cfg, false)
	require.NoError(t, gen.Generate(""))

	// File should be created with hooks
	settingsPath := filepath.Join(baseDir, ".claude", "settings.json")
	assert.FileExists(t, settingsPath)

	data, err := os.ReadFile(settingsPath)
	require.NoError(t, err)

	var settings map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &settings))
	assert.Contains(t, settings, "hooks")
}

// --- Secrets integration tests (mocked op) ---

func TestSecrets_unresolved_refs_stay_when_op_unavailable(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupMinimalConfig(t, configDir, "  - claude\n")

	// Add MCP server with op:// refs
	mcpYAML := `version: "3.0"
mcp_servers:
  - name: my-server
    command: npx
    args: ["my-server"]
    env:
      API_KEY: "op://vault/item/key"
      PLAIN: "not-a-secret"
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "mcp.yaml"), []byte(mcpYAML), 0o644))

	cfg, err := config.LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateV3())

	// resolveSecrets=true but op is not on PATH in test env (or will fail)
	// The generator should warn and continue, not crash
	gen := generator.NewGeneratorV3(cfg, true)
	require.NoError(t, gen.Generate(""))

	// Generation should complete (files written)
	assert.FileExists(t, filepath.Join(baseDir, "CLAUDE.md"))
}

// --- Full pipeline integration test ---

func TestFullPipeline_global_with_hooks_and_rules(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupMinimalConfig(t, configDir, "  - claude\n  - codex\n")

	// Add rules
	rulesDir := filepath.Join(configDir, "rules")
	require.NoError(t, os.MkdirAll(rulesDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(rulesDir, "quality.md"),
		[]byte("# Quality\nWrite good code."), 0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(rulesDir, "testing.md"),
		[]byte("# Testing\nTest everything."), 0o644,
	))

	// Add hooks
	hooksYAML := `pre_tool_use:
  - matcher: "Write|Edit"
    command: "ccf require-contract"
stop:
  - command: "ccf verify-stop"
    timeout: 300
session_start:
  - command: "ccf session-start"
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "hooks.yaml"), []byte(hooksYAML), 0o644))

	// Pre-create settings.json with user keys
	claudeDir := filepath.Join(baseDir, ".claude")
	require.NoError(t, os.MkdirAll(claudeDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(claudeDir, "settings.json"),
		[]byte(`{"permissions":{"allow":["Bash(npm test)"]}}`), 0o644,
	))

	// Load and generate
	cfg, err := config.LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateV3())

	gen := generator.NewGeneratorV3(cfg, false)
	require.NoError(t, gen.Generate(""))

	// --- Verify Claude outputs ---
	claudeMD, err := os.ReadFile(filepath.Join(baseDir, "CLAUDE.md"))
	require.NoError(t, err)
	assert.Contains(t, string(claudeMD), "Write good code.")
	assert.Contains(t, string(claudeMD), "Test everything.")

	// Verify settings.json has hooks AND preserved user keys
	settingsData, err := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	require.NoError(t, err)

	var settings map[string]interface{}
	require.NoError(t, json.Unmarshal(settingsData, &settings))

	// User key preserved
	assert.Contains(t, settings, "permissions")

	// Hooks present with correct structure
	hooks := settings["hooks"].(map[string]interface{})
	assert.Contains(t, hooks, "PreToolUse")
	assert.Contains(t, hooks, "Stop")
	assert.Contains(t, hooks, "SessionStart")
	assert.NotContains(t, hooks, "PostToolUse", "PostToolUse not in hooks.yaml")

	// Verify Stop hook has timeout
	stopEntries := hooks["Stop"].([]interface{})
	stopGroup := stopEntries[0].(map[string]interface{})
	stopHooks := stopGroup["hooks"].([]interface{})
	stopHook := stopHooks[0].(map[string]interface{})
	assert.Equal(t, "ccf verify-stop", stopHook["command"])
	assert.Equal(t, float64(300), stopHook["timeout"])

	// --- Verify Codex outputs ---
	agentsMD, err := os.ReadFile(filepath.Join(baseDir, "AGENTS.md"))
	require.NoError(t, err)
	assert.Contains(t, string(agentsMD), "Write good code.")
	assert.Contains(t, string(agentsMD), "Test everything.")

	// Codex should NOT have settings.json
	assert.NoFileExists(t, filepath.Join(baseDir, ".codex", "settings.json"))
}
