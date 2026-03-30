package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShallowMergeJSON_overlapping_keys(t *testing.T) {
	existing := []byte(`{"hooks":{"old":"value"},"plugins":["foo"],"theme":"dark"}`)
	generated := []byte(`{"hooks":{"new":"value"},"statusLine":"enabled"}`)

	result, err := shallowMergeJSON(existing, generated)
	require.NoError(t, err)

	// hooks should be replaced (not deep-merged), plugins and theme survive
	assert.Contains(t, string(result), `"hooks"`)
	assert.Contains(t, string(result), `"new"`)
	assert.NotContains(t, string(result), `"old"`)
	assert.Contains(t, string(result), `"plugins"`)
	assert.Contains(t, string(result), `"theme"`)
	assert.Contains(t, string(result), `"statusLine"`)
}

func TestShallowMergeJSON_disjoint_keys(t *testing.T) {
	existing := []byte(`{"alpha":"1"}`)
	generated := []byte(`{"beta":"2"}`)

	result, err := shallowMergeJSON(existing, generated)
	require.NoError(t, err)

	assert.Contains(t, string(result), `"alpha"`)
	assert.Contains(t, string(result), `"beta"`)
}

func TestShallowMergeJSON_empty_existing(t *testing.T) {
	generated := []byte(`{"key":"value"}`)

	result, err := shallowMergeJSON(nil, generated)
	require.NoError(t, err)
	assert.Equal(t, generated, result)

	result, err = shallowMergeJSON([]byte{}, generated)
	require.NoError(t, err)
	assert.Equal(t, generated, result)
}

func TestShallowMergeJSON_malformed_existing(t *testing.T) {
	existing := []byte(`not json at all`)
	generated := []byte(`{"key":"value"}`)

	result, err := shallowMergeJSON(existing, generated)
	require.NoError(t, err)
	// Falls back to overwrite
	assert.Equal(t, generated, result)
}

func TestShallowMergeJSON_malformed_generated(t *testing.T) {
	existing := []byte(`{"key":"value"}`)
	generated := []byte(`not json`)

	_, err := shallowMergeJSON(existing, generated)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal generated JSON")
}

func TestShallowMergeJSON_trailing_newline(t *testing.T) {
	existing := []byte(`{"a":"1"}`)
	generated := []byte(`{"b":"2"}`)

	result, err := shallowMergeJSON(existing, generated)
	require.NoError(t, err)
	assert.Equal(t, byte('\n'), result[len(result)-1])
}

func TestShallowMergeJSON_generated_overwrites_existing_key(t *testing.T) {
	existing := []byte(`{"hooks":{"pre":"old"},"plugins":["keep"]}`)
	generated := []byte(`{"hooks":{"pre":"new","post":"added"}}`)

	result, err := shallowMergeJSON(existing, generated)
	require.NoError(t, err)

	// hooks key should be completely replaced (shallow merge, not deep)
	assert.Contains(t, string(result), `"new"`)
	assert.Contains(t, string(result), `"added"`)
	assert.NotContains(t, string(result), `"old"`)
	// plugins key should survive
	assert.Contains(t, string(result), `"plugins"`)
}
