package generator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// adversarial_merge_test.go: adversarial tests for shallowMergeJSON and writeOutput.
// Goal: find inputs that cause incorrect output, data loss, or silent corruption.
// Tests are numbered to match the task specification.

// 1. Enormous existing file: 1MB object with 1000 keys.
// Expectation: merge succeeds, all existing keys survive, generated key overwrites.
func TestAdversarial_EnormousExistingFile(t *testing.T) {
	// Build a 1000-key existing object; pad values so total size is ~1MB
	entries := make([]string, 1000)
	padding := strings.Repeat("x", 980) // ~1KB per value
	for i := 0; i < 1000; i++ {
		entries[i] = fmt.Sprintf(`"key%04d":"%s"`, i, padding)
	}
	existing := []byte("{" + strings.Join(entries, ",") + "}")
	generated := []byte(`{"hooks":{"new":"value"}}`)

	start := time.Now()
	result, err := shallowMergeJSON(existing, generated)
	elapsed := time.Since(start)

	require.NoError(t, err)
	// Should complete in reasonable time (under 5 seconds)
	assert.Less(t, elapsed, 5*time.Second, "merge took too long: %v", elapsed)

	// All original keys must survive
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf(`"key%04d"`, i)
		assert.Contains(t, string(result), key, "key%04d lost after merge", i)
	}

	// Generated key must be present
	assert.Contains(t, string(result), `"hooks"`)
	assert.Contains(t, string(result), `"new"`)
}

// 2. Deeply nested existing JSON (10 levels deep).
// Shallow merge must preserve the deep structure under its top-level key intact.
func TestAdversarial_DeeplyNestedExisting(t *testing.T) {
	// Build 10-level nested object: {"l1":{"l2":{"l3":...{"l10":"deep"}...}}}
	nested := `"l10":"deep_value"`
	for level := 9; level >= 1; level-- {
		nested = fmt.Sprintf(`"l%d":{%s}`, level, nested)
	}
	existing := []byte("{" + nested + "}")
	generated := []byte(`{"newkey":"newval"}`)

	result, err := shallowMergeJSON(existing, generated)
	require.NoError(t, err)

	// The top-level key l1 must survive with its deep structure untouched
	assert.Contains(t, string(result), `"l1"`)
	assert.Contains(t, string(result), `"deep_value"`)
	assert.Contains(t, string(result), `"newkey"`)

	// Verify the entire deep structure round-trips correctly
	var merged map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(result, &merged))
	rawL1, ok := merged["l1"]
	require.True(t, ok, "l1 key missing from merged result")

	// Unmarshal the deep chain and verify l10 survives
	current := map[string]json.RawMessage{}
	require.NoError(t, json.Unmarshal(rawL1, &current))
	for level := 2; level <= 9; level++ {
		key := fmt.Sprintf("l%d", level)
		next, ok := current[key]
		require.True(t, ok, "level %d key missing", level)
		current = map[string]json.RawMessage{}
		require.NoError(t, json.Unmarshal(next, &current))
	}
	l10Raw, ok := current["l10"]
	require.True(t, ok, "l10 key missing")
	assert.Equal(t, `"deep_value"`, string(l10Raw))
}

// 3. Existing file has null, boolean, and numeric values at top level.
// Expectation: all survive unchanged when generated doesn't touch them.
func TestAdversarial_NullBoolNumericTopLevel(t *testing.T) {
	existing := []byte(`{"nullKey":null,"boolTrue":true,"boolFalse":false,"intVal":42,"floatVal":3.14}`)
	generated := []byte(`{"newKey":"new"}`)

	result, err := shallowMergeJSON(existing, generated)
	require.NoError(t, err)

	var merged map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(result, &merged))

	assert.Equal(t, "null", string(merged["nullKey"]))
	assert.Equal(t, "true", string(merged["boolTrue"]))
	assert.Equal(t, "false", string(merged["boolFalse"]))
	assert.Equal(t, "42", string(merged["intVal"]))
	assert.Equal(t, "3.14", string(merged["floatVal"]))
	assert.Equal(t, `"new"`, string(merged["newKey"]))
}

// 4. Generated JSON has a key whose value is null.
// The generated null MUST overwrite the existing non-null value (generated wins by spec).
func TestAdversarial_GeneratedNullOverwritesExisting(t *testing.T) {
	existing := []byte(`{"hooks":{"pre":"keep"},"nullTarget":"existing_value"}`)
	generated := []byte(`{"nullTarget":null}`)

	result, err := shallowMergeJSON(existing, generated)
	require.NoError(t, err)

	var merged map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(result, &merged))

	// Generated null must win over existing value
	nullVal, ok := merged["nullTarget"]
	require.True(t, ok, "nullTarget key must be present")
	assert.Equal(t, "null", string(nullVal), "generated null must overwrite existing value")

	// Unrelated key must survive
	_, ok = merged["hooks"]
	assert.True(t, ok, "hooks key must survive")
}

