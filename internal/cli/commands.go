package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/langtind/proclaimctl/internal/plan"
	"github.com/langtind/proclaimctl/internal/proclaim"
)

func (a *App) presentations(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("presentations", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	limit := fs.Int("limit", 15, "how many to list")
	if _, err := parseArgs(fs, args, 0, "no arguments"); err != nil {
		return err
	}
	if *limit < 1 {
		return usageError{"--limit must be at least 1"}
	}
	db, err := a.openDB(true)
	if err != nil {
		return err
	}
	defer db.Close()

	list, err := db.Presentations(*limit)
	if err != nil {
		return err
	}
	if *asJSON {
		return a.printJSON(list)
	}
	renderPresentations(a.Stdout, list)
	return nil
}

func (a *App) show(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	positional, err := parseArgs(fs, args, 1, "one presentation id or title, quoted if it has spaces")
	if err != nil {
		return err
	}
	db, err := a.openDB(true)
	if err != nil {
		return err
	}
	defer db.Close()

	p, err := db.FindPresentation(positional[0])
	if err != nil {
		return err
	}
	items, err := db.Items(p)
	if err != nil {
		return err
	}
	if *asJSON {
		return a.printJSON(struct {
			proclaim.Presentation
			Items []proclaim.Item `json:"items"`
		}{p, items})
	}
	renderPresentation(a.Stdout, p, items)
	return nil
}

func (a *App) find(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("find", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	kind := fs.String("kind", "", "only this item kind, such as SongLyrics")
	positional, err := parseArgs(fs, args, 1, "one search text, quoted if it has spaces")
	if err != nil {
		return err
	}
	db, err := a.openDB(true)
	if err != nil {
		return err
	}
	defer db.Close()

	uses, err := db.FindItems(positional[0], *kind)
	if err != nil {
		return err
	}
	if *asJSON {
		return a.printJSON(uses)
	}
	renderUses(a.Stdout, positional[0], uses)
	return nil
}

func (a *App) apply(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "write the plan, not just preview it")
	positional, err := parseArgs(fs, args, 1, "one plan file, or - for stdin")
	if err != nil {
		return err
	}
	p, err := readPlan(positional[0])
	if err != nil {
		return err
	}

	if *yes && a.ProclaimRunning() {
		return errors.New("Proclaim is running. Quit it first: it keeps the presentation in memory and would overwrite the change")
	}

	db, err := a.openDB(!*yes)
	if err != nil {
		return err
	}
	defer db.Close()

	now := a.Now()
	change, err := p.Resolve(db, now)
	if err != nil {
		return err
	}
	renderChange(a.Stdout, change)

	if !*yes {
		fmt.Fprintln(a.Stdout, "\nPreview only. Run again with --yes to write it, with Proclaim closed.")
		return nil
	}

	stamp := now.Format("20060102-150405")
	backup, err := db.Backup(a.BackupDir, "PresentationManager-"+stamp+".db")
	if err != nil {
		return err
	}
	if len(change.Uploads) > 0 {
		if _, err := db.BackupUploads(a.BackupDir, "UploadManager-"+stamp+".db"); err != nil {
			return err
		}
	}
	id, err := db.Apply(change, now)
	if err != nil {
		return fmt.Errorf("%w (nothing was written, backup at %s)", err, backup)
	}
	fmt.Fprintf(a.Stdout, "\nWrote presentation %s.\nBackup: %s\n", id, backup)
	fmt.Fprintln(a.Stdout, "Start Proclaim. It uploads the change within a minute.")
	for _, u := range change.Uploads {
		fmt.Fprintf(a.Stdout, "Queued %s for upload. Leave the file where it is until Proclaim has uploaded it.\n", u.Path)
	}
	return nil
}

func readPlan(path string) (plan.Plan, error) {
	var r io.Reader = os.Stdin
	if path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return plan.Plan{}, err
		}
		defer f.Close()
		r = f
	}
	return plan.Parse(r)
}
