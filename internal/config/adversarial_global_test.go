package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// minimalConfigYAML is the smallest valid V3 config content.
const minimalConfigYAML = `version: "3.0"
name: adversarial-test
presets:
  - claude
`

// writeMinimalConfig writes a valid config.yaml into dir.
func writeMinimalConfig(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, configYAMLFilename),
		[]byte(minimalConfigYAML),
		0o644,
	))
}

// Test 1: configDir is a symlink pointing to a real config directory.
// Expected: the symlink is resolved transparently and loading succeeds.
func TestAdversarial_configDir_symlink_to_real_dir(t *testing.T) {
	realDir := t.TempDir()
	writeMinimalConfig(t, realDir)

	symlinkDir := filepath.Join(t.TempDir(), "symlinked-config")
	require.NoError(t, os.Symlink(realDir, symlinkDir))

	baseDir := t.TempDir()
	cfg, err := LoadConfigV3FromDir(context.Background(), symlinkDir, baseDir)
	require.NoError(t, err, "symlink to a real config dir should load successfully")
	assert.Equal(t, "adversarial-test", cfg.Name)
}

// Test 2: configDir is a symlink to a file (not a directory).
// Expected: an error is returned because config.yaml cannot be found inside
// what appears as a directory path.
func TestAdversarial_configDir_symlink_to_file(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a plain file.
	targetFile := filepath.Join(tmpDir, "not-a-dir.yaml")
	require.NoError(t, os.WriteFile(targetFile, []byte("hello"), 0o644))

	// Symlink that resolves to a file.
	symlinkPath := filepath.Join(tmpDir, "link-to-file")
	require.NoError(t, os.Symlink(targetFile, symlinkPath))

	baseDir := t.TempDir()
	_, err := LoadConfigV3FromDir(context.Background(), symlinkPath, baseDir)
	// The loader tries to open config.yaml / config.json inside symlinkPath.
	// Since symlinkPath resolves to a file, Stat/ReadDir on
	// symlinkPath/config.yaml will fail → "no config file found".
	require.Error(t, err, "symlink to a file should return an error")
	assert.Contains(t, err.Error(), "no config file found",
		"error should mention missing config file")
}

// Test 3: baseDir contains path traversal components ("/../..").
// filepath.Abs should normalize the path; the loader must not traverse
// outside the intended directory.
func TestAdversarial_baseDir_path_traversal(t *testing.T) {
	configDir := t.TempDir()
	writeMinimalConfig(t, configDir)

	// Construct a path with traversal that resolves inside /tmp.
	traversalBase := "/tmp/base/../../../tmp"

	cfg, err := LoadConfigV3FromDir(context.Background(), configDir, traversalBase)
	require.NoError(t, err, "path traversal in baseDir should not cause a load error")

	// filepath.Abs must have cleaned the path; it must not contain ".." segments.
	assert.False(t, strings.Contains(cfg.BaseDir, ".."),
		"BaseDir must be fully resolved with no traversal sequences, got: %s", cfg.BaseDir)

	// The resolved path should be /tmp (after normalisation), not a deep escape.
	absExpected, err := filepath.Abs(traversalBase)
	require.NoError(t, err)
	assert.Equal(t, absExpected, cfg.BaseDir,
		"BaseDir must equal filepath.Abs of the traversal input")
}

// Test 4: configDir is the empty string "".
// filepath.Abs("") resolves to cwd; the loader will look for config.yaml there.
// Since cwd almost certainly has no config.yaml, we expect an error.
func TestAdversarial_configDir_empty_string(t *testing.T) {
	baseDir := t.TempDir()

	// We do NOT write a config file in cwd, so this should error.
	_, err := LoadConfigV3FromDir(context.Background(), "", baseDir)
	require.Error(t, err, "empty configDir should produce an error when cwd has no config")
	// Accept either "no config file found" or a resolved path error.
	assert.True(t,
		strings.Contains(err.Error(), "no config file found") ||
			strings.Contains(err.Error(), "config"),
		"unexpected error message: %v", err)
}

// Test 5: configDir and baseDir are the same directory.
// The loader should handle this without creating a conflict or panic.
func TestAdversarial_configDir_equals_baseDir(t *testing.T) {
	sharedDir := t.TempDir()
	writeMinimalConfig(t, sharedDir)

	cfg, err := LoadConfigV3FromDir(context.Background(), sharedDir, sharedDir)
	require.NoError(t, err, "configDir == baseDir should not cause a conflict")
	assert.Equal(t, sharedDir, cfg.BaseDir)
	assert.Equal(t, "adversarial-test", cfg.Name)
}

// Test 6: hooks.yaml references nonexistent event types (unknown YAML keys).
// The YAML decoder should silently ignore unknown keys; hooks for known events
// should still load correctly. The loader must not error.
func TestAdversarial_hooks_yaml_unknown_event_types(t *testing.T) {
	configDir := t.TempDir()
	writeMinimalConfig(t, configDir)

	hooksContent := `stop:
  - command: "echo done"
on_explode:
  - command: "echo boom"
nonexistent_event:
  - command: "echo ghost"
`
	require.NoError(t, os.WriteFile(
		filepath.Join(configDir, hooksYAMLFilename),
		[]byte(hooksContent),
		0o644,
	))

	baseDir := t.TempDir()
	cfg, err := LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err, "hooks.yaml with unknown event keys must not error at load time")
	require.NotNil(t, cfg.Hooks)
	assert.Len(t, cfg.Hooks.Stop, 1, "known stop hook should be loaded")
	// Unknown event types are silently ignored by the YAML decoder.
}