// 5. Type conflict: same key has string in existing, object in generated.
// By spec, generated wins. Verify no error and correct type survives.
func TestAdversarial_TypeConflictStringVsObject(t *testing.T) {
	existing := []byte(`{"conflictKey":"i am a string","other":"survives"}`)
	generated := []byte(`{"conflictKey":{"nested":"object"}}`)

	result, err := shallowMergeJSON(existing, generated)
	require.NoError(t, err)

	var merged map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(result, &merged))

	// Generated object must win
	raw, ok := merged["conflictKey"]
	require.True(t, ok)
	var obj map[string]string
	require.NoError(t, json.Unmarshal(raw, &obj), "conflictKey should be an object after merge")
	assert.Equal(t, "object", obj["nested"])

	// Other key survives
	assert.Equal(t, `"survives"`, string(merged["other"]))
}

// Also test the reverse: existing has object, generated has string.
func TestAdversarial_TypeConflictObjectVsString(t *testing.T) {
	existing := []byte(`{"conflictKey":{"was":"object"},"other":"survives"}`)
	generated := []byte(`{"conflictKey":"now a string"}`)

	result, err := shallowMergeJSON(existing, generated)
	require.NoError(t, err)

	var merged map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(result, &merged))

	raw := merged["conflictKey"]
	assert.Equal(t, `"now a string"`, string(raw), "generated string must win over existing object")
}

// 6. Existing file has a UTF-8 BOM prefix.
// BOM is 0xEF 0xBB 0xBF. json.Unmarshal does NOT handle BOM; it will fail.
// Expectation: falls back to overwrite (same as malformed JSON path), no error returned.
func TestAdversarial_ExistingWithBOM(t *testing.T) {
	bom := []byte{0xEF, 0xBB, 0xBF}
	existing := append(bom, []byte(`{"preserved":"value"}`)...)
	generated := []byte(`{"newkey":"newval"}`)

	result, err := shallowMergeJSON(existing, generated)
	require.NoError(t, err, "BOM should not cause an error to be returned")

	// With BOM, json.Unmarshal fails, so the code falls back to overwrite.
	// The result will be the generated content (existing is silently discarded).
	// This is a DATA LOSS scenario: "preserved":"value" is gone.
	// We document the actual behavior here.
	resultStr := string(result)
	assert.Contains(t, resultStr, `"newkey"`, "generated content must be in result")

	// Document whether the existing key survived or was silently dropped
	if !strings.Contains(resultStr, `"preserved"`) {
		t.Log("FINDING: BOM causes silent data loss - existing content is overwritten without merge")
	}
}

// 7. Existing file has trailing whitespace and newlines after valid JSON.
// json.Unmarshal is strict and rejects trailing content in some versions.
// Verify behavior is correct (merge or overwrite, no error).
func TestAdversarial_ExistingWithTrailingWhitespace(t *testing.T) {
	existing := []byte(`{"preserved":"value"}` + "   \n\n\t  ")
	generated := []byte(`{"newkey":"newval"}`)

	result, err := shallowMergeJSON(existing, generated)
	require.NoError(t, err)

	resultStr := string(result)
	assert.Contains(t, resultStr, `"newkey"`, "generated key must be present")

	// Check if trailing whitespace causes merge failure (data loss) or succeeds
	if strings.Contains(resultStr, `"preserved"`) {
		t.Log("FINDING: trailing whitespace handled correctly - merge succeeded")
	} else {
		t.Log("FINDING: trailing whitespace causes fallback to overwrite - existing key lost")
	}
}

// 8. Existing file is an empty JSON string value "" (not empty bytes).
// This is a valid JSON primitive, not an object. Must fall back to overwrite.
func TestAdversarial_ExistingIsEmptyJSONString(t *testing.T) {
	existing := []byte(`""`) // valid JSON string, not an object
	generated := []byte(`{"key":"value"}`)

	result, err := shallowMergeJSON(existing, generated)
	require.NoError(t, err)
	// Falls back to overwrite since "" is not a JSON object
	assert.Equal(t, generated, result)
}

