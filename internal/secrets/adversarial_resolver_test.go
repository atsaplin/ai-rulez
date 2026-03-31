package secrets

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test 1: op:// ref with spaces in vault/item path.
// The ref is passed verbatim as an argument to the runner. We verify the
// runner receives the exact string (spaces included) and that resolution
// succeeds — there is no sanitisation layer, so this is purely a passthrough
// question.
func TestAdversarial_op_ref_with_spaces(t *testing.T) {
	const ref = "op://My Vault/My Item/password"
	runner := mockRunner(map[string]string{
		ref: "hunter2",
	})
	r := newResolverWithRunner(runner)

	result, err := r.ResolveEnv(map[string]string{"SECRET": ref})
	require.NoError(t, err)
	assert.Equal(t, "hunter2", result["SECRET"],
		"spaces in op:// ref should be passed through unchanged to the runner")
}

// Test 2: op:// ref containing shell metacharacters.
// The runner is invoked via exec.CommandContext with individual args, so shell
// injection through $() or backticks in the ref string should be inert at the
// Go level. We confirm the ref string is forwarded literally.
func TestAdversarial_op_ref_with_shell_metacharacters(t *testing.T) {
	const ref = "op://vault/$(whoami)/key"
	runner := mockRunner(map[string]string{
		ref: "not-injected",
	})
	r := newResolverWithRunner(runner)

	result, err := r.ResolveEnv(map[string]string{"KEY": ref})
	require.NoError(t, err)
	assert.Equal(t, "not-injected", result["KEY"],
		"shell metacharacters must not be evaluated; ref forwarded as literal arg")
}

// Test 3: op:// ref that is just "op://" with nothing after it.
// isOpReference returns true for this value (it has the prefix), so the
// runner will be called. We verify the resolver passes the bare ref to the
// runner and propagates whatever the runner returns — no panic, no special
// handling.
func TestAdversarial_op_ref_bare_prefix_only(t *testing.T) {
	const ref = "op://"
	runner := mockRunner(map[string]string{
		ref: "resolved-empty-path",
	})
	r := newResolverWithRunner(runner)

	result, err := r.ResolveEnv(map[string]string{"KEY": ref})
	require.NoError(t, err)
	assert.Equal(t, "resolved-empty-path", result["KEY"],
		"bare 'op://' must be forwarded to runner without panic")
}

// Test 4: op:// ref that is extremely long (10 KB).
// We verify no panic, no truncation, and that the full ref is used as a
// cache key and passed verbatim to the runner.
func TestAdversarial_op_ref_extremely_long(t *testing.T) {
	longPath := strings.Repeat("a", 10*1024)
	ref := "op://vault/" + longPath + "/field"

	var receivedRef string
	runner := func(_ context.Context, _ string, args ...string) (string, error) {
		if len(args) >= 2 {
			receivedRef = args[1]
		}
		return "long-ref-secret", nil
	}
	r := newResolverWithRunner(runner)

	result, err := r.ResolveEnv(map[string]string{"KEY": ref})
	require.NoError(t, err)
	assert.Equal(t, "long-ref-secret", result["KEY"])
	assert.Equal(t, ref, receivedRef, "full 10KB ref must be forwarded untruncated")
}

// Test 5: runner that returns empty string "" as resolved value.
// The empty string must be cached and used as the resolved value — the
// resolver must not interpret "" as "not found" or skip caching it.
func TestAdversarial_runner_returns_empty_string(t *testing.T) {
	var callCount atomic.Int32
	runner := func(_ context.Context, _ string, _ ...string) (string, error) {
		callCount.Add(1)
		return "", nil // legitimate empty secret
	}
	r := newResolverWithRunner(runner)

	env := map[string]string{
		"KEY1": "op://vault/item/field",
		"KEY2": "op://vault/item/field", // same ref: should use cache
	}

	result, err := r.ResolveEnv(env)
	require.NoError(t, err)
	assert.Equal(t, "", result["KEY1"], "empty resolved value must be preserved")
	assert.Equal(t, "", result["KEY2"], "empty resolved value must be preserved for second key")
	assert.Equal(t, int32(1), callCount.Load(),
		"empty string must be cached; runner called only once for duplicate ref")

	// Confirm the empty string is also in the cache for a second call.
	result2, err := r.ResolveEnv(map[string]string{"KEY3": "op://vault/item/field"})
	require.NoError(t, err)
	assert.Equal(t, "", result2["KEY3"])
	assert.Equal(t, int32(1), callCount.Load(),
		"cached empty string must be returned on subsequent ResolveEnv calls without re-invoking runner")
}

// Test 6: runner that returns a value containing newlines, tabs, and null bytes.
// These are valid Go string contents. The resolver must not strip or escape them
// (defaultCommandRunner does TrimSpace on real output, but our custom runner
// bypasses that). We verify the value comes through intact from the custom runner.
func TestAdversarial_runner_returns_value_with_special_bytes(t *testing.T) {
	special := "line1\nline2\ttabbed\x00nullbyte"
	runner := func(_ context.Context, _ string, _ ...string) (string, error) {
		return special, nil
	}
	r := newResolverWithRunner(runner)

	result, err := r.ResolveEnv(map[string]string{"KEY": "op://vault/item/field"})
	require.NoError(t, err)
	assert.Equal(t, special, result["KEY"],
		"special bytes (newlines, tabs, null) must survive the resolver unchanged")
}