// Test 7: Config with a nonexistent built-in preset name ("nonexistent").
// LoadConfigV3FromDir itself does not validate preset names; ValidateV3() does.
// We test both to be explicit.
func TestAdversarial_nonexistent_builtin_preset(t *testing.T) {
	configDir := t.TempDir()
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	badPresetConfig := `version: "3.0"
name: bad-preset-test
presets:
  - nonexistent
`
	require.NoError(t, os.WriteFile(
		filepath.Join(configDir, configYAMLFilename),
		[]byte(badPresetConfig),
		0o644,
	))

	baseDir := t.TempDir()

	// Load should succeed (loader does not validate preset names).
	cfg, err := LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err, "LoadConfigV3FromDir must not validate preset names")

	// ValidateV3 should catch the unknown preset.
	validateErr := cfg.ValidateV3()
	require.Error(t, validateErr, "ValidateV3 must reject unknown built-in preset name")
	assert.Contains(t, validateErr.Error(), "nonexistent",
		"error must identify the bad preset name")
}

// Test 8: Config with version "2.0" instead of "3.0".
// ValidateV3 should reject it with a clear error.
func TestAdversarial_wrong_version(t *testing.T) {
	configDir := t.TempDir()
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	wrongVersionConfig := `version: "2.0"
name: wrong-version-test
presets:
  - claude
`
	require.NoError(t, os.WriteFile(
		filepath.Join(configDir, configYAMLFilename),
		[]byte(wrongVersionConfig),
		0o644,
	))

	baseDir := t.TempDir()

	// Load should succeed (loader does not check version).
	cfg, err := LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	require.NoError(t, err, "LoadConfigV3FromDir must not reject on wrong version during load")
	assert.Equal(t, "2.0", cfg.Version)

	// ValidateV3 must catch the version mismatch.
	validateErr := cfg.ValidateV3()
	require.Error(t, validateErr, "ValidateV3 must reject version != 3.0")
	assert.Contains(t, validateErr.Error(), "2.0",
		"error should include the actual bad version")
	assert.Contains(t, validateErr.Error(), "3.0",
		"error should mention the expected version")
}

// Test 9: baseDir is a file, not a directory.
// LoadConfigV3FromDir only reads baseDir path into cfg.BaseDir at load time.
// The generator would fail when it tries to write there, but the loader itself
// should succeed (it never stat-checks baseDir).
func TestAdversarial_baseDir_is_a_file(t *testing.T) {
	configDir := t.TempDir()
	writeMinimalConfig(t, configDir)

	// Create a file to use as baseDir.
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "i-am-a-file.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("I am not a directory"), 0o644))

	cfg, err := LoadConfigV3FromDir(context.Background(), configDir, filePath)
	// The loader does NOT validate that baseDir is a directory; it just stores the path.
	require.NoError(t, err, "loader should not error when baseDir is a file path")
	assert.Equal(t, filePath, cfg.BaseDir,
		"BaseDir should be set to the file path even though it's not a directory")
}

// Test 10: config.yaml contains a YAML bomb (billion laughs attack).
// The go-yaml decoder does not expand anchors into exponential copies,
// so this should not OOM. It may fail to unmarshal into the struct or
// succeed with zero values; in either case it must not hang or OOM.
func TestAdversarial_yaml_bomb(t *testing.T) {
	configDir := t.TempDir()
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	// Classic billion-laughs YAML bomb using anchors.
	// go-yaml v3 does expand aliases but is bounded by available memory.
	// We use a modest depth (9 levels, 10x each) to avoid actual OOM in tests
	// while still exercising the alias-expansion path.
	yamlBomb := `version: "3.0"
name: bomb-test
presets:
  - claude
a: &a ["lol","lol","lol","lol","lol","lol","lol","lol","lol","lol"]
b: &b [*a,*a,*a,*a,*a,*a,*a,*a,*a,*a]
c: &c [*b,*b,*b,*b,*b,*b,*b,*b,*b,*b]
d: &d [*c,*c,*c,*c,*c,*c,*c,*c,*c,*c]
e: &e [*d,*d,*d,*d,*d,*d,*d,*d,*d,*d]
f: &f [*e,*e,*e,*e,*e,*e,*e,*e,*e,*e]
g: &g [*f,*f,*f,*f,*f,*f,*f,*f,*f,*f]
h: &h [*g,*g,*g,*g,*g,*g,*g,*g,*g,*g]
i: &i [*h,*h,*h,*h,*h,*h,*h,*h,*h,*h]
`
	require.NoError(t, os.WriteFile(
		filepath.Join(configDir, configYAMLFilename),
		[]byte(yamlBomb),
		0o644,
	))

	baseDir := t.TempDir()

	// The test must complete without hanging. If it returns an error that's fine;
	// what we're probing is whether it panics or OOMs.
	//
	// go-yaml v3 will expand the aliases and fail to fit them into the ConfigV3
	// struct fields (they're typed slices not matching string). The decoder may
	// error or silently ignore the fields. Either outcome is acceptable.
	cfg, err := LoadConfigV3FromDir(context.Background(), configDir, baseDir)
	if err != nil {
		// Acceptable: the YAML bomb caused a parse error.
		t.Logf("YAML bomb caused load error (acceptable): %v", err)
		return
	}

	// If it loaded, the known fields should still be correct because the bomb
	// fields don't conflict with ConfigV3 field names.
	assert.Equal(t, "3.0", cfg.Version, "version should parse correctly despite bomb fields")
	assert.Equal(t, "bomb-test", cfg.Name, "name should parse correctly despite bomb fields")
	t.Log("YAML bomb loaded without error (go-yaml silently ignored unknown anchor fields)")
}
