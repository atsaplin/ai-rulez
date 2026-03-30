package secrets

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mockRunner(responses map[string]string) CommandRunner {
	return func(name string, args ...string) (string, error) {
		if name != "op" || len(args) < 2 || args[0] != "read" {
			return "", fmt.Errorf("unexpected command: %s %v", name, args)
		}
		ref := args[1]
		if val, ok := responses[ref]; ok {
			return val, nil
		}
		return "", fmt.Errorf("op read: item not found: %s", ref)
	}
}

func TestIsOpReference(t *testing.T) {
	assert.True(t, IsOpReference("op://vault/item/field"))
	assert.False(t, IsOpReference("plain-value"))
	assert.False(t, IsOpReference(""))
	assert.False(t, IsOpReference("OP://uppercase"))
}

func TestResolveEnv_resolves_op_references(t *testing.T) {
	runner := mockRunner(map[string]string{
		"op://vault/item/key": "secret-value-123",
	})
	r := NewResolverWithRunner(runner)

	env := map[string]string{
		"API_KEY":  "op://vault/item/key",
		"PLAIN":   "not-a-secret",
		"ANOTHER": "also-plain",
	}

	err := r.ResolveEnv(env)
	require.NoError(t, err)

	assert.Equal(t, "secret-value-123", env["API_KEY"])
	assert.Equal(t, "not-a-secret", env["PLAIN"])
	assert.Equal(t, "also-plain", env["ANOTHER"])
}

func TestResolveEnv_caches_duplicate_refs(t *testing.T) {
	var callCount atomic.Int32
	runner := func(name string, args ...string) (string, error) {
		callCount.Add(1)
		return "cached-secret", nil
	}
	r := NewResolverWithRunner(runner)

	env := map[string]string{
		"KEY1": "op://vault/item/field",
		"KEY2": "op://vault/item/field",
	}

	err := r.ResolveEnv(env)
	require.NoError(t, err)

	assert.Equal(t, "cached-secret", env["KEY1"])
	assert.Equal(t, "cached-secret", env["KEY2"])
	// The runner should only be called once due to caching.
	// Map iteration order is non-deterministic, so we accept 1 call.
	assert.Equal(t, int32(1), callCount.Load())
}

func TestResolveEnv_returns_error_on_op_failure(t *testing.T) {
	runner := func(name string, args ...string) (string, error) {
		return "", fmt.Errorf("op: authentication required")
	}
	r := NewResolverWithRunner(runner)

	env := map[string]string{
		"SECRET": "op://vault/item/field",
	}

	err := r.ResolveEnv(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve SECRET")
	assert.Contains(t, err.Error(), "authentication required")
}

func TestResolveEnv_empty_map(t *testing.T) {
	r := NewResolverWithRunner(mockRunner(nil))
	err := r.ResolveEnv(map[string]string{})
	require.NoError(t, err)
}

func TestResolveEnv_no_op_refs(t *testing.T) {
	var called bool
	runner := func(name string, args ...string) (string, error) {
		called = true
		return "", nil
	}
	r := NewResolverWithRunner(runner)

	env := map[string]string{
		"PLAIN": "value",
	}

	err := r.ResolveEnv(env)
	require.NoError(t, err)
	assert.False(t, called, "runner should not be called when there are no op:// refs")
}
