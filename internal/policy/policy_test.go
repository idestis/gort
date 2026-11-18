package policy

import (
	"errors"
	"strings"
	"testing"
)

func envFunc(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestCheckExecutor(t *testing.T) {
	tests := []struct {
		name     string
		allowed  string
		executor string
		wantErr  bool
	}{
		{"unset denies all", "", "python3", true},
		{"whitespace only denies all", " , ,", "python3", true},
		{"empty name rejected", "python3", "", true},
		{"bare name allowed", "python3", "python3", false},
		{"trimmed entries", "  bash , python3 ,,", "python3", false},
		{"trimmed entries first", "  bash , python3 ,,", "bash", false},
		{"not listed", "python3", "rm", true},
		{"bare does not allow path", "python3", "/tmp/python3", true},
		{"path does not allow bare", "/usr/bin/python3", "python3", true},
		{"absolute path allowed", "/usr/bin/python3", "/usr/bin/python3", false},
		{"no prefix match", "python", "python3", true},
		{"case sensitive", "python3", "Python3", true},
		{"no wildcard for executors", "py*", "python3", true},
		{"padded request not trimmed", "python3", " python3", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Load(envFunc(map[string]string{envAllowedExecutors: tt.allowed}))
			err := p.CheckExecutor(tt.executor)
			if (err != nil) != tt.wantErr {
				t.Fatalf("CheckExecutor(%q) error = %v, wantErr %v", tt.executor, err, tt.wantErr)
			}
			if err != nil {
				if !errors.Is(err, ErrExecutorNotAllowed) {
					t.Errorf("error %v does not wrap ErrExecutorNotAllowed", err)
				}
				if errors.Is(err, ErrEnvVarNotAllowed) {
					t.Errorf("error %v unexpectedly wraps ErrEnvVarNotAllowed", err)
				}
				if tt.executor != "" && !strings.Contains(err.Error(), tt.executor) {
					t.Errorf("error %q does not mention executor %q", err, tt.executor)
				}
			}
		})
	}
}

func TestZeroPolicyDeniesEverything(t *testing.T) {
	var p Policy
	if err := p.CheckExecutor("sh"); !errors.Is(err, ErrExecutorNotAllowed) {
		t.Errorf("zero Policy CheckExecutor = %v, want ErrExecutorNotAllowed", err)
	}
	if err := p.CheckEnvVars([]string{"A=1"}); !errors.Is(err, ErrEnvVarNotAllowed) {
		t.Errorf("zero Policy CheckEnvVars = %v, want ErrEnvVarNotAllowed", err)
	}
	if err := p.CheckEnvVars(nil); err != nil {
		t.Errorf("zero Policy CheckEnvVars(nil) = %v, want nil", err)
	}
}

func TestCheckEnvVars(t *testing.T) {
	tests := []struct {
		name      string
		allowed   string
		vars      []string
		wantErr   bool
		wantInErr string
	}{
		{"nil always fine when unset", "", nil, false, ""},
		{"empty always fine when unset", "", []string{}, false, ""},
		{"nil fine when set", "FOO", nil, false, ""},
		{"unset rejects any", "", []string{"FOO=1"}, true, "FOO"},
		{"exact allowed", "FOO", []string{"FOO=1"}, false, ""},
		{"empty value allowed", "FOO", []string{"FOO="}, false, ""},
		{"value containing equals", "FOO", []string{"FOO=a=b"}, false, ""},
		{"multiple allowed", " FOO , BAR ", []string{"FOO=1", "BAR=2"}, false, ""},
		{"one of many rejected", "FOO", []string{"FOO=1", "BAZ=2"}, true, "BAZ"},
		{"case sensitive", "FOO", []string{"foo=1"}, true, "foo"},
		{"no implicit prefix", "FOO", []string{"FOOBAR=1"}, true, "FOOBAR"},
		{"underscore start ok", "_X", []string{"_X=1"}, false, ""},

		// malformed
		{"missing equals", "FOO", []string{"FOO"}, true, "FOO"},
		{"empty key", "FOO", []string{"=1"}, true, ""},
		{"empty string", "FOO", []string{""}, true, ""},
		{"key starts with digit", "1FOO", []string{"1FOO=1"}, true, "1FOO"},
		{"key with dash", "FOO-BAR", []string{"FOO-BAR=1"}, true, "FOO-BAR"},
		{"key with space", "FOO", []string{"FOO =1"}, true, "FOO "},
		{"key with dot", "A.B", []string{"A.B=1"}, true, "A.B"},
		{"key with NUL", "FOO", []string{"FOO\x00=1"}, true, ""},

		// wildcards
		{"wildcard match", "APP_*", []string{"APP_ENV=prod", "APP_=x"}, false, ""},
		{"wildcard non-match", "APP_*", []string{"APPX=1"}, true, "APPX"},
		{"wildcard case sensitive", "APP_*", []string{"app_env=1"}, true, "app_env"},
		{"star allows all non-denied", "*", []string{"ANYTHING=1"}, false, ""},
		{"wildcard plus exact", "APP_*,DEBUG", []string{"DEBUG=1", "APP_X=2"}, false, ""},

		// hard denylist precedence
		{"PATH exact allowlisted", "PATH", []string{"PATH=/tmp"}, true, "PATH"},
		{"LD_PRELOAD exact allowlisted", "LD_PRELOAD", []string{"LD_PRELOAD=/tmp/x.so"}, true, "LD_PRELOAD"},
		{"LD_ wildcard", "LD_*", []string{"LD_LIBRARY_PATH=/tmp"}, true, "LD_LIBRARY_PATH"},
		{"DYLD_ via star", "*", []string{"DYLD_INSERT_LIBRARIES=/tmp/x"}, true, "DYLD_INSERT_LIBRARIES"},
		{"L wildcard cannot reach LD_", "L*", []string{"LD_PRELOAD=x"}, true, "LD_PRELOAD"},
		{"L wildcard allows others", "L*", []string{"LANG=C"}, false, ""},
		{"P wildcard cannot reach PYTHONPATH", "P*", []string{"PYTHONPATH=/tmp"}, true, "PYTHONPATH"},
		{"star blocks BASH_ENV", "*", []string{"BASH_ENV=/tmp/x"}, true, "BASH_ENV"},
		{"star blocks NODE_OPTIONS", "*", []string{"NODE_OPTIONS=--require=x"}, true, "NODE_OPTIONS"},
		{"denylist is case sensitive", "path", []string{"path=x"}, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Load(envFunc(map[string]string{
				envAllowedExecutors: "python3",
				envAllowedEnvVars:   tt.allowed,
			}))
			err := p.CheckEnvVars(tt.vars)
			if (err != nil) != tt.wantErr {
				t.Fatalf("CheckEnvVars(%q) error = %v, wantErr %v", tt.vars, err, tt.wantErr)
			}
			if err != nil {
				if !errors.Is(err, ErrEnvVarNotAllowed) {
					t.Errorf("error %v does not wrap ErrEnvVarNotAllowed", err)
				}
				if errors.Is(err, ErrExecutorNotAllowed) {
					t.Errorf("error %v unexpectedly wraps ErrExecutorNotAllowed", err)
				}
				if tt.wantInErr != "" && !strings.Contains(err.Error(), tt.wantInErr) {
					t.Errorf("error %q does not mention %q", err, tt.wantInErr)
				}
			}
		})
	}
}

