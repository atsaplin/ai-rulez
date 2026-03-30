package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupConfigDir(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	configContent := `version: "3.0"
name: test-project
presets:
  - claude
`
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, configYAMLFilename),
		[]byte(configContent), 0o644,
	))
}

func TestLoadConfigV3FromDir_separates_configDir_and_baseDir(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupConfigDir(t, configDir)

	// Add a rule to configDir so we can verify content was scanned from configDir
	rulesPath := filepath.Join(configDir, rulesDir)
	require.NoError(t, os.MkdirAll(rulesPath, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(rulesPath, "test-rule.md"),
		[]byte("# Test rule\nDo the thing."),
		0o644,
	))

	cfg, err := LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)

	// BaseDir should be baseDir (where output goes), not configDir
	assert.Equal(t, baseDir, cfg.BaseDir)

	// Content should be scanned from configDir
	assert.Len(t, cfg.Content.Rules, 1)
	assert.Equal(t, "test-rule", cfg.Content.Rules[0].Name)
}

func TestLoadConfigV3FromDir_identical_to_LoadConfigV3(t *testing.T) {
	// LoadConfigV3(ctx, baseDir) should produce the same result as
	// LoadConfigV3FromDir(ctx, baseDir+"/.ai-rulez", baseDir)
	tempDir := t.TempDir()
	aiRulezDir := filepath.Join(tempDir, aiRulezDirName)
	setupConfigDir(t, aiRulezDir)

	rulesPath := filepath.Join(aiRulezDir, rulesDir)
	require.NoError(t, os.MkdirAll(rulesPath, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(rulesPath, "consistency.md"),
		[]byte("# Consistency\nBe consistent."),
		0o644,
	))

	ctx := context.Background()

	cfgViaWrapper, err := LoadConfigV3(ctx, tempDir)
	require.NoError(t, err)

	cfgViaDirect, err := LoadConfigV3FromDir(ctx, aiRulezDir, tempDir)
	require.NoError(t, err)

	assert.Equal(t, cfgViaWrapper.BaseDir, cfgViaDirect.BaseDir)
	assert.Equal(t, cfgViaWrapper.Version, cfgViaDirect.Version)
	assert.Equal(t, cfgViaWrapper.Name, cfgViaDirect.Name)
	assert.Equal(t, len(cfgViaWrapper.Content.Rules), len(cfgViaDirect.Content.Rules))
}

func TestLoadConfigV3FromDir_loads_mcp_servers(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupConfigDir(t, configDir)

	mcpContent := `version: "3.0"
mcp_servers:
  - name: test-server
    command: echo
    args: ["hello"]
    env:
      FOO: bar
`
	require.NoError(t, os.WriteFile(
		filepath.Join(configDir, mcpYAMLFilename),
		[]byte(mcpContent), 0o644,
	))

	cfg, err := LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)

	assert.Contains(t, cfg.MCPServers, "test-server")
	assert.Equal(t, "echo", cfg.MCPServers["test-server"].Command)
}

func TestLoadConfigV3FromDir_missing_config_file_returns_error(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	// configDir exists but has no config.yaml or config.json
	_, err := LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no config file found")
}

func TestLoadConfigV3FromDir_nonexistent_configDir_returns_error(t *testing.T) {
	baseDir := t.TempDir()
	configDir := filepath.Join(baseDir, "does-not-exist")

	_, err := LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no config file found")
}

func TestLoadConfigV3FromDir_nonexistent_baseDir_succeeds(t *testing.T) {
	// baseDir is only used at write time, not load time
	configDir := t.TempDir()
	setupConfigDir(t, configDir)
	baseDir := filepath.Join(t.TempDir(), "nonexistent")

	cfg, err := LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)
	assert.Equal(t, baseDir, cfg.BaseDir)
}

func TestLoadConfigV3FromDir_loads_hooks(t *testing.T) {
	configDir := t.TempDir()
	baseDir := t.TempDir()

	setupConfigDir(t, configDir)

	hooksContent := `stop:
  - command: "echo done"
`
	require.NoError(t, os.WriteFile(
		filepath.Join(configDir, "hooks.yaml"),
		[]byte(hooksContent), 0o644,
	))

	cfg, err := LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err)
	require.NotNil(t, cfg.Hooks)
	assert.Len(t, cfg.Hooks.Stop, 1)
}

func TestLoadConfigV3FromDir_relative_paths_resolve(t *testing.T) {
	// Create a config dir with a relative path component
	tempDir := t.TempDir()
	configDir := filepath.Join(tempDir, "a", "..", "a")
	setupConfigDir(t, filepath.Join(tempDir, "a"))

	cfg, err := LoadConfigV3FromDir(context.Background(), configDir, tempDir)
	require.NoError(t, err)
	// BaseDir should be the resolved absolute path
	assert.Equal(t, tempDir, cfg.BaseDir)
}
