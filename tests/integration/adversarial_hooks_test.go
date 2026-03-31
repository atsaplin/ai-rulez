package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Goldziher/ai-rulez/internal/config"
	"github.com/Goldziher/ai-rulez/internal/generator"
	_ "github.com/Goldziher/ai-rulez/internal/generator/presets" // register all presets
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test 1: hooks.yaml with ONLY events that no preset supports (pre_compact + notification with codex).
// Expected: codex produces no hooks.json (both events are filtered out, nativeHooks is empty).
func TestAdversarial_codex_only_unsupported_events_no_file(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupMinimalConfig(t, configDir, "  - codex\n")

	// pre_compact and notification are not in codexEventNames
	hooksYAML := `pre_compact:
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

	// Codex should NOT produce a hooks.json because all events were filtered
	hooksPath := filepath.Join(baseDir, ".codex", "hooks.json")
	assert.NoFileExists(t, hooksPath,
		"codex hooks.json must not be created when all events are unsupported by Codex")
}

// Test 2: commands containing shell metacharacters must be preserved verbatim.
// The command field is data, not executed by the generator — no escaping should occur.
func TestAdversarial_shell_metacharacters_preserved_verbatim(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupMinimalConfig(t, configDir, "  - claude\n")

	dollarSubst := "$(whoami)"
	backtick := "`id`"
	pipe := "echo foo | tee /tmp/out"
	semicolon := "echo a; echo b"

	hooksYAML := "stop:\n" +
		"  - command: \"" + dollarSubst + "\"\n" +
		"pre_tool_use:\n" +
		"  - command: \"" + backtick + "\"\n" +
		"post_tool_use:\n" +
		"  - command: \"" + pipe + "\"\n" +
		"session_start:\n" +
		"  - command: \"" + semicolon + "\"\n"

	require.NoError(t, os.WriteFile(filepath.Join(configDir, "hooks.yaml"), []byte(hooksYAML), 0o644))

	cfg, err := config.LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateV3())

	gen := generator.NewGeneratorV3(cfg, false)
	require.NoError(t, gen.Generate(""))

	settingsPath := filepath.Join(baseDir, ".claude", "settings.json")
	require.FileExists(t, settingsPath)

	data, err := os.ReadFile(settingsPath)
	require.NoError(t, err)

	// JSON must be valid
	var settings map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &settings), "settings.json must be valid JSON")

	raw := string(data)
	assert.Contains(t, raw, dollarSubst, "$(whoami) must survive unchanged")
	assert.Contains(t, raw, backtick, "backtick command must survive unchanged")
	assert.Contains(t, raw, pipe, "pipe command must survive unchanged")
	assert.Contains(t, raw, semicolon, "semicolon command must survive unchanged")
}

// Test 3: very long command string (10KB). JSON marshaling must not truncate or error.
func TestAdversarial_very_long_command_string(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupMinimalConfig(t, configDir, "  - claude\n")

	longCmd := strings.Repeat("x", 10*1024)
	hooksYAML := "stop:\n  - command: \"" + longCmd + "\"\n"

	require.NoError(t, os.WriteFile(filepath.Join(configDir, "hooks.yaml"), []byte(hooksYAML), 0o644))

	cfg, err := config.LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateV3())

	gen := generator.NewGeneratorV3(cfg, false)
	require.NoError(t, gen.Generate(""))

	settingsPath := filepath.Join(baseDir, ".claude", "settings.json")
	require.FileExists(t, settingsPath)

	data, err := os.ReadFile(settingsPath)
	require.NoError(t, err)

	var settings map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &settings), "settings.json must be valid JSON")

	// The full 10KB command must be present in the output
	assert.Contains(t, string(data), longCmd, "10KB command must be preserved in full")
}

// Test 4: unicode and emoji in command and matcher fields.
func TestAdversarial_unicode_and_emoji_in_commands(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupMinimalConfig(t, configDir, "  - claude\n")

	emojiCmd := "echo 🚀 && ./run.sh"
	unicodeMatcher := "文件写入|파일쓰기|Schreiben"
	arabicCmd := "عملية التحقق"

	hooksYAML := "stop:\n  - command: \"" + emojiCmd + "\"\n" +
		"pre_tool_use:\n  - matcher: \"" + unicodeMatcher + "\"\n    command: \"check\"\n" +
		"session_start:\n  - command: \"" + arabicCmd + "\"\n"

	require.NoError(t, os.WriteFile(filepath.Join(configDir, "hooks.yaml"), []byte(hooksYAML), 0o644))

	cfg, err := config.LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateV3())

	gen := generator.NewGeneratorV3(cfg, false)
	require.NoError(t, gen.Generate(""))

	settingsPath := filepath.Join(baseDir, ".claude", "settings.json")
	require.FileExists(t, settingsPath)

	data, err := os.ReadFile(settingsPath)
	require.NoError(t, err)

	var settings map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &settings), "settings.json must be valid JSON with unicode content")

	raw := string(data)
	// JSON encodes non-ASCII as \uXXXX or keeps as UTF-8; the decoded value must round-trip
	hooks := settings["hooks"].(map[string]interface{})

	stopEntries := hooks["Stop"].([]interface{})
	stopHook := stopEntries[0].(map[string]interface{})["hooks"].([]interface{})[0].(map[string]interface{})
	assert.Equal(t, emojiCmd, stopHook["command"], "emoji command must round-trip through JSON")

	preToolUse := hooks["PreToolUse"].([]interface{})
	group := preToolUse[0].(map[string]interface{})
	assert.Equal(t, unicodeMatcher, group["matcher"], "unicode matcher must round-trip through JSON")

	sessionStart := hooks["SessionStart"].([]interface{})
	sessionHook := sessionStart[0].(map[string]interface{})["hooks"].([]interface{})[0].(map[string]interface{})
	assert.Equal(t, arabicCmd, sessionHook["command"], "Arabic command must round-trip through JSON")

	_ = raw
}

// Test 5: same event defined twice in hooks.yaml (duplicate YAML keys).
// go-yaml v3 treats duplicate mapping keys as a parse error. The loader catches this,
// logs a warning, and proceeds with nil hooks (no settings.json produced).
// This is a behavior documentation test — the system does not silently drop or merge.
func TestAdversarial_duplicate_event_keys_rejected_as_parse_error(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupMinimalConfig(t, configDir, "  - claude\n")

	// YAML with duplicate "stop" key — go-yaml v3 rejects this as a parse error
	hooksYAML := `stop:
  - command: "first-command"
stop:
  - command: "second-command"
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "hooks.yaml"), []byte(hooksYAML), 0o644))

	// LoadConfigV3FromDir logs a warning but does NOT return an error for invalid hooks.yaml;
	// it proceeds with nil hooks.
	cfg, err := config.LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err, "LoadConfigV3FromDir must not propagate hooks parse errors as fatal")

	// With nil hooks, ValidateV3 must still pass
	require.NoError(t, cfg.ValidateV3())

	gen := generator.NewGeneratorV3(cfg, false)
	require.NoError(t, gen.Generate(""))

	// Because hooks failed to parse, no settings.json should be produced
	settingsPath := filepath.Join(baseDir, ".claude", "settings.json")
	assert.NoFileExists(t, settingsPath,
		"duplicate YAML key causes hooks parse failure; settings.json must not be created")
}

