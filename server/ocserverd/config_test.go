package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadConfigMissingFileYieldsDefaults(t *testing.T) {
	cfg, warnings, err := loadConfig(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatalf("missing file must not error: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("missing file must not warn: %v", warnings)
	}
	if cfg.Server.Port != 8770 || cfg.Server.Namespace != "" {
		t.Fatalf("server defaults: %+v", cfg.Server)
	}
	if cfg.Auth.Password != "" || cfg.Auth.Secret != "" || cfg.Auth.TokenTTL != 86400 || cfg.Auth.TokenTTLSet {
		t.Fatalf("auth defaults (deny-by-default empty password): %+v", cfg.Auth)
	}
	if cfg.StorageDSN != "" {
		t.Fatalf("storage default is unset: %+v", cfg)
	}
	if cfg.SseContextHigh != defaultSseContextHigh() || cfg.SseContextHighSet != (SseContextHighSet{}) {
		t.Fatalf("sse_context_high defaults: %+v", cfg.SseContextHigh)
	}
}

func TestLoadConfigReadsAllTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oc.toml")
	body := `
[server]
host = "0.0.0.0"
port = 8796

[auth]
password = "correct-horse"
secret = "unit-test-signing-secret"
token_ttl = 3600

[storage]
dsn = "sqlite:///var/data/test.db"

[sse_context_high]
warn_pct = 45
stale_guard = false
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, warnings, err := loadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Server.Port != 8796 {
		t.Fatalf("server: %+v", cfg.Server)
	}
	// Retired keys are still PARSED (the one-shot DB migration consumes them)
	// but each retired table draws exactly one warning.
	if cfg.Auth.Password != "correct-horse" || cfg.Auth.Secret != "unit-test-signing-secret" || cfg.Auth.TokenTTL != 3600 || !cfg.Auth.TokenTTLSet {
		t.Fatalf("auth: %+v", cfg.Auth)
	}
	if len(warnings) != 3 {
		t.Fatalf("want 3 retired-key warnings (host/auth/sse_context_high): %v", warnings)
	}
	for i, frag := range []string{"[server].host", "[auth]", "[sse_context_high]"} {
		if !strings.Contains(warnings[i], frag) {
			t.Fatalf("warning %d must name %s: %q", i, frag, warnings[i])
		}
	}
	if cfg.StorageDSN != "sqlite:///var/data/test.db" {
		t.Fatalf("storage: %q", cfg.StorageDSN)
	}
	// Set keys land (with their Set flags — the migration imports exactly
	// those); absent keys keep the non-zero convention defaults.
	want := defaultSseContextHigh()
	want.WarnPct = 45
	want.StaleGuard = false
	if cfg.SseContextHigh != want {
		t.Fatalf("sse_context_high: %+v", cfg.SseContextHigh)
	}
	if cfg.SseContextHighSet != (SseContextHighSet{WarnPct: true, StaleGuard: true}) {
		t.Fatalf("sse_context_high set flags: %+v", cfg.SseContextHighSet)
	}
}

func TestLoadConfigPartialFileKeepsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oc.toml")
	if err := os.WriteFile(path, []byte("[auth]\npassword = \"pw\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, warnings, err := loadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Server.Port != 8770 || cfg.Auth.TokenTTL != 86400 || cfg.Auth.TokenTTLSet || cfg.Auth.Password != "pw" {
		t.Fatalf("absent keys must keep convention defaults: %+v", cfg)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "[auth]") {
		t.Fatalf("a lone [auth] table must draw exactly its own warning: %v", warnings)
	}
}

func TestLoadConfigEffectiveSchemaDrawsNoWarnings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oc.toml")
	body := "[server]\nport = 8796\nnamespace = \"seth\"\n\n[storage]\ndsn = \"sqlite:///x.db\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, warnings, err := loadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("the port+dsn+namespace schema must not warn: %v", warnings)
	}
}

func TestLoadConfigNamespace(t *testing.T) {
	write := func(t *testing.T, body string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "oc.toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	// Absent → the main instance ("" — no namespace key exists today, so this
	// is the zero-diff default).
	cfg, _, err := loadConfig(write(t, "[server]\nport = 8770\n"))
	if err != nil || cfg.Server.Namespace != "" {
		t.Fatalf("absent namespace must stay empty: %+v, %v", cfg.Server, err)
	}
	// Present + valid → read through.
	cfg, _, err = loadConfig(write(t, "[server]\nport = 8771\nnamespace = \"seth\"\n"))
	if err != nil || cfg.Server.Namespace != "seth" || cfg.Server.Port != 8771 {
		t.Fatalf("namespace/port not read: %+v, %v", cfg.Server, err)
	}
	// Present + malformed → fail LOUD (never silently fold back to the main
	// instance — that would cross-wire two instances' wardens).
	for _, bad := range []string{"Seth", "s.eth", "s eth", "seventeen-chars-xx"} {
		if _, _, err := loadConfig(write(t, "[server]\nnamespace = \""+bad+"\"\n")); err == nil {
			t.Errorf("namespace %q must fail loud", bad)
		}
	}
}

func TestLoadConfigMalformedFileFailsLoud(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oc.toml")
	if err := os.WriteFile(path, []byte("[storage]\n[storage]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadConfig(path); err == nil {
		t.Fatal("a duplicate [storage] table must fail loud (tomllib parity)")
	}
}

func TestConfigPathEnvOverride(t *testing.T) {
	// $OC_CONFIG wins (the out-of-repo canonical path); unset → ./oc.toml.
	if got := configPath(envOf(map[string]string{"OC_CONFIG": "/etc/oc.toml"})); got != "/etc/oc.toml" {
		t.Fatalf("OC_CONFIG must win: %q", got)
	}
	if got := configPath(envOf(nil)); got != "oc.toml" {
		t.Fatalf("unset OC_CONFIG → convention default: %q", got)
	}
}

func TestResolveDSNOrder(t *testing.T) {
	// env → oc.toml [storage].dsn → the ABSOLUTE convention default under the
	// instance's canonical root (never CWD-relative — B2).
	env := envOf(map[string]string{"OC_DATABASE_URL": "sqlite:///env.db"})
	if got := resolveDSN(env, Config{StorageDSN: "sqlite:///toml.db"}); got != "sqlite:///env.db" {
		t.Fatalf("env must win: %q", got)
	}
	if got := resolveDSN(envOf(nil), Config{StorageDSN: "sqlite:///toml.db"}); got != "sqlite:///toml.db" {
		t.Fatalf("oc.toml next: %q", got)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := "sqlite:///" + filepath.Join(home, ".officraft", "server", "data", "officraft.db")
	if got := resolveDSN(envOf(nil), Config{}); got != want {
		t.Fatalf("convention default: %q != %q", got, want)
	}
	// A namespaced instance defaults under ITS root — two instances must never
	// silently share the main DB.
	wantNS := "sqlite:///" + filepath.Join(home, ".officraft-seth", "server", "data", "officraft.db")
	if got := resolveDSN(envOf(nil), Config{Server: ServerConfig{Namespace: "seth"}}); got != wantNS {
		t.Fatalf("namespaced convention default: %q != %q", got, wantNS)
	}
}

func TestSqliteFilePath(t *testing.T) {
	cases := []struct {
		dsn  string
		path string
		ok   bool
	}{
		{"sqlite:///var/data/x.db", "var/data/x.db", true},       // relative (3 slashes)
		{"sqlite:////abs/x.db", "/abs/x.db", true},               // absolute (4 slashes)
		{"sqlite+pysqlite:///var/x.db", "var/x.db", true},        // SQLAlchemy driver suffix
		{"plain/path.db", "plain/path.db", true},                 // bare path
		{"postgresql+psycopg://u:p@h:5432/officraft", "", false}, // not sqlite
	}
	for _, c := range cases {
		got, ok := sqliteFilePath(c.dsn)
		if ok != c.ok || got != c.path {
			t.Fatalf("sqliteFilePath(%q) = (%q,%v), want (%q,%v)", c.dsn, got, ok, c.path, c.ok)
		}
	}
}