// 9. Generated content is an empty JSON object {}.
// Merging {} with existing should produce the existing content unchanged (no keys to overwrite).
func TestAdversarial_GeneratedIsEmptyObject(t *testing.T) {
	existing := []byte(`{"keep":"this","also":"keep"}`)
	generated := []byte(`{}`)

	result, err := shallowMergeJSON(existing, generated)
	require.NoError(t, err)

	var merged map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(result, &merged), "result must be valid JSON")

	assert.Equal(t, `"this"`, string(merged["keep"]), "keep key must survive")
	assert.Equal(t, `"keep"`, string(merged["also"]), "also key must survive")
	assert.Len(t, merged, 2, "no extra keys should appear")
}

// 10. Race condition between os.ReadFile and os.WriteFile in writeOutput.
// We can't reliably trigger the OS race without hooking the filesystem,
// but we can verify the write is NOT atomic (no temp+rename pattern).
// This test documents the window, runs concurrent writes, and checks for corruption.
func TestAdversarial_ConcurrentWriteRace(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "settings.json")

	// Write initial file
	initial := []byte(`{"existing":"value","counter":0}`)
	require.NoError(t, os.WriteFile(targetPath, initial, 0o644))

	const goroutines = 20
	var wg sync.WaitGroup
	errors := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			generated := fmt.Sprintf(`{"counter":%d}`, idx)
			existing, err := os.ReadFile(targetPath)
			if err != nil {
				errors[idx] = err
				return
			}
			merged, err := shallowMergeJSON(existing, []byte(generated))
			if err != nil {
				errors[idx] = err
				return
			}
			errors[idx] = os.WriteFile(targetPath, merged, 0o644)
		}(i)
	}
	wg.Wait()

	for i, err := range errors {
		assert.NoError(t, err, "goroutine %d failed", i)
	}

	// Final file must be valid JSON regardless of which write won
	finalContent, err := os.ReadFile(targetPath)
	require.NoError(t, err)

	var finalMap map[string]json.RawMessage
	err = json.Unmarshal(finalContent, &finalMap)
	if err != nil {
		t.Logf("FINDING: concurrent writes caused JSON corruption: %v", err)
		t.Logf("Corrupt content: %s", string(finalContent))
	} else {
		t.Log("FINDING: file is valid JSON after concurrent writes (one writer won cleanly)")
		// The existing key may or may not be present depending on which read/write pair won
		_, hasExisting := finalMap["existing"]
		t.Logf("existing key survived: %v", hasExisting)
	}
}

// 11. Existing file has duplicate keys: {"a":1,"a":2}.
// Go's json.Unmarshal silently uses the LAST value for duplicate keys.
// This means earlier values are silently discarded during merge.
func TestAdversarial_DuplicateKeysInExisting(t *testing.T) {
	// JSON with duplicate key - technically invalid per RFC 8259 but parsers vary
	existing := []byte(`{"a":1,"b":"keep","a":2}`)
	generated := []byte(`{"newkey":"newval"}`)

	result, err := shallowMergeJSON(existing, generated)
	require.NoError(t, err)

	var merged map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(result, &merged))

	// Go's json.Unmarshal uses the LAST value for "a"; first value is silently dropped
	aVal := merged["a"]
	t.Logf("FINDING: duplicate key 'a' resolved to: %s (Go uses last value, first is silently dropped)", string(aVal))
	assert.Equal(t, "2", string(aVal), "Go json.Unmarshal keeps last duplicate value")

	// b must survive
	assert.Equal(t, `"keep"`, string(merged["b"]))
	assert.Equal(t, `"newval"`, string(merged["newkey"]))
}

// Bonus: verify the output of shallowMergeJSON is always valid JSON (round-trip test).
func TestAdversarial_OutputIsAlwaysValidJSON(t *testing.T) {
	cases := []struct {
		name      string
		existing  []byte
		generated []byte
	}{
		{"both empty objects", []byte(`{}`), []byte(`{}`)},
		{"existing empty bytes", []byte{}, []byte(`{"k":"v"}`)},
		{"single key each", []byte(`{"a":1}`), []byte(`{"b":2}`)},
		{"unicode values", []byte(`{"a":"héllo 世界"}`), []byte(`{"b":"🎉"}`)},
		{"nested values", []byte(`{"a":{"b":{"c":3}}}`), []byte(`{"x":[1,2,3]}`)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := shallowMergeJSON(tc.existing, tc.generated)
			require.NoError(t, err)
			var v interface{}
			require.NoError(t, json.Unmarshal(result, &v), "output must be valid JSON: %s", string(result))
		})
	}
}