// Test 6: Gemini preset with hooks AND MCP — verify hooks key doesn't clobber mcpServers or vice versa.
func TestAdversarial_gemini_hooks_and_mcp_coexist(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupMinimalConfig(t, configDir, "  - gemini\n")

	hooksYAML := `pre_tool_use:
  - matcher: "Write"
    command: "ccf check"
stop:
  - command: "ccf stop"
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "hooks.yaml"), []byte(hooksYAML), 0o644))

	cfg, err := config.LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateV3())

	gen := generator.NewGeneratorV3(cfg, false)
	require.NoError(t, gen.Generate(""))

	settingsPath := filepath.Join(baseDir, ".gemini", "settings.json")
	require.FileExists(t, settingsPath)

	data, err := os.ReadFile(settingsPath)
	require.NoError(t, err)

	var settings map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &settings), "settings.json must be valid JSON")

	// Both keys must be present and independent
	mcpServers, hasMCP := settings["mcpServers"]
	require.True(t, hasMCP, "mcpServers must be present")
	require.NotNil(t, mcpServers, "mcpServers must not be nil")

	hooks, hasHooks := settings["hooks"]
	require.True(t, hasHooks, "hooks must be present")
	require.NotNil(t, hooks, "hooks must not be nil")

	hooksMap := hooks.(map[string]interface{})
	mcpMap := mcpServers.(map[string]interface{})

	// Neither key should bleed into the other
	assert.NotContains(t, hooksMap, "ai-rulez", "hooks must not contain MCP server entries")
	assert.NotContains(t, mcpMap, "BeforeTool", "mcpServers must not contain hook event entries")
	assert.NotContains(t, mcpMap, "AfterAgent", "mcpServers must not contain hook event entries")

	// Verify hook event names are Gemini-native
	assert.Contains(t, hooksMap, "BeforeTool")
	assert.Contains(t, hooksMap, "AfterAgent")
}

// Test 7: multiple hooks per event (3 PreToolUse entries with different matchers).
// All 3 must be preserved in order.
func TestAdversarial_multiple_hooks_per_event_all_preserved(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupMinimalConfig(t, configDir, "  - claude\n")

	hooksYAML := `pre_tool_use:
  - matcher: "Write"
    command: "ccf check-write"
  - matcher: "Edit"
    command: "ccf check-edit"
  - matcher: "Bash"
    command: "ccf check-bash"
    timeout: 60
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "hooks.yaml"), []byte(hooksYAML), 0o644))

	cfg, err := config.LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateV3())

	gen := generator.NewGeneratorV3(cfg, false)
	require.NoError(t, gen.Generate(""))

	settingsPath := filepath.Join(baseDir, ".claude", "settings.json")
	require.FileExists(t, settingsPath)

	data, err := os.ReadFile(settingsPath)
	require.NoError(t, err)

	var settings map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &settings), "settings.json must be valid JSON")

	hooks := settings["hooks"].(map[string]interface{})
	preToolUse, ok := hooks["PreToolUse"].([]interface{})
	require.True(t, ok)
	require.Len(t, preToolUse, 3, "all 3 PreToolUse entries must be preserved")

	// Verify each entry in order
	entry0 := preToolUse[0].(map[string]interface{})
	assert.Equal(t, "Write", entry0["matcher"])
	hook0 := entry0["hooks"].([]interface{})[0].(map[string]interface{})
	assert.Equal(t, "ccf check-write", hook0["command"])
	assert.Nil(t, hook0["timeout"], "no timeout on first hook")

	entry1 := preToolUse[1].(map[string]interface{})
	assert.Equal(t, "Edit", entry1["matcher"])
	hook1 := entry1["hooks"].([]interface{})[0].(map[string]interface{})
	assert.Equal(t, "ccf check-edit", hook1["command"])

	entry2 := preToolUse[2].(map[string]interface{})
	assert.Equal(t, "Bash", entry2["matcher"])
	hook2 := entry2["hooks"].([]interface{})[0].(map[string]interface{})
	assert.Equal(t, "ccf check-bash", hook2["command"])
	assert.Equal(t, float64(60), hook2["timeout"])
}

