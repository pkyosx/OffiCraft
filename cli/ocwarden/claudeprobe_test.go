package main

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// (fakeFileInfo — the shared minimal fs.FileInfo — lives in fingerprint_test.go.)

// probeRunner wraps a fakeRunner-style canned map and counts each argv's
// invocations, so cache tests can assert "the subprocess did NOT rerun".
type probeRunner struct {
	out   map[string]string
	errs  map[string]error
	calls map[string]int
}

func newProbeRunner() *probeRunner {
	return &probeRunner{out: map[string]string{}, errs: map[string]error{}, calls: map[string]int{}}
}

func (r *probeRunner) Run(name string, args ...string) (string, error) {
	key := strings.Join(append([]string{name}, args...), " ")
	r.calls[key]++
	if err, ok := r.errs[key]; ok {
		return "", err
	}
	if s, ok := r.out[key]; ok {
		return s, nil
	}
	return "", os.ErrNotExist
}

// newTestProber builds a fully-faked prober: claude resolved at /fake/claude
// (100 bytes, fixed mtime), HOME=/home, cred file present with a readable
// subscriptionType, keychain item present, darwin, controllable clock.
func newTestProber(runner *probeRunner) (*claudeProber, *time.Time, map[string]fakeFileInfo, map[string][]byte) {
	now := time.Unix(1_000_000, 0)
	stats := map[string]fakeFileInfo{
		"/fake/claude":                    {size: 100, mtime: time.Unix(500, 0)},
		"/home/.claude/.credentials.json": {size: 10, mtime: time.Unix(600, 0)},
	}
	files := map[string][]byte{
		"/home/.claude/.credentials.json": []byte(`{"claudeAiOauth":{"subscriptionType":"max","accessToken":"SECRET"}}`),
	}
	runner.out["/fake/claude --version"] = "2.1.211 (Claude Code)\n"
	runner.out["security find-generic-password -s Claude Code-credentials"] = ""
	p := &claudeProber{
		env: func(k string) string {
			if k == "HOME" {
				return "/home"
			}
			return ""
		},
		resolveBin: func() string { return "/fake/claude" },
		stat: func(path string) (os.FileInfo, error) {
			if fi, ok := stats[path]; ok {
				return fi, nil
			}
			return nil, os.ErrNotExist
		},
		readFile: func(path string) ([]byte, error) {
			if b, ok := files[path]; ok {
				return b, nil
			}
			return nil, os.ErrNotExist
		},
		runner: runner,
		goos:   "darwin",
		now:    func() time.Time { return now },
	}
	return p, &now, stats, files
}

func TestClaudeProbe_FullShape(t *testing.T) {
	runner := newProbeRunner()
	p, _, _, _ := newTestProber(runner)
	got := p.collect()
	want := map[string]any{"version": "2.1.211", "cred_file": true, "sub_readable": true, "keychain": true}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("collect()[%q] = %v, want %v", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("collect() = %v, want exactly %v", got, want)
	}
}

func TestClaudeProbe_TTLCacheServesWithoutReprobe(t *testing.T) {
	runner := newProbeRunner()
	p, now, _, _ := newTestProber(runner)
	p.collect()
	// Inside the TTL window: the cached group is served, ZERO subprocess.
	*now = now.Add(claudeProbeTTL - time.Second)
	p.collect()
	if n := runner.calls["/fake/claude --version"]; n != 1 {
		t.Fatalf("--version ran %d times inside the TTL, want 1", n)
	}
	if n := runner.calls["security find-generic-password -s Claude Code-credentials"]; n != 1 {
		t.Fatalf("security ran %d times inside the TTL, want 1", n)
	}
	// Past the TTL: the group re-probes — security reruns, but the version
	// exec is still skipped because the binary's stat identity is unchanged.
	*now = now.Add(2 * time.Second)
	got := p.collect()
	if got["version"] != "2.1.211" {
		t.Fatalf("version after TTL refresh = %v, want 2.1.211", got["version"])
	}
	if n := runner.calls["security find-generic-password -s Claude Code-credentials"]; n != 2 {
		t.Fatalf("security ran %d times after the TTL, want 2", n)
	}
	if n := runner.calls["/fake/claude --version"]; n != 1 {
		t.Fatalf("--version ran %d times with an unchanged stat identity, want 1", n)
	}
}

func TestClaudeProbe_VersionCacheInvalidatesOnStatChange(t *testing.T) {
	runner := newProbeRunner()
	p, now, stats, _ := newTestProber(runner)
	p.collect()
	// An upgrade swaps the symlink target → new stat identity + new output.
	stats["/fake/claude"] = fakeFileInfo{size: 222, mtime: time.Unix(900, 0)}
	runner.out["/fake/claude --version"] = "2.2.0 (Claude Code)\n"
	*now = now.Add(claudeProbeTTL + time.Second)
	got := p.collect()
	if got["version"] != "2.2.0" {
		t.Fatalf("version after swap = %v, want 2.2.0", got["version"])
	}
	if n := runner.calls["/fake/claude --version"]; n != 2 {
		t.Fatalf("--version ran %d times across an identity change, want 2", n)
	}
}

