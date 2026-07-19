package main

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// makeJWT forges an UNSIGNED-payload JWT (header.payload.sig) whose payload
// carries the given claims — enough to exercise jwtSub, which never verifies.
func makeJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	return "h." + payload + ".s"
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg := loadConfig(func(string) string { return "" })
	if cfg.Base != defaultBase {
		t.Errorf("Base = %q, want %q", cfg.Base, defaultBase)
	}
	if cfg.Token != "" || cfg.ID != "" {
		t.Errorf("Token/ID should be empty on a bare env, got %q/%q", cfg.Token, cfg.ID)
	}
}

func TestLoadConfigTrimsBaseAndDerivesID(t *testing.T) {
	tok := makeJWT(t, map[string]any{"sub": "kyle"})
	env := map[string]string{
		"OC_BASE":  "https://oc.example.com/",
		"OC_TOKEN": tok,
	}
	cfg := loadConfig(func(k string) string { return env[k] })
	if cfg.Base != "https://oc.example.com" {
		t.Errorf("Base trailing slash not trimmed: %q", cfg.Base)
	}
	if cfg.ID != "kyle" {
		t.Errorf("ID should derive from JWT sub, got %q", cfg.ID)
	}
}

func TestLoadConfigExplicitIDWins(t *testing.T) {
	tok := makeJWT(t, map[string]any{"sub": "kyle"})
	env := map[string]string{"OC_TOKEN": tok, "OC_ID": "explicit"}
	cfg := loadConfig(func(k string) string { return env[k] })
	if cfg.ID != "explicit" {
		t.Errorf("explicit OC_ID should win, got %q", cfg.ID)
	}
}

func TestJWTSubMalformed(t *testing.T) {
	for _, tok := range []string{"", "not-a-jwt", "a.b", "a.b.c.d", "h..s"} {
		if got := jwtSub(tok); got != "" {
			t.Errorf("jwtSub(%q) = %q, want empty", tok, got)
		}
	}
}

func TestUsageListsAllPlaneA(t *testing.T) {
	var b strings.Builder
	usage(&b)
	for _, s := range planeASubcommands {
		if !strings.Contains(b.String(), s.name) {
			t.Errorf("usage missing subcommand %q", s.name)
		}
	}
}

func TestRealMainListenMisWireExitsZero(t *testing.T) {
	// listen is now implemented (Phase 4). With no OC_ID/OC_TOKEN it degrades to one
	// quiet line + exit 0 (the mis-wire guard, mirroring cmd_listen) — never the old
	// "not implemented" stub. The full SSE behaviour is covered in listen_test.go.
	env := func(string) string { return "" }
	var b strings.Builder
	if rc := realMain([]string{"listen"}, env, strings.NewReader(""), &b); rc != 0 {
		t.Errorf("listen mis-wire rc = %d, want 0", rc)
	}
	if !strings.Contains(b.String(), "no OC_ID/OC_TOKEN") {
		t.Errorf("listen mis-wire should print the guard line, got %q", b.String())
	}
}

func TestRealMainNoArgs(t *testing.T) {
	var b strings.Builder
	if rc := realMain(nil, func(string) string { return "" }, strings.NewReader(""), &b); rc != 2 {
		t.Errorf("no-args rc = %d, want 2", rc)
	}
	if !strings.Contains(b.String(), "usage:") {
		t.Errorf("no-args should print usage, got %q", b.String())
	}
}
