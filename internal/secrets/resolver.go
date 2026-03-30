package secrets

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/samber/oops"
)

const opTimeout = 10 * time.Second

// commandRunner executes a command and returns its stdout.
type commandRunner func(ctx context.Context, name string, args ...string) (string, error)

// defaultCommandRunner shells out to the real binary with a context deadline.
func defaultCommandRunner(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", oops.
				With("command", name).
				Wrapf(err, "%s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", oops.
			With("command", name).
			Wrapf(err, "execute command")
	}
	return strings.TrimSpace(string(out)), nil
}

// Resolver resolves op:// references in MCP server env vars using the 1Password CLI.
type Resolver struct {
	runner commandRunner
	cache  map[string]string
	mu     sync.Mutex
}

// NewResolver creates a Resolver with the default command runner.
func NewResolver() *Resolver {
	return &Resolver{
		runner: defaultCommandRunner,
		cache:  make(map[string]string),
	}
}

// newResolverWithRunner creates a Resolver with a custom command runner (for testing).
func newResolverWithRunner(runner commandRunner) *Resolver {
	return &Resolver{
		runner: runner,
		cache:  make(map[string]string),
	}
}

// isOpReference returns true if the value starts with "op://".
func isOpReference(value string) bool {
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
		if !isOpReference(value) {
			continue
		}
		resolved, err := r.resolve(value)
		if err != nil {
			return nil, oops.
				With("env_key", key).
				Wrapf(err, "resolve secret")
		}
		result[key] = resolved
	}
	return result, nil
}

// resolve resolves a single op:// reference, deduplicating via cache.
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
		return "", oops.Wrapf(err, "op read")
	}

	r.mu.Lock()
	r.cache[ref] = value
	r.mu.Unlock()

	return value, nil
}
