package main

// config.go — oc.toml, advanced overrides only: [server].port,
// [server].namespace and [storage].dsn. Everything auth- and knob-shaped
// lives in the DB settings table (settings.go); the file may be absent.
//
// RETIRED keys ([auth].*, [server].host, [sse_context_high].*) are warned
// about and ignored at runtime, but still PARSED (never fatal): the one-shot
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
	envConfigPath  = "OC_CONFIG"
	envDatabaseURL = "OC_DATABASE_URL"

	// defaultHost is HARDWIRED: the security model is loopback-bind only;
	// exposure goes through a tunnel.
	defaultHost = "127.0.0.1"
	// History: the default moved 8770 → 8780 → 7755.
	// Existing installs pin their port in oc.toml (bin/ocserver renders it),
	// so changing defaultPort moves only a config-less `ocserverd serve`.
	defaultPort = 7755
	// These TTLs are FALLBACKS, not what a station enforces: loadSettings
	// (settings.go) prefers auth.owner_token_ttl / auth.agent_token_ttl from
	// the DB. Read the DB to learn a station's value.
	defaultOwnerTokenTTL = 86400
	defaultAgentTokenTTL = 604800
)

// Namespace ("" = the main instance) is stamped by bin/ocserver install
// --namespace and leaves the server on exactly two lines: the install.sh line
// and the bootstrap/teardown-here env (OC_NAMESPACE).
type ServerConfig struct {
	Port      int
	Namespace string
}

// namespaceShape: the same charset lock as cli/ocwarden and bin/ocserver
// (launchd label / path component / tmux socket) — keep the three in sync.
var namespaceShape = regexp.MustCompile(`^[a-z0-9-]{1,16}$`)

type AuthConfig struct {
	Password    string
	Secret      string
	TokenTTL    int
	TokenTTLSet bool
}

// NoticePct (the soft first notice) and HandoverPct (the final one, the only
// point the handover decision reads) are an owner-set pair (owner
// 2026-08-16), validated notice < handover. HandoverPct <= 0 disables the
// band. Do not reintroduce a standalone threshold beside them (the retired
// warn_pct drifted exactly that way).
type SseContextHighConfig struct {
	NoticePct   int
	HandoverPct int
	MinBootSecs float64
	StaleGuard  bool
}

func defaultSseContextHigh() SseContextHighConfig {
	return SseContextHighConfig{
		NoticePct:   40,
		HandoverPct: 50,
		MinBootSecs: 120.0,
		StaleGuard:  true,
	}
}

// SseContextHighSet records which retired knobs the file wrote explicitly;
// the one-shot ctx.* DB migration (settings.go) imports exactly those.
type SseContextHighSet struct {
	NoticePct   bool
	HandoverPct bool
	MinBootSecs bool
	StaleGuard  bool
}

type Config struct {
	Server            ServerConfig
	Auth              AuthConfig
	StorageDSN        string
	SseContextHigh    SseContextHighConfig
	SseContextHighSet SseContextHighSet
}

type tomlFile struct {
	Server struct {
		Host      string `toml:"host"`
		Port      int    `toml:"port"`
		Namespace string `toml:"namespace"`
	} `toml:"server"`
	Auth struct {
		Password string `toml:"password"`
		Secret   string `toml:"secret"`

		TokenTTL *int `toml:"token_ttl"`
	} `toml:"auth"`
	Storage struct {
		DSN string `toml:"dsn"`

		DatabaseURL string `toml:"database_url"`
	} `toml:"storage"`
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

// Implementing toml.Unmarshaler makes the decoder mark the whole [extensions]
// subtree as consumed, while unknown keys elsewhere still fail.
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
	// Fail LOUD: folding a malformed namespace back to the main instance
	// would cross-wire two instances' wardens/paths.
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
	// warn_pct / remind_step_pct are still PARSED so an old file stays
	// loadable; they feed nothing.
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

// The default is ABSOLUTE so launching from another directory never grows a
// second database; the relative path is only the no-home fallback.
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

func sqliteFilePath(dsn string) (string, bool) {
	scheme, rest, found := strings.Cut(dsn, "://")
	if !found {
		return dsn, true
	}
	if scheme != "sqlite" && !strings.HasPrefix(scheme, "sqlite+") {
		return "", false
	}
	// SQLAlchemy: sqlite:///relative, sqlite:////absolute — after "://" one
	// more leading "/" separates authority from path.
	return strings.TrimPrefix(rest, "/"), true
}