// Test 7: env map with 1000 entries, all with distinct op:// refs.
// Verifies no deadlock, no panic, and that all 1000 refs are resolved.
// Each ref is unique so the cache does not reduce runner calls.
func TestAdversarial_large_env_map_1000_entries(t *testing.T) {
	const n = 1000

	responses := make(map[string]string, n)
	for i := range n {
		ref := fmt.Sprintf("op://vault/item/field%d", i)
		responses[ref] = fmt.Sprintf("secret-%d", i)
	}

	runner := mockRunner(responses)
	r := newResolverWithRunner(runner)

	env := make(map[string]string, n)
	for i := range n {
		env[fmt.Sprintf("KEY_%d", i)] = fmt.Sprintf("op://vault/item/field%d", i)
	}

	start := time.Now()
	result, err := r.ResolveEnv(env)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Len(t, result, n)

	for i := range n {
		key := fmt.Sprintf("KEY_%d", i)
		assert.Equal(t, fmt.Sprintf("secret-%d", i), result[key])
	}

	// Generous bound: 1000 sequential mock calls should finish in well under 5s.
	assert.Less(t, elapsed, 5*time.Second,
		"resolving 1000 entries should not time out or hang")
}

// Test 8: same resolver used across two sequential ResolveEnv calls.
// The cache must persist between calls so the second call does not re-invoke
// the runner for refs it already resolved.
func TestAdversarial_cache_persists_across_sequential_calls(t *testing.T) {
	var callCount atomic.Int32
	runner := func(_ context.Context, _ string, args ...string) (string, error) {
		callCount.Add(1)
		return "cached-value", nil
	}
	r := newResolverWithRunner(runner)

	env := map[string]string{"KEY": "op://vault/item/field"}

	result1, err := r.ResolveEnv(env)
	require.NoError(t, err)
	assert.Equal(t, "cached-value", result1["KEY"])
	assert.Equal(t, int32(1), callCount.Load())

	result2, err := r.ResolveEnv(env)
	require.NoError(t, err)
	assert.Equal(t, "cached-value", result2["KEY"])
	assert.Equal(t, int32(1), callCount.Load(),
		"second ResolveEnv call must use cache; runner must not be called again")
}

// Test 9: runner succeeds on first ref but fails on a different second ref.
// When a later key fails, the function returns an error. The question is:
// does the error leave the result map in a partially-resolved state that
// leaks out to the caller? Because ResolveEnv returns nil on error, partial
// state must not be accessible.
func TestAdversarial_partial_resolution_on_runner_failure(t *testing.T) {
	const goodRef = "op://vault/item/good"
	const badRef = "op://vault/item/bad"

	runner := func(_ context.Context, _ string, args ...string) (string, error) {
		if len(args) >= 2 && args[1] == goodRef {
			return "good-secret", nil
		}
		return "", fmt.Errorf("item not found")
	}
	r := newResolverWithRunner(runner)

	// We can't guarantee which key is processed first due to map iteration
	// order, so use a map where only one key has a good ref and one has bad.
	// The call must return an error.
	env := map[string]string{
		"GOOD": goodRef,
		"BAD":  badRef,
	}

	result, err := r.ResolveEnv(env)

	// The error case: result must be nil so no partial secrets are exposed.
	require.Error(t, err, "error on any ref must cause ResolveEnv to return error")
	assert.Nil(t, result,
		"on error, result must be nil to prevent partial secret exposure")
}

// Test 10: nil env map passed to ResolveEnv.
// A nil map in Go is range-safe (iterating over nil map yields 0 iterations),
// but make(map[string]string, len(nil)) is also safe (len of nil map = 0).
// We expect no panic and an empty (non-nil) result.
func TestAdversarial_nil_env_map(t *testing.T) {
	r := newResolverWithRunner(mockRunner(nil))

	require.NotPanics(t, func() {
		result, err := r.ResolveEnv(nil)
		require.NoError(t, err)
		assert.NotNil(t, result, "result must be a non-nil map even when input is nil")
		assert.Empty(t, result)
	})
}

// Test 11: env map where the value is literally "op://" (bare prefix, nothing after).
// isOpReference returns true for this, so the runner will be called.
// This is identical to Test 3 but focuses on the VALUE (as opposed to the
// "env map value is the bare prefix" reading): we ensure no special-casing
// treats "op://" as a no-op or skips it.
func TestAdversarial_env_value_is_bare_op_prefix(t *testing.T) {
	const bareRef = "op://"

	// Case A: runner can resolve it — value comes through.
	runnerOK := mockRunner(map[string]string{bareRef: "resolved"})
	r1 := newResolverWithRunner(runnerOK)

	result, err := r1.ResolveEnv(map[string]string{"KEY": bareRef})
	require.NoError(t, err)
	assert.Equal(t, "resolved", result["KEY"],
		"bare 'op://' that the runner can handle must be resolved normally")

	// Case B: runner returns an error for the bare ref — error must propagate.
	runnerFail := func(_ context.Context, _ string, _ ...string) (string, error) {
		return "", fmt.Errorf("invalid reference: op://")
	}
	r2 := newResolverWithRunner(runnerFail)

	_, err = r2.ResolveEnv(map[string]string{"KEY": bareRef})
	require.Error(t, err, "runner error for bare 'op://' must propagate as an error")
}
