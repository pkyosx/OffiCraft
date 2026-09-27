package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Owner ruling rc-d961ee5e790c [0]: never refuse to start without a config file
// (a normal install has none, and the rescue commands `mfa-disable` / `backup`
// would be blocked) — instead SAY which config file and database were resolved,
// and where each answer came from. A no-op on behaviour by construction: it
// opens nothing, writes nothing and never changes an exit code.

func configSource(env func(string) string) (path string, exists bool, from string) {
	path = configPath(env)
	if p := env(envConfigPath); p != "" {
		from = "$" + envConfigPath
	} else {
		from = "the default filename, resolved against the current directory"
	}
	if _, err := os.Stat(path); err == nil {
		exists = true
	}
	return path, exists, from
}

// A second reading of config.go's resolveDSN, branch for branch INCLUDING the
// no-home fallback (which drops the namespace). If you edit resolveDSN's branch
// order, edit this one in the same commit.
func dsnSource(env func(string) string, cfg Config) string {
	if v := env(envDatabaseURL); v != "" {
		return "$" + envDatabaseURL
	}
	if cfg.StorageDSN != "" {
		return "[storage].dsn in the config file"
	}
	if home, err := os.UserHomeDir(); err != nil || home == "" {
		return "the no-home fallback — the namespace is IGNORED on this path"
	}
	if cfg.Server.Namespace != "" {
		return fmt.Sprintf("the built-in default for namespace %q", cfg.Server.Namespace)
	}
	return "the built-in default (no namespace)"
}

// Announces the FILE, not just the DSN: `sqlite:///data/oc.db` (relative) and
// `sqlite:////data/oc.db` (absolute) differ by one character, and the same
// relative DSN run from two directories acts on two different databases.
func announcedTarget(dsn string) string {
	path, ok := sqliteFilePath(dsn)
	if !ok {
		return fmt.Sprintf("%s (not a sqlite DSN — no local file)", dsn)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return fmt.Sprintf("%s (DSN %s)", abs, dsn)
}

func howToPointAtAConfigFile(env func(string) string) string {
	const tail = "Without one, nothing is read from oc.toml — $" + envDatabaseURL + " and the built-in defaults decide the rest."
	if env(envConfigPath) != "" {
		return fmt.Sprintf("to point this run at a config file, set %s=/path/to/oc.toml; it is set now, but names a file that is not there, and while it is set ./oc.toml is not consulted. %s", envConfigPath, tail)
	}
	return fmt.Sprintf("to point this run at a config file, set %s=/path/to/oc.toml or run from a directory containing oc.toml. %s", envConfigPath, tail)
}

// Bundles loadConfig + resolveDSN + the announcement so a caller cannot resolve
// a DSN without announcing it (TestEveryDSNResolutionIsAnnounced).
func announceResolution(name string, env func(string) string, out io.Writer) (cfg Config, dsn string, rc int) {
	cfgPath, cfgExists, cfgFrom := configSource(env)
	cfg, warnings, err := loadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: %v\n", err)
		return cfg, "", 1
	}
	for _, w := range warnings {
		fmt.Fprintf(out, "[ocserverd] WARN: %s\n", w)
	}
	dsn = resolveDSN(env, cfg)

	if cfgExists {
		fmt.Fprintf(out, "[ocserverd] %s: config file = %s (from %s)\n", name, cfgPath, cfgFrom)
	} else {
		// Deliberately not "ERROR"/"WARN": on a normal install this is the expected
		// state, and crying wolf teaches people to ignore this very line.
		wd, wdErr := os.Getwd()
		where := cfgPath
		if wdErr == nil && !filepath.IsAbs(cfgPath) {
			where = filepath.Join(wd, cfgPath)
		}
		fmt.Fprintf(out, "[ocserverd] %s: config file = none (looked at %s, from %s)\n", name, where, cfgFrom)
		fmt.Fprintf(out, "[ocserverd] %s: %s\n", name, howToPointAtAConfigFile(env))
	}
	fmt.Fprintf(out, "[ocserverd] %s: database    = %s, from %s\n", name, announcedTarget(dsn), dsnSource(env, cfg))
	return cfg, dsn, 0
}
