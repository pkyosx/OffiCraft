package main

// config.go — the oc-updater.toml reader. Effective schema:
//
//	[server].port      (default 8790 — clear of ocserverd's 8770 and vibe's 8766)
//	[storage].data_dir (default ~/.ocupdaterd — holds updater.db + blobs/)
//
// Resolution: $OC_UPDATER_CONFIG (out-of-repo canonical) → ./oc-updater.toml
// (CWD-relative). The file may be entirely absent: every key has a convention
// default. The bind host is HARDWIRED loopback (same B2 posture as ocserverd:
// exposure goes through a tunnel, never a direct non-loopback bind).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	envConfigPath = "OC_UPDATER_CONFIG"

	defaultHost = "127.0.0.1"
	defaultPort = 8790
)

// updaterConfig is the fully resolved oc-updater.toml.
type updaterConfig struct {
	Port    int
	DataDir string
}

// tomlFile is the on-disk oc-updater.toml shape (unknown keys/tables are
// ignored so the file can grow sections without breaking this reader).
type tomlFile struct {
	Server struct {
		Port int `toml:"port"`
	} `toml:"server"`
	Storage struct {
		DataDir string `toml:"data_dir"`
	} `toml:"storage"`
}

// configPath resolves the config location: $OC_UPDATER_CONFIG (when set
// non-empty) wins, else the CWD-relative convention default.
func configPath(env func(string) string) string {
	if p := env(envConfigPath); p != "" {
		return p
	}
	return "oc-updater.toml"
}

// loadConfig reads oc-updater.toml at path. A missing file yields convention
// defaults (never an error); a MALFORMED file is an error — a half-read config
// must not silently boot with defaults.
func loadConfig(path string) (updaterConfig, error) {
	cfg := updaterConfig{Port: defaultPort}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return withDefaultDataDir(cfg)
		}
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}
	var f tomlFile
	f.Server.Port = cfg.Port
	if err := toml.Unmarshal(raw, &f); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	if f.Server.Port <= 0 || f.Server.Port > 65535 {
		return cfg, fmt.Errorf("parse %s: [server].port must be 1..65535, got %d", path, f.Server.Port)
	}
	cfg.Port = f.Server.Port
	cfg.DataDir = expandHome(strings.TrimSpace(f.Storage.DataDir))
	if cfg.DataDir == "" {
		return withDefaultDataDir(cfg)
	}
	return cfg, nil
}

// withDefaultDataDir fills the convention data dir: an ABSOLUTE path under the
// home directory (never CWD-relative — launching from a different directory
// must not silently grow a second database, same lesson as ocserverd B2).
func withDefaultDataDir(cfg updaterConfig) (updaterConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return cfg, fmt.Errorf("cannot resolve the default data dir (no home directory): set [storage].data_dir in oc-updater.toml")
	}
	cfg.DataDir = filepath.Join(home, ".ocupdaterd")
	return cfg, nil
}

// expandHome resolves a leading "~/" (and bare "~") against the home dir so a
// hand-written config behaves the way its author read it.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~"+string(filepath.Separator)) {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), string(filepath.Separator)))
		}
	}
	return p
}