// Test 8: hook entry with matcher but no command should fail validation.
func TestAdversarial_matcher_without_command_fails_validation(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupMinimalConfig(t, configDir, "  - claude\n")

	hooksYAML := `pre_tool_use:
  - matcher: "Write"
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "hooks.yaml"), []byte(hooksYAML), 0o644))

	cfg, err := config.LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)

	// ValidateV3 should catch the empty command
	err = cfg.ValidateV3()
	require.Error(t, err, "hook with matcher but no command must fail validation")
	assert.Contains(t, err.Error(), "empty command",
		"validation error must mention empty command")

	_ = baseDir
}

// Test 9: hook entry with timeout=0 must be omitted from JSON output (not emitted as 0).
func TestAdversarial_timeout_zero_omitted_from_json(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupMinimalConfig(t, configDir, "  - claude\n")

	hooksYAML := `stop:
  - command: "ccf verify"
    timeout: 0
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "hooks.yaml"), []byte(hooksYAML), 0o644))

	cfg, err := config.LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateV3())

	gen := generator.NewGeneratorV3(cfg, false)
	require.NoError(t, gen.Generate(""))

	settingsPath := filepath.Join(baseDir, ".claude", "settings.json")
	require.FileExists(t, settingsPath)

	data, err := os.ReadFile(settingsPath)
	require.NoError(t, err)

	var settings map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &settings), "settings.json must be valid JSON")

	hooks := settings["hooks"].(map[string]interface{})
	stopEntries := hooks["Stop"].([]interface{})
	require.Len(t, stopEntries, 1)

	stopHook := stopEntries[0].(map[string]interface{})["hooks"].([]interface{})[0].(map[string]interface{})

	// timeout=0 means "use default" — it must not appear in the JSON at all
	_, hasTimeout := stopHook["timeout"]
	assert.False(t, hasTimeout,
		"timeout=0 must be omitted from JSON output, not emitted as 0")
}

// Test 10: hooks.yaml with YAML anchors and aliases.
// go-yaml v3 supports anchors natively; the resolved values should be used.
func TestAdversarial_yaml_anchors_and_aliases_resolved(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupMinimalConfig(t, configDir, "  - claude\n")

	// Define a base hook via anchor and reuse it via alias
	hooksYAML := `_base_hook: &base
  command: "ccf verify-stop"
  timeout: 120

stop:
  - <<: *base

session_start:
  - command: "ccf session-start"
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "hooks.yaml"), []byte(hooksYAML), 0o644))

	cfg, err := config.LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateV3())

	gen := generator.NewGeneratorV3(cfg, false)
	require.NoError(t, gen.Generate(""))

	settingsPath := filepath.Join(baseDir, ".claude", "settings.json")
	require.FileExists(t, settingsPath)

	data, err := os.ReadFile(settingsPath)
	require.NoError(t, err)

	var settings map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &settings), "settings.json must be valid JSON")

	hooks := settings["hooks"].(map[string]interface{})

	// The anchor-merged stop hook should resolve command and timeout
	stopEntries, ok := hooks["Stop"].([]interface{})
	require.True(t, ok, "Stop must be present after anchor resolution")
	require.Len(t, stopEntries, 1)

	stopHook := stopEntries[0].(map[string]interface{})["hooks"].([]interface{})[0].(map[string]interface{})
	assert.Equal(t, "ccf verify-stop", stopHook["command"],
		"anchor-merged command must be resolved correctly")
	assert.Equal(t, float64(120), stopHook["timeout"],
		"anchor-merged timeout must be resolved correctly")

	// session_start should also be present
	assert.Contains(t, hooks, "SessionStart")
}
