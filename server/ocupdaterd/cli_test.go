package main

// cli_test.go — the local management subcommands, driven end-to-end through
// realMain against a throwaway store (a temp data_dir wired in via
// $OC_UPDATER_CONFIG). The publish-token list/revoke commands are the focus:
// the kind gate must keep them from ever touching invite rows (and vice
// versa), because both credential kinds live in the one generic table.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// newCLIEnv builds the env accessor for one test: a fresh data dir behind a
// minimal oc-updater.toml.
func newCLIEnv(t *testing.T) func(string) string {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "oc-updater.toml")
	cfg := fmt.Sprintf("[storage]\ndata_dir = %q\n", filepath.Join(dir, "data"))
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return func(key string) string {
		if key == envConfigPath {
			return cfgPath
		}
		return ""
	}
}

// runCLI invokes one subcommand and returns (exit code, combined output).
func runCLI(t *testing.T, env func(string) string, args ...string) (int, string) {
	t.Helper()
	var out strings.Builder
	rc := realMain(args, env, &out)
	return rc, out.String()
}

var mintedIDRe = regexp.MustCompile(`\(id (\d+)`)

// mintCredential mints via the real subcommand and returns the new row's id.
func mintCredential(t *testing.T, env func(string) string, args ...string) string {
	t.Helper()
	rc, out := runCLI(t, env, args...)
	if rc != 0 {
		t.Fatalf("%v: rc=%d out=%q", args, rc, out)
	}
	m := mintedIDRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("%v: no id in output %q", args, out)
	}
	return m[1]
}

func TestMintPublishTokenPointsAtListCommand(t *testing.T) {
	env := newCLIEnv(t)
	rc, out := runCLI(t, env, "mint-publish-token", "--name", "ci-bot")
	if rc != 0 {
		t.Fatalf("mint-publish-token: rc=%d out=%q", rc, out)
	}
	if !strings.Contains(out, "ocu-pub-") {
		t.Fatalf("mint output lacks the token: %q", out)
	}
	if !strings.Contains(out, "list-publish-tokens") || !strings.Contains(out, "revoke-publish-token") {
		t.Fatalf("mint output lacks the management hint: %q", out)
	}
}

func TestListPublishTokensShowsOnlyPublishKind(t *testing.T) {
	env := newCLIEnv(t)

	rc, out := runCLI(t, env, "list-publish-tokens")
	if rc != 0 || !strings.Contains(out, "no publish tokens yet") {
		t.Fatalf("empty list: rc=%d out=%q", rc, out)
	}

	mintCredential(t, env, "mint-publish-token", "--name", "pub-one")
	mintCredential(t, env, "mint-invite", "--name", "inv-one")

	rc, out = runCLI(t, env, "list-publish-tokens")
	if rc != 0 {
		t.Fatalf("list-publish-tokens: rc=%d out=%q", rc, out)
	}
	if !strings.Contains(out, "pub-one") {
		t.Fatalf("publish token missing from list: %q", out)
	}
	if strings.Contains(out, "inv-one") {
		t.Fatalf("invite leaked into the publish-token list: %q", out)
	}

	rc, out = runCLI(t, env, "list-invites")
	if rc != 0 || !strings.Contains(out, "inv-one") || strings.Contains(out, "pub-one") {
		t.Fatalf("list-invites cross-kind check: rc=%d out=%q", rc, out)
	}
}

func TestRevokePublishTokenRevokesOnlyPublishKind(t *testing.T) {
	env := newCLIEnv(t)
	pubID := mintCredential(t, env, "mint-publish-token", "--name", "pub-gone")

	rc, out := runCLI(t, env, "revoke-publish-token", pubID)
	if rc != 0 || !strings.Contains(out, "publish token "+pubID+" revoked") {
		t.Fatalf("revoke-publish-token: rc=%d out=%q", rc, out)
	}

	rc, out = runCLI(t, env, "list-publish-tokens")
	if rc != 0 || !strings.Contains(out, "revoked") {
		t.Fatalf("revoked token not marked in list: rc=%d out=%q", rc, out)
	}

	rc, out = runCLI(t, env, "revoke-publish-token", pubID)
	if rc != 1 || !strings.Contains(out, "no live publish token has id "+pubID) {
		t.Fatalf("double revoke must refuse: rc=%d out=%q", rc, out)
	}

	rc, out = runCLI(t, env, "revoke-publish-token", "not-a-number")
	if rc != 2 || !strings.Contains(out, "not a publish token id") {
		t.Fatalf("non-numeric id must refuse: rc=%d out=%q", rc, out)
	}
}

func TestRevokePublishTokenRefusesInviteID(t *testing.T) {
	env := newCLIEnv(t)
	invID := mintCredential(t, env, "mint-invite", "--name", "inv-alive")

	rc, out := runCLI(t, env, "revoke-publish-token", invID)
	if rc != 1 || !strings.Contains(out, "no live publish token has id "+invID) {
		t.Fatalf("kind gate must refuse an invite id: rc=%d out=%q", rc, out)
	}

	rc, out = runCLI(t, env, "list-invites")
	if rc != 0 || strings.Contains(out, "revoked") || !strings.Contains(out, "inv-alive") {
		t.Fatalf("invite must survive a cross-kind revoke: rc=%d out=%q", rc, out)
	}
}

func TestRevokeInviteRefusesPublishTokenID(t *testing.T) {
	env := newCLIEnv(t)
	pubID := mintCredential(t, env, "mint-publish-token", "--name", "pub-alive")

	rc, out := runCLI(t, env, "revoke-invite", pubID)
	if rc != 1 || !strings.Contains(out, "no live invite has id "+pubID) {
		t.Fatalf("kind gate must refuse a publish-token id: rc=%d out=%q", rc, out)
	}

	rc, out = runCLI(t, env, "list-publish-tokens")
	if rc != 0 || strings.Contains(out, "revoked") || !strings.Contains(out, "pub-alive") {
		t.Fatalf("publish token must survive a cross-kind revoke: rc=%d out=%q", rc, out)
	}
}