func TestClaudeProbe_VersionFailSoft(t *testing.T) {
	t.Run("unresolved binary omits version", func(t *testing.T) {
		runner := newProbeRunner()
		p, _, _, _ := newTestProber(runner)
		p.resolveBin = func() string { return "" }
		if got := p.collect(); got["version"] != nil {
			t.Fatalf("version = %v, want absent", got["version"])
		}
	})
	t.Run("exec failure omits version but keeps the cred probes", func(t *testing.T) {
		runner := newProbeRunner()
		p, _, _, _ := newTestProber(runner)
		runner.errs["/fake/claude --version"] = errors.New("boom")
		got := p.collect()
		if _, present := got["version"]; present {
			t.Fatalf("version = %v, want absent on exec failure", got["version"])
		}
		if got["cred_file"] != true || got["keychain"] != true {
			t.Fatalf("cred probes must survive a version failure, got %v", got)
		}
	})
	t.Run("stat failure omits version", func(t *testing.T) {
		runner := newProbeRunner()
		p, _, stats, _ := newTestProber(runner)
		delete(stats, "/fake/claude")
		if got := p.collect(); got["version"] != nil {
			t.Fatalf("version = %v, want absent on stat failure", got["version"])
		}
	})
}

func TestClaudeProbe_CredThreeStates(t *testing.T) {
	t.Run("file absent", func(t *testing.T) {
		runner := newProbeRunner()
		p, _, stats, _ := newTestProber(runner)
		delete(stats, "/home/.claude/.credentials.json")
		got := p.collect()
		if got["cred_file"] != false || got["sub_readable"] != false {
			t.Fatalf("absent file: %v, want cred_file=false sub_readable=false", got)
		}
	})
	t.Run("file present, subscriptionType missing", func(t *testing.T) {
		runner := newProbeRunner()
		p, _, _, files := newTestProber(runner)
		files["/home/.claude/.credentials.json"] = []byte(`{"claudeAiOauth":{"accessToken":"SECRET"}}`)
		got := p.collect()
		if got["cred_file"] != true || got["sub_readable"] != false {
			t.Fatalf("field-less file: %v, want cred_file=true sub_readable=false", got)
		}
	})
	t.Run("file present, subscriptionType readable", func(t *testing.T) {
		runner := newProbeRunner()
		p, _, _, _ := newTestProber(runner)
		got := p.collect()
		if got["cred_file"] != true || got["sub_readable"] != true {
			t.Fatalf("readable file: %v, want cred_file=true sub_readable=true", got)
		}
	})
	t.Run("no HOME omits both cred keys", func(t *testing.T) {
		runner := newProbeRunner()
		p, _, _, _ := newTestProber(runner)
		p.env = func(string) string { return "" }
		got := p.collect()
		if _, present := got["cred_file"]; present {
			t.Fatalf("cred_file must be absent with no HOME, got %v", got)
		}
		if _, present := got["sub_readable"]; present {
			t.Fatalf("sub_readable must be absent with no HOME, got %v", got)
		}
	})
}

func TestClaudeProbe_Keychain(t *testing.T) {
	t.Run("security exit 0 reads present", func(t *testing.T) {
		runner := newProbeRunner()
		p, _, _, _ := newTestProber(runner)
		if got := p.collect(); got["keychain"] != true {
			t.Fatalf("keychain = %v, want true", got["keychain"])
		}
	})
	t.Run("security non-zero exit reads absent", func(t *testing.T) {
		runner := newProbeRunner()
		p, _, _, _ := newTestProber(runner)
		runner.errs["security find-generic-password -s Claude Code-credentials"] =
			errors.New("exit status 44: could not be found")
		if got := p.collect(); got["keychain"] != false {
			t.Fatalf("keychain = %v, want false", got["keychain"])
		}
	})
	t.Run("non-darwin skips the keychain probe entirely", func(t *testing.T) {
		runner := newProbeRunner()
		p, _, _, _ := newTestProber(runner)
		p.goos = "linux"
		got := p.collect()
		if _, present := got["keychain"]; present {
			t.Fatalf("keychain must be absent on non-darwin, got %v", got)
		}
		if n := runner.calls["security find-generic-password -s Claude Code-credentials"]; n != 0 {
			t.Fatalf("security ran %d times on non-darwin, want 0", n)
		}
	})
}

// TestClaudeCredSubscriptionType pins the LITERAL-COPY contract with ocagent's
// claudeSubscriptionType (cli/ocagent/contextreport.go): same miss conditions,
// same trim, and the secret fields never influence the result.
func TestClaudeCredSubscriptionType(t *testing.T) {
	read := func(body string, err error) func(string) ([]byte, error) {
		return func(string) ([]byte, error) { return []byte(body), err }
	}
	if got := claudeCredSubscriptionType(read("", os.ErrNotExist), "p"); got != "" {
		t.Errorf("missing file: %q, want empty", got)
	}
	if got := claudeCredSubscriptionType(read("not json", nil), "p"); got != "" {
		t.Errorf("bad json: %q, want empty", got)
	}
	if got := claudeCredSubscriptionType(read(`{"claudeAiOauth":{}}`, nil), "p"); got != "" {
		t.Errorf("blank field: %q, want empty", got)
	}
	if got := claudeCredSubscriptionType(
		read(`{"claudeAiOauth":{"subscriptionType":" max "}}`, nil), "p"); got != "max" {
		t.Errorf("readable field: %q, want max (trimmed)", got)
	}
}
