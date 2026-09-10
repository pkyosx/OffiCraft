// Skeleton generated from server/ocserverd/config.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultSseContextHigh(t *testing.T) {
	want := SseContextHighConfig{NoticePct: 40, HandoverPct: 50, MinBootSecs: 120, StaleGuard: true}
	if got := defaultSseContextHigh(); got != want {
		t.Fatalf("defaultSseContextHigh() = %#v, want %#v", got, want)
	}
}

func TestDefaultConfig(t *testing.T) {
	want := Config{
		Server: ServerConfig{Port: defaultPort},
		Auth:   AuthConfig{TokenTTL: defaultOwnerTokenTTL},
		SseContextHigh: SseContextHighConfig{
			NoticePct: 40, HandoverPct: 50, MinBootSecs: 120, StaleGuard: true,
		},
	}
	if got := defaultConfig(); !reflect.DeepEqual(got, want) {
		t.Fatalf("defaultConfig() = %#v, want %#v", got, want)
	}
}

func TestConfigPath(t *testing.T) {
	if got := configPath(func(string) string { return "" }); got != "oc.toml" {
		t.Fatalf("configPath(unset) = %q, want oc.toml", got)
	}
	if got := configPath(func(name string) string {
		if name == envConfigPath {
			return "deploy/oc.toml"
		}
		return ""
	}); got != "deploy/oc.toml" {
		t.Fatalf("configPath(configured) = %q, want deploy/oc.toml", got)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	for _, p := range []string{"~", "~/nested/oc.toml"} {
		t.Run(p, func(t *testing.T) {
			if got, want := configPath(func(string) string { return p }), filepath.Clean(home+strings.TrimPrefix(p, "~")); got != want {
				t.Fatalf("configPath(%q) = %q, want %q", p, got, want)
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	t.Run("missing file keeps convention defaults", func(t *testing.T) {
		cfg, warnings, err := loadConfig(filepath.Join(t.TempDir(), "missing.toml"))
		if err != nil {
			t.Fatalf("loadConfig(missing): %v", err)
		}
		if warnings != nil {
			t.Fatalf("warnings = %#v, want nil", warnings)
		}
		if !reflect.DeepEqual(cfg, defaultConfig()) {
			t.Fatalf("cfg = %#v, want defaultConfig()", cfg)
		}
	})

	t.Run("valid file resolves effective fields and reports retired tables", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "oc.toml")
		raw := `[server]
host = "0.0.0.0"
port = 9000
namespace = "demo-1"

[auth]
password = "old-password"
secret = "old-secret"
token_ttl = 123

[storage]
dsn = "sqlite:///configured.db"
database_url = "sqlite:///legacy.db"

[sse_context_high]
warn_pct = 35
notice_pct = 60
handover_pct = 70
remind_step_pct = 5
min_boot_secs = 90.5
stale_guard = false

[extensions]
owned_by_extension = "kept"
`
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		cfg, warnings, err := loadConfig(path)
		if err != nil {
			t.Fatalf("loadConfig: %v", err)
		}
		if cfg.Server != (ServerConfig{Port: 9000, Namespace: "demo-1"}) {
			t.Fatalf("Server = %#v, want configured port and namespace", cfg.Server)
		}
		if cfg.StorageDSN != "sqlite:///configured.db" {
			t.Fatalf("StorageDSN = %q, want configured dsn to win", cfg.StorageDSN)
		}
		if cfg.Auth != (AuthConfig{Password: "old-password", Secret: "old-secret", TokenTTL: 123, TokenTTLSet: true}) {
			t.Fatalf("Auth = %#v, want parsed legacy auth", cfg.Auth)
		}
		if cfg.SseContextHigh != (SseContextHighConfig{NoticePct: 60, HandoverPct: 70, MinBootSecs: 90.5, StaleGuard: false}) {
			t.Fatalf("SseContextHigh = %#v, want parsed legacy context settings", cfg.SseContextHigh)
		}
		if cfg.SseContextHighSet != (SseContextHighSet{NoticePct: true, HandoverPct: true, MinBootSecs: true, StaleGuard: true}) {
			t.Fatalf("SseContextHighSet = %#v, want every retained setting marked", cfg.SseContextHighSet)
		}
		if len(warnings) != 3 {
			t.Fatalf("warnings = %#v, want host/auth/sse warnings", warnings)
		}
		joined := strings.Join(warnings, "\n")
		for _, want := range []string{"[server].host", "[auth]", "[sse_context_high]"} {
			if !strings.Contains(joined, want) {
				t.Fatalf("warnings = %#v, want %q", warnings, want)
			}
		}
	})

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "malformed toml", raw: "[server\nport = 1", want: "parse"},
		{name: "unknown setting", raw: "[server]\nport = 1\nunknown = true", want: "unknown setting"},
		{name: "invalid namespace", raw: "[server]\nnamespace = \"Bad_Name\"", want: "namespace"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "oc.toml")
			if err := os.WriteFile(path, []byte(tt.raw), 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			_, _, err := loadConfig(path)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("loadConfig error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestResolveDSN(t *testing.T) {
	cfg := Config{StorageDSN: "sqlite:///from-config", Server: ServerConfig{Namespace: "tenant-1"}}
	if got := resolveDSN(func(name string) string {
		if name == envDatabaseURL {
			return "postgres://ignored-by-driver"
		}
		return ""
	}, cfg); got != "postgres://ignored-by-driver" {
		t.Fatalf("resolveDSN(env, cfg) = %q, want environment override", got)
	}
	if got := resolveDSN(func(string) string { return "" }, cfg); got != cfg.StorageDSN {
		t.Fatalf("resolveDSN(config, cfg) = %q, want %q", got, cfg.StorageDSN)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := "sqlite:///" + filepath.Join(home, ".officraft-tenant-1", "server", "data", "officraft.db")
	if got := resolveDSN(func(string) string { return "" }, Config{Server: ServerConfig{Namespace: "tenant-1"}}); got != want {
		t.Fatalf("resolveDSN(default, namespaced) = %q, want %q", got, want)
	}
	mainWant := "sqlite:///" + filepath.Join(home, ".officraft", "server", "data", "officraft.db")
	if got := resolveDSN(func(string) string { return "" }, Config{}); got != mainWant {
		t.Fatalf("resolveDSN(default, main) = %q, want %q", got, mainWant)
	}
}

func TestSqliteFilePath(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		path string
		ok   bool
	}{
		{name: "relative sqlite dsn", dsn: "sqlite:///var/data.db", path: "var/data.db", ok: true},
		{name: "absolute sqlite dsn", dsn: "sqlite:////tmp/data.db", path: "/tmp/data.db", ok: true},
		{name: "pysqlite dsn", dsn: "sqlite+pysqlite:///var/data.db", path: "var/data.db", ok: true},
		{name: "bare path", dsn: "/tmp/data.db", path: "/tmp/data.db", ok: true},
		{name: "postgres is unsupported", dsn: "postgres://db/app", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := sqliteFilePath(tt.dsn)
			if got != tt.path || ok != tt.ok {
				t.Fatalf("sqliteFilePath(%q) = (%q, %v), want (%q, %v)", tt.dsn, got, ok, tt.path, tt.ok)
			}
		})
	}
}
