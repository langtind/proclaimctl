// Package cli implements the proclaimctl command line.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/langtind/proclaimctl/internal/proclaim"
)

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// Build holds the version information injected at build time.
type Build struct {
	Version string
	Commit  string
	Date    string
}

// App is one invocation of proclaimctl.
type App struct {
	Stdout io.Writer
	Stderr io.Writer
	Build  Build

	// Skill holds the agent skill files that install-skill copies out.
	Skill fs.FS

	// LocateDB finds PresentationManager.db.
	LocateDB func() (string, error)
	// ProclaimRunning reports whether the Proclaim app is open.
	ProclaimRunning func() bool
	// BackupDir is where apply keeps a copy of the database before writing.
	BackupDir string
	// Now is the clock used for timestamps.
	Now func() time.Time
}

func New(stdout, stderr io.Writer, build Build) *App {
	backups := ""
	if dir, err := os.UserConfigDir(); err == nil {
		backups = filepath.Join(dir, "proclaimctl", "backups")
	}
	return &App{
		Stdout:          stdout,
		Stderr:          stderr,
		Build:           build,
		LocateDB:        locateDB,
		ProclaimRunning: proclaim.Running,
		BackupDir:       backups,
		Now:             time.Now,
	}
}

// locateDB honours PROCLAIMCTL_DB before looking in Proclaim's data folder.
func locateDB() (string, error) {
	if path := os.Getenv("PROCLAIMCTL_DB"); path != "" {
		return path, nil
	}
	return proclaim.Locate()
}

type command struct {
	name    string
	usage   string
	summary string
	run     func(a *App, ctx context.Context, args []string) error
}

var commands = []command{
	{"presentations", "presentations [--limit <n>] [--json]", "List presentations, newest first", (*App).presentations},
	{"show", "show <presentation> [--json]", "Show a presentation's order of items", (*App).show},
	{"find", "find <text> [--kind <kind>] [--json]", "Find items to reuse, such as songs, across all presentations", (*App).find},
	{"apply", "apply <plan.json|-> [--yes]", "Preview a plan, and write it with --yes", (*App).apply},
	{"delete", "delete <presentation> [--yes]", "Delete a presentation on every computer, with --yes", (*App).delete},
	{"install-skill", "install-skill [--path <dir>]", "Install the agent skill to ~/.claude/skills", (*App).installSkill},
}

// usageError marks a mistake in how proclaimctl was called.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

// Run executes proclaimctl with args (without the program name) and returns the exit code.
func (a *App) Run(args []string) int {
	if len(args) == 0 {
		a.help(a.Stderr)
		return exitUsage
	}

	switch args[0] {
	case "-h", "--help", "help":
		a.help(a.Stdout)
		return exitOK
	case "-v", "--version", "version":
		a.version()
		return exitOK
	}

	for _, cmd := range commands {
		if cmd.name != args[0] {
			continue
		}
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		err := cmd.run(a, ctx, args[1:])
		if err == nil {
			return exitOK
		}
		fmt.Fprintf(a.Stderr, "Error: %v\n", err)
		var usage usageError
		if errors.As(err, &usage) {
			fmt.Fprintf(a.Stderr, "Usage: proclaimctl %s\n", cmd.usage)
			return exitUsage
		}
		return exitError
	}

	fmt.Fprintf(a.Stderr, "Error: unknown command %q\n\n", args[0])
	a.help(a.Stderr)
	return exitUsage
}

func (a *App) help(w io.Writer) {
	fmt.Fprintln(w, "proclaimctl - read and build Faithlife Proclaim presentations")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	for _, cmd := range commands {
		fmt.Fprintf(w, "  proclaimctl %s\n      %s\n", cmd.usage, cmd.summary)
	}
	fmt.Fprintf(w, "  proclaimctl version\n      Show version information\n")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "PROCLAIMCTL_DB overrides the path to PresentationManager.db.")
}

func (a *App) version() {
	fmt.Fprintf(a.Stdout, "proclaimctl version %s\n", a.Build.Version)
	if a.Build.Commit != "unknown" {
		fmt.Fprintf(a.Stdout, "commit: %s\n", a.Build.Commit)
	}
	if a.Build.Date != "unknown" {
		fmt.Fprintf(a.Stdout, "built: %s\n", a.Build.Date)
	}
}

// parseArgs parses flags placed on either side of the positional arguments.
func parseArgs(fs *flag.FlagSet, args []string, wantPositional int, what string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var positional []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return nil, usageError{err.Error()}
		}
		args = fs.Args()
		if len(args) > 0 {
			positional = append(positional, args[0])
			args = args[1:]
		}
	}
	if len(positional) != wantPositional {
		return nil, usageError{"expected " + what}
	}
	return positional, nil
}

func (a *App) printJSON(v any) error {
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// openDB opens the database, read-only unless the command writes.
func (a *App) openDB(readOnly bool) (*proclaim.DB, error) {
	path, err := a.LocateDB()
	if err != nil {
		return nil, err
	}
	return proclaim.Open(path, readOnly)
}
