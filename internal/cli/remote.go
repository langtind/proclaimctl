package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"time"

	"github.com/langtind/proclaimctl/internal/remote"
)

// event is one line of the watch log.
type event struct {
	Time        string `json:"time"`
	Event       string `json:"event"`
	Error       string `json:"error,omitempty"`
	ItemID      string `json:"item_id,omitempty"`
	Title       string `json:"title,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Slide       *int   `json:"slide,omitempty"`
	QuickScreen string `json:"quick_screen,omitempty"`
	MediaState  string `json:"media_state,omitempty"`
}

func (a *App) remoteCmd(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "watch" {
		return usageError{"expected a subcommand: watch"}
	}
	fs := flag.NewFlagSet("remote watch", flag.ContinueOnError)
	logPath := fs.String("log", "", "also append each event to this file")
	baseURL := fs.String("url", remote.DefaultURL, "Proclaim's local server")
	if _, err := parseArgs(fs, args[1:], 0, "no arguments"); err != nil {
		return err
	}

	out := io.Writer(a.Stdout)
	if *logPath != "" {
		f, err := os.OpenFile(*logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		out = io.MultiWriter(a.Stdout, f)
	}
	enc := json.NewEncoder(out)
	emit := func(e event) {
		e.Time = a.Now().Format(time.RFC3339Nano)
		_ = enc.Encode(e)
	}

	return watch(ctx, remote.New(*baseURL), emit)
}

// watch logs every slide change until ctx ends. It waits through off-air
// periods and Proclaim restarts, so it can run for a whole service.
func watch(ctx context.Context, c *remote.Client, emit func(event)) error {
	var (
		session  string
		last     *remote.Status
		items    map[string]remote.Item
		revision int64 = -1
		offAir         = false
	)
	pause := func(d time.Duration) bool {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(d):
			return true
		}
	}

	for ctx.Err() == nil {
		if session == "" {
			id, err := c.Session(ctx)
			if err != nil {
				switch {
				case !errors.Is(err, remote.ErrOffAir):
					emit(event{Event: "error", Error: err.Error()})
				case !offAir:
					emit(event{Event: "off_air"})
				}
				offAir = errors.Is(err, remote.ErrOffAir)
				if !pause(time.Second) {
					break
				}
				continue
			}
			session, last, revision, offAir = id, nil, -1, false
			emit(event{Event: "on_air"})
		}

		s, err := c.WaitForChange(ctx, session, last)
		switch {
		case ctx.Err() != nil:
			return nil
		case errors.Is(err, context.DeadlineExceeded):
			continue // nothing changed during the long poll
		case errors.Is(err, remote.ErrOffAir):
			session = ""
			emit(event{Event: "off_air"})
			offAir = true
			continue
		case err != nil:
			emit(event{Event: "error", Error: err.Error()})
			session = ""
			continue
		}

		if s.LocalRevision != revision {
			list, err := c.Items(ctx, session)
			if err == nil {
				items = map[string]remote.Item{}
				for _, it := range list {
					items[it.ID] = it
				}
				revision = s.LocalRevision
			}
		}
		if last == nil || last.Status.ItemID != s.Status.ItemID || last.Status.SlideIndex != s.Status.SlideIndex ||
			last.Status.QuickScreen != s.Status.QuickScreen || last.Status.MediaState != s.Status.MediaState {
			slide := s.Status.SlideIndex
			it := items[s.Status.ItemID]
			emit(event{Event: "slide", ItemID: s.Status.ItemID, Title: it.Title, Kind: it.Kind, Slide: &slide,
				QuickScreen: s.Status.QuickScreen, MediaState: s.Status.MediaState})
		}
		last = &s
	}
	return nil
}