func TestHardDenylistAlwaysWins(t *testing.T) {
	keys := []string{
		"PATH", "IFS", "BASH_ENV", "ENV", "SHELLOPTS", "PS4", "PROMPT_COMMAND",
		"NODE_OPTIONS", "PYTHONPATH", "PYTHONSTARTUP", "PERL5OPT", "PERL5LIB",
		"RUBYOPT", "GCONV_PATH", "LD_PRELOAD", "LD_LIBRARY_PATH", "LD_AUDIT",
		"DYLD_INSERT_LIBRARIES", "DYLD_LIBRARY_PATH",
	}
	allowed := "*," + strings.Join(keys, ",")
	p := Load(envFunc(map[string]string{envAllowedEnvVars: allowed}))
	for _, k := range keys {
		t.Run(k, func(t *testing.T) {
			err := p.CheckEnvVars([]string{k + "=x"})
			if !errors.Is(err, ErrEnvVarNotAllowed) {
				t.Fatalf("CheckEnvVars(%s=x) = %v, want ErrEnvVarNotAllowed", k, err)
			}
			if !strings.Contains(err.Error(), k) {
				t.Errorf("error %q does not mention %q", err, k)
			}
		})
	}
}

func TestWarnings(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		wantContain []string
		wantAbsent  []string
		wantCount   int
	}{
		{
			name:        "nothing configured",
			env:         map[string]string{},
			wantContain: []string{"GORT_ALLOWED_EXECUTORS", "/v1/start is effectively disabled", "GORT_ALLOWED_ENV_VARS"},
			wantCount:   2,
		},
		{
			name:        "only executors",
			env:         map[string]string{envAllowedExecutors: "python3"},
			wantContain: []string{"GORT_ALLOWED_ENV_VARS"},
			wantAbsent:  []string{"effectively disabled"},
			wantCount:   1,
		},
		{
			name:      "fully configured safe",
			env:       map[string]string{envAllowedExecutors: "python3", envAllowedEnvVars: "APP_*,DEBUG"},
			wantCount: 0,
		},
		{
			name:        "denylisted exact entries",
			env:         map[string]string{envAllowedExecutors: "python3", envAllowedEnvVars: "DEBUG,PATH,LD_PRELOAD"},
			wantContain: []string{`"PATH"`, `"LD_PRELOAD"`, "denylist"},
			wantAbsent:  []string{`"DEBUG"`},
			wantCount:   2,
		},
		{
			name:        "denylisted wildcard entries",
			env:         map[string]string{envAllowedExecutors: "python3", envAllowedEnvVars: "LD_*,DYLD_LIB*,*"},
			wantContain: []string{`"LD_*"`, `"DYLD_LIB*"`, `"*"`},
			wantCount:   3,
		},
		{
			name:        "wildcard overlapping exact denied key",
			env:         map[string]string{envAllowedExecutors: "python3", envAllowedEnvVars: "PYTHON*"},
			wantContain: []string{`"PYTHON*"`},
			wantCount:   1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := Load(envFunc(tt.env)).Warnings()
			if len(w) != tt.wantCount {
				t.Errorf("got %d warnings, want %d: %q", len(w), tt.wantCount, w)
			}
			joined := strings.Join(w, "\n")
			for _, s := range tt.wantContain {
				if !strings.Contains(joined, s) {
					t.Errorf("warnings %q missing %q", w, s)
				}
			}
			for _, s := range tt.wantAbsent {
				if strings.Contains(joined, s) {
					t.Errorf("warnings %q unexpectedly contain %q", w, s)
				}
			}
		})
	}
}

func TestLoadReadsOnlyItsVariables(t *testing.T) {
	var seen []string
	Load(func(k string) string {
		seen = append(seen, k)
		return ""
	})
	want := map[string]bool{envAllowedExecutors: true, envAllowedEnvVars: true}
	if len(seen) != len(want) {
		t.Fatalf("getenv called with %q, want exactly %v", seen, want)
	}
	for _, k := range seen {
		if !want[k] {
			t.Errorf("unexpected getenv(%q)", k)
		}
	}
}
