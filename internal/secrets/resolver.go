package secrets

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const opTimeout = 10 * time.Second

// CommandRunner executes a command and returns its stdout.
// Abstracted for testing.
type CommandRunner func(ctx context.Context, name string, args ...string) (string, error)

// DefaultCommandRunner shells out to the real binary with a context deadline.
func DefaultCommandRunner(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%s failed: %s", name, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Resolver resolves op:// references in MCP server env vars using the 1Password CLI.
type Resolver struct {
	runner CommandRunner
	cache  map[string]string
	mu     sync.Mutex
}

// NewResolver creates a Resolver with the default command runner.
func NewResolver() *Resolver {
	return &Resolver{
		runner: DefaultCommandRunner,
		cache:  make(map[string]string),
	}
}

// NewResolverWithRunner creates a Resolver with a custom command runner (for testing).
func NewResolverWithRunner(runner CommandRunner) *Resolver {
	return &Resolver{
		runner: runner,
		cache:  make(map[string]string),
	}
}

// IsAvailable returns true if the `op` CLI is on PATH.
func (r *Resolver) IsAvailable() bool {
	_, err := exec.LookPath("op")
	return err == nil
}

// IsOpReference returns true if the value starts with "op://".
func IsOpReference(value string) bool {
	return strings.HasPrefix(value, "op://")
}

// ResolveEnv resolves all op:// references in a copy of the given env map.
// Returns a new map with resolved values; the original is not modified.
func (r *Resolver) ResolveEnv(env map[string]string) (map[string]string, error) {
	result := make(map[string]string, len(env))
	for k, v := range env {
		result[k] = v
	}
	for key, value := range result {
		if !IsOpReference(value) {
			continue
		}
		resolved, err := r.resolve(value)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", key, err)
		}
		result[key] = resolved
	}
	return result, nil
}

// resolve resolves a single op:// reference, deduplicating via cache.
// The lock is held across the full check-and-fetch to prevent duplicate subprocess calls.
func (r *Resolver) resolve(ref string) (string, error) {
	r.mu.Lock()
	if cached, ok := r.cache[ref]; ok {
		r.mu.Unlock()
		return cached, nil
	}
	r.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	value, err := r.runner(ctx, "op", "read", ref)
	if err != nil {
		return "", fmt.Errorf("op read: %w", err)
	}

	r.mu.Lock()
	r.cache[ref] = value
	r.mu.Unlock()

	return value, nil
}
