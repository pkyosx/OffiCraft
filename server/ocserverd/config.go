package main

// config.go — the single repo-root config (oc.toml), shrunk to advanced
// overrides only (owner-password-in-db design, B2): the effective schema is
// [server].port, [server].namespace and [storage].dsn — everything auth- and
// knob-shaped lives in the DB settings table (settings.go). The file may be
// entirely absent: every key has a convention default.
//
// Resolution order:
//   * config file: $OC_CONFIG (out-of-repo canonical, survives a re-clone) →
//     ./oc.toml (CWD-relative — a static binary has no source path to anchor;
//     $OC_CONFIG is the canonical deployment path anyway).
//   * DSN: $OC_DATABASE_URL → oc.toml [storage].dsn (or the legacy
//     database_url key) → the ABSOLUTE convention default
//     ~/.officraft{-<ns>}/server/data/officraft.db (never CWD-relative —
//     running from a different directory must not grow a second database).
//
// RETIRED keys ([auth].*, [server].host, [sse_context_high].*) are warned
// about and ignored at runtime — the DB is the only read path. They are still
// PARSED (never fatal: existing installs keep their file) because the one-shot
// oc.toml → DB migration (settings.go loadAuthSettings) consumes them on the
// first boot of an install that predates the settings table.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	// envConfigPath mirrors service.config.ENV_CONFIG_PATH / dal.engine (the
	// constant VALUE is "OC_CONFIG" on both Python sides).
	envConfigPath  = "OC_CONFIG"
	envDatabaseURL = "OC_DATABASE_URL" // dal.engine.ENV_DATABASE_URL

	// defaultHost is HARDWIRED (B2): the security model is loopback-bind only,
	// exposure goes through a tunnel. The retired [server].host key is ignored.
	defaultHost     = "127.0.0.1"
	defaultPort     = 8770  // FIXED canonical port (Python DEFAULT_PORT)
	defaultTokenTTL = 86400 // owner JWT lifetime: 24h (DEFAULT_TOKEN_TTL)
)

// ServerConfig is the effective [server] table: port plus the same-machine
// multi-instance namespace ("" = the main instance). A non-empty namespace is
// stamped by bin/ocserver install --namespace and rides OUT of the server on
// exactly two lines: the install.sh line and the bootstrap/teardown-here env
// (OC_NAMESPACE) — that is the whole cross-plane propagation. The bind host is
// hardwired to loopback (defaultHost), not configurable.
type ServerConfig struct {
	Port      int
	Namespace string
}

// namespaceShape locks the namespace charset to the strict intersection of
// launchd-label / path-component / tmux-socket syntax (same lock as
// cli/ocwarden and bin/ocserver — keep the three in sync).
var namespaceShape = regexp.MustCompile(`^[a-z0-9-]{1,16}$`)

// AuthConfig carries the RETIRED oc.toml [auth] table (password/secret/ttl).
// Runtime never reads these: their ONLY consumer is the one-shot oc.toml → DB
// settings migration (settings.go loadAuthSettings) for installs that predate
// the settings table. TokenTTLSet distinguishes an explicitly written
// token_ttl (migrated into the DB) from the convention default (left
// unwritten).
type AuthConfig struct {
	Password    string
	Secret      string
	TokenTTL    int
	TokenTTLSet bool
}

// SseContextHighConfig mirrors service.config.SseContextHighConfig — the
// [sse_context_high] knobs for the server-side context band push. A threshold
// <= 0 disables that band entirely (reversible kill-switch).
type SseContextHighConfig struct {
	WarnPct     int
	HandoverPct int
	// RemindStepPct is the width (in gauge %) of one remind bucket: a same-band
	// WARN re-reminds only when the gauge climbs a full step into a new higher
	// bucket (T-7826 dedup, replacing the old per-tick cooldown that bombarded
	// the agent). <= 0 falls back to 1% buckets.
	RemindStepPct int
	MinBootSecs   float64
	StaleGuard    bool
}

// defaultSseContextHigh mirrors the Python dataclass defaults (WARN=40,
// HANDOVER=50, 5% remind buckets, 120s boot-storm guard, stale guard on).
func defaultSseContextHigh() SseContextHighConfig {
	return SseContextHighConfig{
		WarnPct:       40,
		HandoverPct:   50,
		RemindStepPct: 5,
		MinBootSecs:   120.0,
		StaleGuard:    true,
	}
}

