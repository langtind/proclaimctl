# proclaimctl

A command line tool for reading and building presentations in [Faithlife Proclaim](https://faithlife.com/products/proclaim), the church presentation software.

proclaimctl works on the database that the Proclaim desktop app keeps on your computer. It lists presentations, shows their order, finds songs and other items you have used before, and rewrites a presentation from a short JSON plan. The next time Proclaim starts, it uploads the change to Faithlife like any edit made in the app. Your team then sees it on every computer.

It is built to be driven by an AI agent. An agent reads the order of service from an email, writes the plan, and proclaimctl does the clicking.

proclaimctl is not made or supported by Faithlife. Proclaim has no public API, and the database format is undocumented. A Proclaim update can break proclaimctl. Every write makes a backup first.

## Install

```bash
brew install langtind/tap/proclaimctl
```

Or with Go:

```bash
go install github.com/langtind/proclaimctl@latest
```

proclaimctl is tested on macOS. It looks for the database where Proclaim keeps it on Windows too, but that is untested. Proclaim must have been started and signed in on the computer at least once.

## Use

List presentations, newest first.

```bash
proclaimctl presentations
```

Show the order of one presentation, by id or by part of its title.

```bash
proclaimctl show "12 April"
```

Find items to reuse: songs, liturgy, announcements, Bible passages.

```bash
proclaimctl find "amazing grace"
proclaimctl find grace --kind SongLyrics
```

Every read command prints Markdown, and JSON with `--json`. Reading is safe while Proclaim runs.

### Build a presentation from a plan

A plan lists the items of a section in order. Each step does one thing.

| Step | What it does |
| --- | --- |
| `{"keep": "Creed"}` | Keeps an item that is already in the presentation, by title or id |
| `{"reuse": "Amazing Grace", "kind": "SongLyrics"}` | Copies the most recent use of an item from any presentation |
| `{"bible": "John 11:1–7"}` | Adds an on-screen Bible passage in your church's Bible |
| `{"video": "/path/to/film.mp4"}` | Adds a video. Proclaim uploads it to your church's media library on its next start |
| `"replace": {"4 October": "18 October"}` | Added to any step, changes text on the item's slides |

```json
{
  "presentation": "Service 12 April",
  "start_time": "11:00",
  "service": [
    {"keep": "Countdown"},
    {"reuse": "Amazing Grace", "kind": "SongLyrics"},
    {"bible": "John 11:1–7"},
    {"keep": "Creed"},
    {"keep": "Announcements", "replace": {"4 October": "18 October"}}
  ]
}
```

The sections are `pre_service`, `warm_up`, `service` and `post_service`. A section you leave out keeps its items. A section you list contains only the items you list.

To start a new presentation from an old one, use `from` and a `title` in place of `presentation`.

```json
{"from": "Service 12 April", "title": "Service 19 April", "date": "2026-04-19"}
```

Preview the plan. This changes nothing.

```bash
proclaimctl apply plan.json
```

Quit Proclaim, then write it.

```bash
proclaimctl apply plan.json --yes
```

proclaimctl refuses to write while Proclaim runs, because Proclaim would overwrite the change. Before each write it copies the database to `~/Library/Application Support/proclaimctl/backups/` on macOS, or the matching folder on Windows. Start Proclaim afterwards. It uploads the presentation within a minute, and Bible passages fill in from your licensed Bible when they are shown. A video plays from the local file until Proclaim has uploaded it. Then Proclaim swaps in the uploaded copy and syncs again, so leave the file in place until that is done. macOS may ask whether Proclaim can read the folder the video is in. Allow it.

### Delete a presentation

```bash
proclaimctl delete "Service 12 April"        # preview
proclaimctl delete "Service 12 April" --yes  # with Proclaim closed
```

The presentation disappears from every computer in your group when Proclaim starts and syncs.

## Agent skill

proclaimctl carries a skill that teaches an agent how to use it.

```bash
proclaimctl install-skill
```

This copies the skill to `~/.claude/skills/proclaimctl`. Run it again after an upgrade.

## Develop

```bash
go build -o proclaimctl .
go test -race ./...
go vet ./...
```

`PROCLAIMCTL_DB` points proclaimctl at another copy of `PresentationManager.db`. Use it to try changes on a copy.

## License

MIT
