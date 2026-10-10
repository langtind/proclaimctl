package main

import (
	"fmt"
	"io/fs"
	"os"

	"github.com/langtind/proclaimctl/internal/cli"
)

// Version information - injected at build time for GitHub releases
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	app := cli.New(os.Stdout, os.Stderr, cli.Build{Version: version, Commit: commit, Date: date})
	skill, err := fs.Sub(skillFS, "skills/proclaimctl")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	app.Skill = skill
	os.Exit(app.Run(os.Args[1:]))
}
