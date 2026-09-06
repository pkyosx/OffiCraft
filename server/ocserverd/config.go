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
	defaultHost = "127.0.0.1"
	// defaultPort is the OffiCraft standard port: 7755.
	// History (both hops were the same class of collision fix): 8770 was a
	// migration leftover — the retired open-company station's port — so a
	// transition-period first install on the same machine collided with the
	// still-serving old station; t-dc68 moved the default to 8780, and this
	// change moves it again to 7755. Existing installs pin their port in
	// oc.toml (bin/ocserver renders it explicitly) and are unaffected; only a
	// config-less `ocserverd serve` moves.
	defaultPort = 7755
	// ⚠️ THESE ARE DEFAULTS, NOT WHAT A RUNNING STATION USES. Both comments below
	// describe the CONSTANT and are accurate about it — which is exactly why they
	// mislead: a reader who takes "24h" as the answer will be wrong on any station
	// where the DB says otherwise, and nothing here says to go look.
	//
	// The DB `setting` table wins: loadSettings (settings.go) reads
	// `auth.owner_token_ttl` / `auth.agent_token_ttl` and only falls back to these
	// when neither is stored. On a station upgraded from before those two keys were
	// split apart, the successor key is written ONCE from the pre-split shared key
	// `auth.token_ttl` — so an inherited value can be sitting there without anyone
	// having chosen it, and from here that is indistinguishable from the default.
	//
	// ⇒ To learn what a station actually enforces, READ ITS DB. Do not read this
	// line. Deliberately no current value is quoted here: a number in a comment
	// goes stale silently, and looking it up is cheap.
	defaultOwnerTokenTTL = 86400  // owner login JWT lifetime: 24h
	defaultAgentTokenTTL = 604800 // agent/worker JWT lifetime: 7d
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

// SseContextHighConfig is the server-side context band config. HandoverPct <= 0
// disables the band entirely (reversible kill-switch).
//
// warn_pct and remind_step_pct USED to live here and are gone (T-c382). They
// were a SECOND threshold beside HandoverPct, hard-wired at 40 and 5 with no UI
// on either, so an owner who moved the handover threshold to 65% did not move
// the reminder — it kept firing from 40%, every 5%, five times before the event
// it was warning about. The advance notice is now DERIVED from HandoverPct (see
// handoverNoticeLeadPct in sse_bands.go), which is the only threshold the owner
// can actually see and set. Do not reintroduce a standalone one: two thresholds
// for one decision is how this drifted in the first place.
// T-a9d6 turned the single threshold into a PAIR, which is not the same shape
// as the warn_pct that was removed above. NoticePct is the FIRST, soft notice;
// HandoverPct is the SECOND, final one — and the final one is still the single
// point the handover decision reads, so the two can never disagree about when a
// session ends. Owner 2026-08-16, verbatim: 「以 claude 來說我們允許設定第二個
// 數字 65% / 75% 表示第一次通知會是 65% 第二次通知會是 75%」.
// What made warn_pct wrong was that it did NOT track the threshold the owner
// could see; this pair is set together in one UI, validated against each other
// (notice must sit below final), and neither is derived behind his back.
type SseContextHighConfig struct {
	NoticePct   int
	HandoverPct int
	MinBootSecs float64
	StaleGuard  bool
}

// defaultSseContextHigh: NOTICE=40, HANDOVER=50, 120s boot-storm guard, stale
// guard on. The 10-point default gap is the lead T-c382 derived; T-a9d6 keeps
// the same shipped behaviour while making the gap the owner's to set.
func defaultSseContextHigh() SseContextHighConfig {
	return SseContextHighConfig{
		NoticePct:   40,
		HandoverPct: 50,
		MinBootSecs: 120.0,
		StaleGuard:  true,
	}
}

// SseContextHighSet records which RETIRED [sse_context_high] knobs the file
// wrote explicitly — the one-shot ctx.* DB migration (settings.go) imports
// exactly those, so an old file's tuned knobs survive the key retirement.
// warn_pct / remind_step_pct dropped out with the knobs themselves (T-c382):
// an old file may still carry them and is still parsed without error, but there
// is no longer anything for the migration to import them INTO.
type SseContextHighSet struct {
	NoticePct   bool
	HandoverPct bool
	MinBootSecs bool
	StaleGuard  bool
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

// tomlFile is the on-disk oc.toml shape. Settings outside this schema are
// rejected so a typo cannot silently start the server with a default. The one
// deliberate escape hatch is [extensions], which reserves a namespaced area
// for configuration owned by extensions rather than ocserverd itself.
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
		NoticePct     *int     `toml:"notice_pct"`
		HandoverPct   *int     `toml:"handover_pct"`
		RemindStepPct *int     `toml:"remind_step_pct"`
		MinBootSecs   *float64 `toml:"min_boot_secs"`
		StaleGuard    *bool    `toml:"stale_guard"`
	} `toml:"sse_context_high"`
	Extensions extensionConfig `toml:"extensions"`
}

// extensionConfig deliberately owns every key below [extensions] without
// interpreting it. Implementing toml.Unmarshaler makes the decoder mark that
// whole subtree as consumed while keeping unknown keys elsewhere fail-closed.
type extensionConfig struct{}

func (extensionConfig) UnmarshalTOML(value any) error {
	if _, ok := value.(map[string]any); !ok {
		return fmt.Errorf("[extensions] must be a table")
	}
	return nil
}

func defaultConfig() Config {
	return Config{
		Server:         ServerConfig{Port: defaultPort},
		Auth:           AuthConfig{TokenTTL: defaultOwnerTokenTTL},
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
	md, err := toml.Decode(string(raw), &f)
	if err != nil {
		return cfg, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if unknown := md.Undecoded(); len(unknown) > 0 {
		keys := make([]string, 0, len(unknown))
		for _, key := range unknown {
			keys = append(keys, key.String())
		}
		return cfg, nil, fmt.Errorf("parse %s: unknown setting(s): %s; only [extensions] may contain extension-owned settings", path, strings.Join(keys, ", "))
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
	cfg.Auth = AuthConfig{Password: f.Auth.Password, Secret: f.Auth.Secret, TokenTTL: defaultOwnerTokenTTL}
	if f.Auth.TokenTTL != nil {
		cfg.Auth.TokenTTL = *f.Auth.TokenTTL
		cfg.Auth.TokenTTLSet = true
	}
	if f.Storage.DSN != "" {
		cfg.StorageDSN = f.Storage.DSN
	} else {
		cfg.StorageDSN = f.Storage.DatabaseURL
	}
	// warn_pct / remind_step_pct are still PARSED (an old file must not become
	// fatal) but no longer land anywhere: the knobs they fed were removed in
	// T-c382 and the advance notice is derived from handover_pct.
	if f.SseContextHigh.NoticePct != nil {
		cfg.SseContextHigh.NoticePct = *f.SseContextHigh.NoticePct
		cfg.SseContextHighSet.NoticePct = true
	}
	if f.SseContextHigh.HandoverPct != nil {
		cfg.SseContextHigh.HandoverPct = *f.SseContextHigh.HandoverPct
		cfg.SseContextHighSet.HandoverPct = true
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
