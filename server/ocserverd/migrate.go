package main

// migrate.go — the goose migration base over modernc.org/sqlite (cgo-free).
// The migrations are EMBEDDED so the shipped binary carries its own schema
// history (no on-disk migrations dir needed at runtime).
//
// migrations/ carries the real schema (00001_schema.sql — the retired Python
// implementation's tables reshaped per the ontology blueprint; dal.go is the
// access layer). sqlite-only for now: a postgres DSN is an explicit error
// until a postgres driver story is decided.

import (
	"database/sql"
	"embed"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // registers the cgo-free "sqlite" driver
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// openSQLite opens (creating parent dirs — dal.engine's zero-setup first boot)
// the SQLite database at path via the modernc driver. Concurrency posture for
// a single-process server: ONE pooled connection (SQLite is a single-writer
// store; a second pooled conn only manufactures SQLITE_BUSY between our own
// requests) plus a busy timeout as the belt for external openers.
func openSQLite(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// runMigrations applies every embedded goose migration (goose up) to db.
func runMigrations(db *sql.DB) error {
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}
	return goose.Up(db, "migrations")
}

// cmdMigrate resolves the DSN (env → oc.toml → sqlite convention default) and
// runs goose up against it.
func cmdMigrate(env func(string) string, out io.Writer) int {
	cfg, warnings, err := loadConfig(configPath(env))
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: %v\n", err)
		return 1
	}
	for _, w := range warnings {
		fmt.Fprintf(out, "[ocserverd] WARN: %s\n", w)
	}
	dsn := resolveDSN(env, cfg)
	path, ok := sqliteFilePath(dsn)
	if !ok {
		fmt.Fprintf(out, "[ocserverd] FATAL: migrate supports sqlite DSNs only for now (got %q); postgres lands with the M3 dal step\n", dsn)
		return 1
	}
	db, err := openSQLite(path)
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: open %s: %v\n", path, err)
		return 1
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: goose up: %v\n", err)
		return 1
	}
	// Out-of-box seed (idempotent): Mira + the server-self warden — the same
	// seed serve start ensures, so a bare `migrate` yields a bootable roster.
	if err := seedOutOfBox(NewDAL(db)); err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: seed: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "[ocserverd] migrations applied + seed ensured (%s)\n", path)
	return 0
}
