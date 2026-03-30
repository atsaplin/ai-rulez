package secrets

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mockRunner(responses map[string]string) commandRunner {
	return func(_ context.Context, name string, args ...string) (string, error) {
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
	assert.True(t, isOpReference("op://vault/item/field"))
	assert.False(t, isOpReference("plain-value"))
	assert.False(t, isOpReference(""))
	assert.False(t, isOpReference("OP://uppercase"))
}

func TestResolveEnv_resolves_op_references(t *testing.T) {
	runner := mockRunner(map[string]string{
		"op://vault/item/key": "secret-value-123",
	})
	r := newResolverWithRunner(runner)

	env := map[string]string{
		"API_KEY":  "op://vault/item/key",
		"PLAIN":   "not-a-secret",
		"ANOTHER": "also-plain",
	}

	result, err := r.ResolveEnv(env)
	require.NoError(t, err)

	// Original map is not mutated
	assert.Equal(t, "op://vault/item/key", env["API_KEY"])

	// Result has resolved values
	assert.Equal(t, "secret-value-123", result["API_KEY"])
	assert.Equal(t, "not-a-secret", result["PLAIN"])
	assert.Equal(t, "also-plain", result["ANOTHER"])
}

func TestResolveEnv_caches_duplicate_refs(t *testing.T) {
	var callCount atomic.Int32
	runner := func(_ context.Context, _ string, _ ...string) (string, error) {
		callCount.Add(1)
		return "cached-secret", nil
	}
	r := newResolverWithRunner(runner)

	env := map[string]string{
		"KEY1": "op://vault/item/field",
		"KEY2": "op://vault/item/field",
	}

	result, err := r.ResolveEnv(env)
	require.NoError(t, err)

	assert.Equal(t, "cached-secret", result["KEY1"])
	assert.Equal(t, "cached-secret", result["KEY2"])
	assert.Equal(t, int32(1), callCount.Load())
}

func TestResolveEnv_returns_error_on_op_failure(t *testing.T) {
	runner := func(_ context.Context, _ string, _ ...string) (string, error) {
		return "", fmt.Errorf("authentication required")
	}
	r := newResolverWithRunner(runner)

	env := map[string]string{
		"SECRET": "op://vault/item/field",
	}

	_, err := r.ResolveEnv(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve secret")
}

func TestResolveEnv_empty_map(t *testing.T) {
	r := newResolverWithRunner(mockRunner(nil))
	result, err := r.ResolveEnv(map[string]string{})
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestResolveEnv_no_op_refs(t *testing.T) {
	var called bool
	runner := func(_ context.Context, _ string, _ ...string) (string, error) {
		called = true
		return "", nil
	}
	r := newResolverWithRunner(runner)

	env := map[string]string{
		"PLAIN": "value",
	}

	result, err := r.ResolveEnv(env)
	require.NoError(t, err)
	assert.False(t, called, "runner should not be called when there are no op:// refs")
	assert.Equal(t, "value", result["PLAIN"])
}

func TestResolveEnv_context_deadline_exceeded(t *testing.T) {
	runner := func(ctx context.Context, _ string, _ ...string) (string, error) {
		// Simulate op hanging until context expires
		<-ctx.Done()
		return "", ctx.Err()
	}
	r := newResolverWithRunner(runner)

	env := map[string]string{
		"KEY": "op://vault/item/field",
	}

	_, err := r.ResolveEnv(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}

func TestResolveEnv_op_not_found_returns_error(t *testing.T) {
	runner := func(_ context.Context, _ string, _ ...string) (string, error) {
		return "", fmt.Errorf("exec: \"op\": executable file not found in $PATH")
	}
	r := newResolverWithRunner(runner)

	env := map[string]string{
		"KEY": "op://vault/item/field",
	}

	_, err := r.ResolveEnv(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve secret")
}

func TestResolveEnv_single_op_ref_in_env(t *testing.T) {
	runner := mockRunner(map[string]string{
		"op://vault/item/key": "secret",
	})
	r := newResolverWithRunner(runner)

	env := map[string]string{
		"ONLY_KEY": "op://vault/item/key",
	}

	result, err := r.ResolveEnv(env)
	require.NoError(t, err)
	assert.Equal(t, "secret", result["ONLY_KEY"])
}

func TestResolveEnv_does_not_mutate_original(t *testing.T) {
	runner := mockRunner(map[string]string{
		"op://vault/item/key": "resolved",
	})
	r := newResolverWithRunner(runner)

	original := map[string]string{
		"KEY": "op://vault/item/key",
	}

	result, err := r.ResolveEnv(original)
	require.NoError(t, err)

	assert.Equal(t, "op://vault/item/key", original["KEY"], "original must not be mutated")
	assert.Equal(t, "resolved", result["KEY"])
}
