// Package proclaim reads and writes the local database of Faithlife Proclaim.
//
// Proclaim keeps every presentation it has synced in PresentationManager.db.
// A row with SyncState 1 is a local change. Proclaim uploads it within a
// minute of starting, so a write here reaches every other computer in the
// church group the same way an edit in the app does.
package proclaim

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

// syncState values in the Presentations table.
const (
	syncChanged = 1
	syncSynced  = 3
)

// ErrNoDatabase means Proclaim has not created a database on this computer.
var ErrNoDatabase = errors.New("no Proclaim database found, has Proclaim been started and signed in on this computer?")

// DataDir is where Proclaim keeps its per-account data folders.
func DataDir() (string, error) {
	switch runtime.GOOS {
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			return "", errors.New("LOCALAPPDATA is not set")
		}
		return filepath.Join(local, "Proclaim", "Data"), nil
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "Proclaim", "Data"), nil
	}
}

// Locate finds PresentationManager.db. With several signed-in accounts it
// picks the database changed most recently.
func Locate() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*", "PresentationManager", "PresentationManager.db"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", ErrNoDatabase
	}
	sort.Slice(matches, func(i, j int) bool { return modTime(matches[i]) > modTime(matches[j]) })
	return matches[0], nil
}

func modTime(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.ModTime().UnixNano()
}

// Running reports whether the Proclaim app is open. Proclaim holds the
// presentation in memory and overwrites the database when it saves, so
// writes must happen while it is closed.
func Running() bool {
	if runtime.GOOS == "windows" {
		out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq Proclaim.exe", "/NH").Output()
		return err == nil && strings.Contains(string(out), "Proclaim.exe")
	}
	return exec.Command("pgrep", "-x", "Proclaim").Run() == nil
}

// DB is an open PresentationManager.db.
type DB struct {
	path string
	sql  *sql.DB
}

// Open opens the database at path. A read-only handle never takes a write lock,
// so it is safe while Proclaim runs.
func Open(path string, readOnly bool) (*DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)"
	if readOnly {
		dsn += "&mode=ro"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return &DB{path: path, sql: db}, nil
}

// Path is the file the database was opened from.
func (d *DB) Path() string { return d.path }

// Close closes the database.
func (d *DB) Close() error { return d.sql.Close() }

// Backup writes a consistent copy of the database into dir and returns its path.
func (d *DB) Backup(dir, name string) (string, error) {
	return backup(d.sql, dir, name)
}

// BackupUploads copies UploadManager.db the same way.
func (d *DB) BackupUploads(dir, name string) (string, error) {
	db, err := sql.Open("sqlite", "file:"+UploadDBPath(d.path)+"?mode=ro")
	if err != nil {
		return "", err
	}
	defer db.Close()
	return backup(db, dir, name)
}

func backup(db *sql.DB, dir, name string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	target := filepath.Join(dir, name)
	if _, err := db.Exec("VACUUM INTO ?", target); err != nil {
		return "", fmt.Errorf("back up %s: %w", name, err)
	}
	return target, nil
}
