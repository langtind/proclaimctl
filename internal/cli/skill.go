package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const skillName = "proclaimctl"

// installSkill copies the embedded agent skill into a skills directory.
// Reinstalling replaces the files, which is how the skill follows an upgrade.
func (a *App) installSkill(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("install-skill", flag.ContinueOnError)
	parent := flags.String("path", "", "skills directory (default ~/.claude/skills)")
	if _, err := parseArgs(flags, args, 0, "no arguments"); err != nil {
		return err
	}
	if a.Skill == nil {
		return errors.New("this build has no skill embedded")
	}

	if *parent == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("find home directory: %w", err)
		}
		*parent = filepath.Join(home, ".claude", "skills")
	}
	dest := filepath.Join(*parent, skillName)

	count := 0
	err := fs.WalkDir(a.Skill, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(a.Skill, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		count++
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		return fmt.Errorf("install skill: %w", err)
	}
	fmt.Fprintf(a.Stdout, "Installed %d file(s) to %s\n", count, dest)
	return nil
}
