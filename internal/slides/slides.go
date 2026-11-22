// Package slides turns a slide deck into one 1920×1080 image per slide and
// animation step, the way Proclaim shows a PowerPoint.
package slides

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

//go:embed export.applescript
var exportScript string

//go:embed raster.js
var rasterScript string

// Dir is the folder next to the deck that Render writes its images to.
// Proclaim uploads the images from there, so they must stay until it has.
func Dir(deck string) string {
	name := strings.TrimSuffix(filepath.Base(deck), filepath.Ext(deck))
	return filepath.Join(filepath.Dir(deck), name+" (1920x1080)")
}

// Render exports a PowerPoint or Keynote file through Keynote to a PDF with
// every animation step as its own page, then draws each page at 1920×1080.
// Proclaim's own import uses Keynote's image export, which keeps the deck's
// size, often 720×405, and looks soft on a big screen.
func Render(deck string) ([]string, error) {
	if runtime.GOOS != "darwin" {
		return nil, errors.New("a slide deck needs Keynote, which only runs on macOS")
	}
	deck, err := filepath.Abs(deck)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(deck); err != nil {
		return nil, err
	} else if info.IsDir() {
		return nil, fmt.Errorf("%s is a folder, not a slide deck", deck)
	}

	tmp, err := os.MkdirTemp("", "proclaimctl-slides-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	scripts := map[string]string{"export.applescript": exportScript, "raster.js": rasterScript}
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(tmp, name), []byte(body), 0o600); err != nil {
			return nil, err
		}
	}

	pdf := filepath.Join(tmp, "deck.pdf")
	if _, err := osascript(filepath.Join(tmp, "export.applescript"), deck, pdf); err != nil {
		return nil, fmt.Errorf("export %s with Keynote: %w", filepath.Base(deck), err)
	}

	out := Dir(deck)
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, err
	}
	name := strings.TrimSuffix(filepath.Base(deck), filepath.Ext(deck))
	written, err := osascript("-l", "JavaScript", filepath.Join(tmp, "raster.js"), pdf, out, name)
	if err != nil {
		return nil, fmt.Errorf("draw the slides of %s: %w", filepath.Base(deck), err)
	}
	paths := strings.Split(strings.TrimSpace(written), "\n")
	if len(paths) == 0 || paths[0] == "" {
		return nil, fmt.Errorf("%s has no slides", filepath.Base(deck))
	}
	return paths, nil
}

func osascript(args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("osascript", args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", errors.New(msg)
		}
		return "", err
	}
	return stdout.String(), nil
}
