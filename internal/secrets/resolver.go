package secrets

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// CommandRunner executes a command and returns its stdout.
// Abstracted for testing.
type CommandRunner func(name string, args ...string) (string, error)

// DefaultCommandRunner shells out to the real binary.
func DefaultCommandRunner(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
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

// ResolveEnv resolves all op:// references in the given env map in place.
// Non-op:// values are left unchanged.
func (r *Resolver) ResolveEnv(env map[string]string) error {
	for key, value := range env {
		if !IsOpReference(value) {
			continue
		}
		resolved, err := r.resolve(value)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", key, err)
		}
		env[key] = resolved
	}
	return nil
}

// resolve resolves a single op:// reference, using the cache to avoid duplicate lookups.
func (r *Resolver) resolve(ref string) (string, error) {
	r.mu.Lock()
	if cached, ok := r.cache[ref]; ok {
		r.mu.Unlock()
		return cached, nil
	}
	r.mu.Unlock()

	value, err := r.runner("op", "read", ref)
	if err != nil {
		return "", fmt.Errorf("op read %s: %w", ref, err)
	}

	r.mu.Lock()
	r.cache[ref] = value
	r.mu.Unlock()

	return value, nil
}
