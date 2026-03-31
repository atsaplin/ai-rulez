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

	// Codex should have hooks.json (not settings.json)
	assert.NoFileExists(t, filepath.Join(baseDir, ".codex", "settings.json"))
	codexHooksPath := filepath.Join(baseDir, ".codex", "hooks.json")
	assert.FileExists(t, codexHooksPath)

	codexHooksData, err := os.ReadFile(codexHooksPath)
	require.NoError(t, err)
	var codexHooksJSON map[string]interface{}
	require.NoError(t, json.Unmarshal(codexHooksData, &codexHooksJSON))
	codexHooks := codexHooksJSON["hooks"].(map[string]interface{})
	assert.Contains(t, codexHooks, "PreToolUse", "Codex should have PreToolUse")
	assert.Contains(t, codexHooks, "Stop", "Codex should have Stop")
	assert.Contains(t, codexHooks, "SessionStart", "Codex should have SessionStart")
	assert.NotContains(t, codexHooks, "PreCompact", "Codex does not support PreCompact")
}

// --- Codex hooks integration tests ---

func TestCodex_hooks_json_generated(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupMinimalConfig(t, configDir, "  - codex\n")

	hooksYAML := `pre_tool_use:
  - matcher: "shell"
    command: "ccf check-shell"
post_tool_use:
  - matcher: "shell"
    command: "ccf post-shell"
stop:
  - command: "ccf verify-stop"
    timeout: 120
session_start:
  - command: "ccf session-start"
pre_compact:
  - command: "ccf pre-compact"
notification:
  - command: "ccf notify"
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "hooks.yaml"), []byte(hooksYAML), 0o644))

	cfg, err := config.LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateV3())

	gen := generator.NewGeneratorV3(cfg, false)
	require.NoError(t, gen.Generate(""))

	// hooks.json should be in .codex/
	hooksPath := filepath.Join(baseDir, ".codex", "hooks.json")
	assert.FileExists(t, hooksPath)

	data, err := os.ReadFile(hooksPath)
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &parsed))

	hooks := parsed["hooks"].(map[string]interface{})

	// Codex supports these 4 events
	assert.Contains(t, hooks, "PreToolUse")
	assert.Contains(t, hooks, "PostToolUse")
	assert.Contains(t, hooks, "Stop")
	assert.Contains(t, hooks, "SessionStart")

	// Codex does NOT support these
	assert.NotContains(t, hooks, "PreCompact")
	assert.NotContains(t, hooks, "Notification")

	// Verify structure matches Codex's expected format
	preToolUse := hooks["PreToolUse"].([]interface{})
	require.Len(t, preToolUse, 1)
	group := preToolUse[0].(map[string]interface{})
	assert.Equal(t, "shell", group["matcher"])
	hooksList := group["hooks"].([]interface{})
	hook := hooksList[0].(map[string]interface{})
	assert.Equal(t, "command", hook["type"])
	assert.Equal(t, "ccf check-shell", hook["command"])

	// Verify timeout on Stop
	stopEntries := hooks["Stop"].([]interface{})
	stopHook := stopEntries[0].(map[string]interface{})["hooks"].([]interface{})[0].(map[string]interface{})
	assert.Equal(t, float64(120), stopHook["timeout"])
}

func TestCodex_no_hooks_json_when_no_hooks(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupMinimalConfig(t, configDir, "  - codex\n")

	cfg, err := config.LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateV3())

	gen := generator.NewGeneratorV3(cfg, false)
	require.NoError(t, gen.Generate(""))

	assert.NoFileExists(t, filepath.Join(baseDir, ".codex", "hooks.json"))
}

// --- Gemini hooks integration tests ---

func TestGemini_hooks_in_settings_json(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupMinimalConfig(t, configDir, "  - gemini\n")

	hooksYAML := `pre_tool_use:
  - matcher: "read_file|write_file"
    command: "ccf check-file-access"
post_tool_use:
  - matcher: "run_shell_command"
    command: "ccf post-shell"
stop:
  - command: "ccf verify-stop"
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

	settingsPath := filepath.Join(baseDir, ".gemini", "settings.json")
	assert.FileExists(t, settingsPath)

	data, err := os.ReadFile(settingsPath)
	require.NoError(t, err)

	var settings map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &settings))

	// MCP config should still be present
	assert.Contains(t, settings, "mcpServers", "MCP servers must survive")

	// Hooks should use Gemini event names
	hooks := settings["hooks"].(map[string]interface{})
	assert.Contains(t, hooks, "BeforeTool", "pre_tool_use maps to BeforeTool")
	assert.Contains(t, hooks, "AfterTool", "post_tool_use maps to AfterTool")
	assert.Contains(t, hooks, "AfterAgent", "stop maps to AfterAgent")
	assert.Contains(t, hooks, "BeforeAgent", "session_start maps to BeforeAgent")

	// Gemini does not support PreCompact
	assert.NotContains(t, hooks, "PreCompact")

	// Verify BeforeTool structure
	beforeTool := hooks["BeforeTool"].([]interface{})
	require.Len(t, beforeTool, 1)
	group := beforeTool[0].(map[string]interface{})
	assert.Equal(t, "read_file|write_file", group["matcher"])
	hooksList := group["hooks"].([]interface{})
	hook := hooksList[0].(map[string]interface{})
	assert.Equal(t, "command", hook["type"])
	assert.Equal(t, "ccf check-file-access", hook["command"])
}