// SseContextHighSet records which RETIRED [sse_context_high] knobs the file
// wrote explicitly — the one-shot ctx.* DB migration (settings.go) imports
// exactly those, so an old file's tuned knobs survive the key retirement.
type SseContextHighSet struct {
	WarnPct       bool
	HandoverPct   bool
	RemindStepPct bool
	MinBootSecs   bool
	StaleGuard    bool
}

// Config is the fully resolved oc.toml. The EFFECTIVE schema is Server
// (port/namespace) + StorageDSN; Auth and SseContextHigh carry retired keys
// for the one-shot DB migration only. StorageDSN is the RAW [storage].dsn
// value ("" when unset); resolveDSN applies the env override + convention
// default.
type Config struct {
	Server            ServerConfig
	Auth              AuthConfig
	StorageDSN        string
	SseContextHigh    SseContextHighConfig
	SseContextHighSet SseContextHighSet
}

// tomlFile is the on-disk oc.toml shape (unknown keys/tables are ignored, so
// the file can carry other sections — [sse_context_high] etc. — without
// breaking this reader, same as the Python tomllib readers).
type tomlFile struct {
	Server struct {
		Host      string `toml:"host"`
		Port      int    `toml:"port"`
		Namespace string `toml:"namespace"`
	} `toml:"server"`
	Auth struct {
		Password string `toml:"password"`
		Secret   string `toml:"secret"`
		// Pointer: an absent token_ttl keeps the convention default AND is
		// distinguishable from an explicit value (AuthConfig.TokenTTLSet).
		TokenTTL *int `toml:"token_ttl"`
	} `toml:"auth"`
	Storage struct {
		DSN string `toml:"dsn"`
		// Legacy alias dal.engine also honours (dsn wins when both are set).
		DatabaseURL string `toml:"database_url"`
	} `toml:"storage"`
	// Pointer fields: an ABSENT key must keep its non-zero convention default
	// (e.g. stale_guard true), which a plain field could not distinguish.
	SseContextHigh struct {
		WarnPct       *int     `toml:"warn_pct"`
		HandoverPct   *int     `toml:"handover_pct"`
		RemindStepPct *int     `toml:"remind_step_pct"`
		MinBootSecs   *float64 `toml:"min_boot_secs"`
		StaleGuard    *bool    `toml:"stale_guard"`
	} `toml:"sse_context_high"`
}

func defaultConfig() Config {
	return Config{
		Server:         ServerConfig{Port: defaultPort},
		Auth:           AuthConfig{TokenTTL: defaultTokenTTL},
		SseContextHigh: defaultSseContextHigh(),
	}
}

// configPath resolves the oc.toml location: $OC_CONFIG (when set non-empty)
// wins, else the CWD-relative convention default (see the module comment).
func configPath(env func(string) string) string {
	if p := env(envConfigPath); p != "" {
		if strings.HasPrefix(p, "~"+string(filepath.Separator)) || p == "~" {
			if home, err := os.UserHomeDir(); err == nil {
				return filepath.Join(home, strings.TrimPrefix(p[1:], string(filepath.Separator)))
			}
		}
		return p
	}
	return "oc.toml"
}

