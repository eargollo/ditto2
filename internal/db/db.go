package db

import (
	"database/sql"
	"embed"
	"fmt"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open opens (or creates) the SQLite database at path, applies PRAGMAs
// for WAL mode, and enforces a single writer connection.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}

	// Single writer prevents SQLITE_BUSY under WAL.
	db.SetMaxOpenConns(1)

	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA cache_size = -131072", // 128 MB
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("pragma %q: %w", p, err)
		}
	}

	return db, nil
}

// OpenReadPool opens a dedicated read-only connection pool to the same SQLite
// database. WAL mode allows concurrent readers alongside a single writer, so
// this pool can be used for read-intensive operations (e.g. cache check during
// scans) without contending with the write connection.
func OpenReadPool(path string, maxConns int) (*sql.DB, error) {
	rdb, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite read pool %q: %w", path, err)
	}
	rdb.SetMaxOpenConns(maxConns)

	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA query_only = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA cache_size = -131072", // 128 MB
	}
	for _, p := range pragmas {
		if _, err := rdb.Exec(p); err != nil {
			rdb.Close()
			return nil, fmt.Errorf("pragma %q: %w", p, err)
		}
	}
	return rdb, nil
}

// LoadSettings returns all rows from the settings table as a key→value map.
func LoadSettings(db *sql.DB) (map[string]string, error) {
	rows, err := db.Query("SELECT key, value FROM settings")
	if err != nil {
		return nil, fmt.Errorf("query settings: %w", err)
	}
	defer rows.Close()
	m := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("scan settings row: %w", err)
		}
		m[k] = v
	}
	return m, rows.Err()
}

// SaveSetting upserts a single key in the settings table.
func SaveSetting(db *sql.DB, key, value string) error {
	_, err := db.Exec(
		"INSERT OR REPLACE INTO settings(key, value, updated_at) VALUES(?, ?, ?)",
		key, value, time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("save setting %q: %w", key, err)
	}
	return nil
}

// RunMigrations applies all pending goose migrations from the embedded FS.
func RunMigrations(db *sql.DB) error {
	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("goose set dialect: %w", err)
	}
	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}