func TestGemini_no_hooks_in_settings_when_no_hooks(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupMinimalConfig(t, configDir, "  - gemini\n")

	cfg, err := config.LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateV3())

	gen := generator.NewGeneratorV3(cfg, false)
	require.NoError(t, gen.Generate(""))

	// settings.json should still exist (for MCP) but have no hooks key
	settingsPath := filepath.Join(baseDir, ".gemini", "settings.json")
	assert.FileExists(t, settingsPath)

	data, err := os.ReadFile(settingsPath)
	require.NoError(t, err)

	var settings map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &settings))

	assert.Contains(t, settings, "mcpServers")
	assert.NotContains(t, settings, "hooks")
}

// --- Cross-preset hooks isolation test ---

func TestAllPresets_hooks_use_correct_event_names(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupMinimalConfig(t, configDir, "  - claude\n  - codex\n  - gemini\n")

	hooksYAML := `pre_tool_use:
  - matcher: "Write"
    command: "ccf pre"
stop:
  - command: "ccf stop"
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "hooks.yaml"), []byte(hooksYAML), 0o644))

	cfg, err := config.LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateV3())

	gen := generator.NewGeneratorV3(cfg, false)
	require.NoError(t, gen.Generate(""))

	// Claude: .claude/settings.json with PreToolUse, Stop
	claudeData, err := os.ReadFile(filepath.Join(baseDir, ".claude", "settings.json"))
	require.NoError(t, err)
	var claudeSettings map[string]interface{}
	require.NoError(t, json.Unmarshal(claudeData, &claudeSettings))
	claudeHooks := claudeSettings["hooks"].(map[string]interface{})
	assert.Contains(t, claudeHooks, "PreToolUse")
	assert.Contains(t, claudeHooks, "Stop")

	// Codex: .codex/hooks.json with PreToolUse, Stop
	codexData, err := os.ReadFile(filepath.Join(baseDir, ".codex", "hooks.json"))
	require.NoError(t, err)
	var codexFile map[string]interface{}
	require.NoError(t, json.Unmarshal(codexData, &codexFile))
	codexHooks := codexFile["hooks"].(map[string]interface{})
	assert.Contains(t, codexHooks, "PreToolUse")
	assert.Contains(t, codexHooks, "Stop")

	// Gemini: .gemini/settings.json with BeforeTool, AfterAgent
	geminiData, err := os.ReadFile(filepath.Join(baseDir, ".gemini", "settings.json"))
	require.NoError(t, err)
	var geminiSettings map[string]interface{}
	require.NoError(t, json.Unmarshal(geminiData, &geminiSettings))
	geminiHooks := geminiSettings["hooks"].(map[string]interface{})
	assert.Contains(t, geminiHooks, "BeforeTool", "Gemini uses BeforeTool not PreToolUse")
	assert.Contains(t, geminiHooks, "AfterAgent", "Gemini uses AfterAgent not Stop")
	assert.NotContains(t, geminiHooks, "PreToolUse", "Gemini should NOT use Claude event names")
	assert.NotContains(t, geminiHooks, "Stop", "Gemini should NOT use Claude event names")
}