// loadConfig reads oc.toml at path. A missing file yields convention defaults
// (never an error); a MALFORMED file is an error (fail loud — a half-read
// config must not silently boot with defaults). warnings carries one line per
// RETIRED table/key the file still writes ([auth], [server].host,
// [sse_context_high]) — the caller prints them; the values are ignored at
// runtime (the one-shot DB migration is their only consumer).
func loadConfig(path string) (Config, []string, error) {
	cfg := defaultConfig()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil, nil
		}
		return cfg, nil, fmt.Errorf("read %s: %w", path, err)
	}
	var f tomlFile
	// Preload the default so an absent port keeps its convention value.
	f.Server.Port = cfg.Server.Port
	if err := toml.Unmarshal(raw, &f); err != nil {
		return cfg, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	// A malformed namespace must fail LOUD: silently folding back to the main
	// instance would cross-wire two instances' wardens/paths.
	if f.Server.Namespace != "" && !namespaceShape.MatchString(f.Server.Namespace) {
		return cfg, nil, fmt.Errorf("parse %s: [server].namespace must match [a-z0-9-]{1,16}, got %q", path, f.Server.Namespace)
	}
	var warnings []string
	if f.Server.Host != "" {
		warnings = append(warnings, "[server].host is retired and ignored — the server always binds "+defaultHost+" (expose via a tunnel); remove the key from "+path)
	}
	cfg.Server = ServerConfig{Port: f.Server.Port, Namespace: f.Server.Namespace}
	if f.Auth.Password != "" || f.Auth.Secret != "" || f.Auth.TokenTTL != nil {
		warnings = append(warnings, "[auth] is retired and ignored — credentials live in the DB settings table (one-shot migrated on first boot); remove the section from "+path)
	}
	cfg.Auth = AuthConfig{Password: f.Auth.Password, Secret: f.Auth.Secret, TokenTTL: defaultTokenTTL}
	if f.Auth.TokenTTL != nil {
		cfg.Auth.TokenTTL = *f.Auth.TokenTTL
		cfg.Auth.TokenTTLSet = true
	}
	if f.Storage.DSN != "" {
		cfg.StorageDSN = f.Storage.DSN
	} else {
		cfg.StorageDSN = f.Storage.DatabaseURL
	}
	if f.SseContextHigh.WarnPct != nil {
		cfg.SseContextHigh.WarnPct = *f.SseContextHigh.WarnPct
		cfg.SseContextHighSet.WarnPct = true
	}
	if f.SseContextHigh.HandoverPct != nil {
		cfg.SseContextHigh.HandoverPct = *f.SseContextHigh.HandoverPct
		cfg.SseContextHighSet.HandoverPct = true
	}
	if f.SseContextHigh.RemindStepPct != nil {
		cfg.SseContextHigh.RemindStepPct = *f.SseContextHigh.RemindStepPct
		cfg.SseContextHighSet.RemindStepPct = true
	}
	if f.SseContextHigh.MinBootSecs != nil {
		cfg.SseContextHigh.MinBootSecs = *f.SseContextHigh.MinBootSecs
		cfg.SseContextHighSet.MinBootSecs = true
	}
	if f.SseContextHigh.StaleGuard != nil {
		cfg.SseContextHigh.StaleGuard = *f.SseContextHigh.StaleGuard
		cfg.SseContextHighSet.StaleGuard = true
	}
	if cfg.SseContextHighSet != (SseContextHighSet{}) {
		warnings = append(warnings, "[sse_context_high] is retired and ignored — knobs live in the DB settings table (ctx.*, one-shot migrated on first boot); remove the section from "+path)
	}
	return cfg, warnings, nil
}

// resolveDSN applies the dal.engine resolution order: $OC_DATABASE_URL
// → oc.toml [storage].dsn → the ABSOLUTE convention default under the
// instance's canonical root (~/.officraft{-<ns>}/server/data). The default
// deliberately stopped being CWD-relative in B2: launching from a different
// directory must not silently grow a second database. The legacy relative path
// remains only as the no-home fallback (never expected in practice).
func resolveDSN(env func(string) string, cfg Config) string {
	if v := env(envDatabaseURL); v != "" {
		return v
	}
	if cfg.StorageDSN != "" {
		return cfg.StorageDSN
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "sqlite:///" + filepath.Join("var", "data", "officraft.db")
	}
	root := ".officraft"
	if cfg.Server.Namespace != "" {
		root += "-" + cfg.Server.Namespace
	}
	return "sqlite:///" + filepath.Join(home, root, "server", "data", "officraft.db")
}

// sqliteFilePath maps a SQLAlchemy-style SQLite DSN ("sqlite:///path",
// "sqlite+pysqlite:///path", or a bare filesystem path) onto the file path the
// modernc.org/sqlite driver opens. Non-SQLite DSNs (postgres etc.) return
// ok=false — the Go migrate plumbing is sqlite-only for now (M3 decides the
// postgres driver story).
func sqliteFilePath(dsn string) (string, bool) {
	scheme, rest, found := strings.Cut(dsn, "://")
	if !found {
		return dsn, true // a bare path — already a file
	}
	if scheme != "sqlite" && !strings.HasPrefix(scheme, "sqlite+") {
		return "", false
	}
	// SQLAlchemy: sqlite:///relative, sqlite:////absolute — after "://" one more
	// leading "/" separates authority from path.
	return strings.TrimPrefix(rest, "/"), true
}
