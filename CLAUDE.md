# CLAUDE.md

proclaimctl is a Go CLI for reading and building Faithlife Proclaim presentations. It is open source. It works on the SQLite database the Proclaim desktop app keeps locally. The format is undocumented and observed from the app.

## Rules

- Never write while Proclaim runs. Proclaim keeps the presentation in memory and overwrites the database on save. `apply --yes` checks this. Keep the check.
- Every write takes a backup first and runs in one transaction.
- Nothing from a real church goes into the repo: no database, no backup, no presentation, song text, or names. Tests use the made-up fixture in `internal/cli/cli_test.go`.
- Do not commit decompiled Proclaim code. Read it to learn the format, then write down what you learned in `reference/`.
- Do not change a format from a guess. Change something in the app, diff the database, and note the result in `reference/`.

## Layout

- `main.go` wires version info into `internal/cli`.
- `internal/cli` holds command routing, flag parsing, and rendering. Commands live in the `commands` table in `cli.go`.
- `internal/proclaim` reads and writes `PresentationManager.db`.
- `internal/plan` parses a JSON plan and resolves it into a `proclaim.Change`.
- `skills/proclaimctl/SKILL.md` is the agent skill, embedded and copied out by `install-skill`. Update it when a command changes.
- `reference/` holds notes on the database format.

## Commands

```bash
go build -o proclaimctl .
go test -race ./...
go vet ./...
```

The only dependency is `modernc.org/sqlite`, a SQLite driver in pure Go, so release builds work with `CGO_ENABLED=0`. Add another only when it removes real code.

Releases are cut by pushing a `v*` tag. GoReleaser builds, publishes, and updates the cask in `langtind/homebrew-tap`.
