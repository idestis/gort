package policy

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Environment variables that configure the execution policy.
const (
	envAllowedExecutors = "GORT_ALLOWED_EXECUTORS"
	envAllowedEnvVars   = "GORT_ALLOWED_ENV_VARS"
)

var (
	// ErrExecutorNotAllowed is returned when a requested executor is not in
	// the configured allowlist.
	ErrExecutorNotAllowed = errors.New("executor not allowed")
	// ErrEnvVarNotAllowed is returned when a requested environment variable
	// is malformed, not allowlisted, or on the hard denylist.
	ErrEnvVarNotAllowed = errors.New("environment variable not allowed")
)

// envKeyPattern is the set of acceptable environment variable names.
var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// deniedEnvKeys can never be set by a request, even when allowlisted, because
// they let a caller change which code the executor loads or runs.
var deniedEnvKeys = map[string]struct{}{
	"PATH":           {},
	"IFS":            {},
	"BASH_ENV":       {},
	"ENV":            {},
	"SHELLOPTS":      {},
	"PS4":            {},
	"PROMPT_COMMAND": {},
	"NODE_OPTIONS":   {},
	"PYTHONPATH":     {},
	"PYTHONSTARTUP":  {},
	"PERL5OPT":       {},
	"PERL5LIB":       {},
	"RUBYOPT":        {},
	"GCONV_PATH":     {},
}

// deniedEnvPrefixes are key prefixes that can never be set by a request.
var deniedEnvPrefixes = []string{"LD_", "DYLD_"}

// Policy decides which executors and environment variables a /v1/start
// request may use. The zero value denies everything.
type Policy struct {
	executors      map[string]struct{}
	envExact       map[string]struct{}
	envPrefixes    []string
	deniedAllowed  []string // allowlist entries that the hard denylist overrides
	envConfigured  bool
	execConfigured bool
}

// Load builds a Policy from GORT_ALLOWED_EXECUTORS and
// GORT_ALLOWED_ENV_VARS as returned by getenv (typically os.Getenv). Both are
// comma-separated lists; entries are whitespace-trimmed and empty entries are
// ignored. GORT_ALLOWED_ENV_VARS entries may end in "*" to allow a prefix.
func Load(getenv func(string) string) Policy {
	p := Policy{
		executors: make(map[string]struct{}),
		envExact:  make(map[string]struct{}),
	}
	for _, e := range splitList(getenv(envAllowedExecutors)) {
		p.executors[e] = struct{}{}
	}
	p.execConfigured = len(p.executors) > 0

	for _, e := range splitList(getenv(envAllowedEnvVars)) {
		if strings.HasSuffix(e, "*") {
			prefix := strings.TrimSuffix(e, "*")
			p.envPrefixes = append(p.envPrefixes, prefix)
			if prefixOverlapsDenylist(prefix) {
				p.deniedAllowed = append(p.deniedAllowed, e)
			}
		} else {
			p.envExact[e] = struct{}{}
			if isDeniedEnvKey(e) {
				p.deniedAllowed = append(p.deniedAllowed, e)
			}
		}
	}
	p.envConfigured = len(p.envExact) > 0 || len(p.envPrefixes) > 0
	sort.Strings(p.deniedAllowed)
	return p
}

// CheckExecutor returns nil if name exactly matches an allowlisted executor,
// otherwise an error wrapping ErrExecutorNotAllowed.
func (p Policy) CheckExecutor(name string) error {
	if name == "" {
		return fmt.Errorf("%w: empty executor name", ErrExecutorNotAllowed)
	}
	if _, ok := p.executors[name]; !ok {
		return fmt.Errorf("%w: %q", ErrExecutorNotAllowed, name)
	}
	return nil
}

// CheckEnvVars returns nil if every entry is a well-formed KEY=VALUE whose KEY
// is allowlisted and not on the hard denylist. Otherwise it returns an error
// wrapping ErrEnvVarNotAllowed that names the first offending key.
func (p Policy) CheckEnvVars(vars []string) error {
	for _, v := range vars {
		i := strings.IndexByte(v, '=')
		if i < 0 {
			return fmt.Errorf("%w: %q is not KEY=VALUE", ErrEnvVarNotAllowed, v)
		}
		key := v[:i]
		if !envKeyPattern.MatchString(key) {
			return fmt.Errorf("%w: invalid key %q", ErrEnvVarNotAllowed, key)
		}
		if isDeniedEnvKey(key) {
			return fmt.Errorf("%w: %q is on the hard denylist", ErrEnvVarNotAllowed, key)
		}
		if !p.envAllowed(key) {
			return fmt.Errorf("%w: %q", ErrEnvVarNotAllowed, key)
		}
	}
	return nil
}

// Warnings returns human-readable messages describing risky or restrictive
// configuration, suitable for logging at startup.
func (p Policy) Warnings() []string {
	var w []string
	if !p.execConfigured {
		w = append(w, fmt.Sprintf("%s is not set: every executor is rejected, so /v1/start is effectively disabled; "+
			"set %s to a comma-separated list of executors (e.g. \"python3,/bin/bash\") to enable it",
			envAllowedExecutors, envAllowedExecutors))
	}
	if !p.envConfigured {
		w = append(w, fmt.Sprintf("%s is not set: requests that pass env_vars are rejected; "+
			"set %s to a comma-separated list of keys (e.g. \"APP_ENV,APP_*\") to allow some",
			envAllowedEnvVars, envAllowedEnvVars))
	}
	for _, e := range p.deniedAllowed {
		w = append(w, fmt.Sprintf("%s entry %q overlaps the hard denylist; those keys will still be rejected",
			envAllowedEnvVars, e))
	}
	return w
}

func (p Policy) envAllowed(key string) bool {
	if _, ok := p.envExact[key]; ok {
		return true
	}
	for _, prefix := range p.envPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func isDeniedEnvKey(key string) bool {
	if _, ok := deniedEnvKeys[key]; ok {
		return true
	}
	for _, prefix := range deniedEnvPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// prefixOverlapsDenylist reports whether a wildcard allowlist prefix would
// match any hard-denied key.
func prefixOverlapsDenylist(prefix string) bool {
	for k := range deniedEnvKeys {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	for _, d := range deniedEnvPrefixes {
		if strings.HasPrefix(d, prefix) || strings.HasPrefix(prefix, d) {
			return true
		}
	}
	return false
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
