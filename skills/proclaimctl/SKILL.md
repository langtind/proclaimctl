---
name: proclaimctl
description: Read and build Faithlife Proclaim presentations for a church service with the proclaimctl CLI - show a presentation's order, find songs and items used before, and rewrite a presentation from an order of service. Use when the user is the tech or projection person for a service and wants the presentation set up, checked, or changed, or asks what is in a Proclaim presentation. Triggers - "proclaim", "sett opp gudstjenesten", "storskjerm", "kjøreplan", "legg inn sangene", "order of service", "set up the slides", "worship slides", "projection".
---

# proclaimctl

`proclaimctl` reads and writes the local database of the Proclaim desktop app. Proclaim uploads a written change to Faithlife the next time it starts, so the whole team sees it. Output is Markdown by default. Add `--json` to a read command when you need to process the result.

## Look before you change

```bash
proclaimctl presentations                    # newest first, with ids
proclaimctl show "<id or part of title>"     # the order, by section, with item ids
proclaimctl find "<text>" [--kind SongLyrics]
```

`show` lists the four sections: pre-service loop, warm-up, service and post-service loop. Bible items show their passages. Reading is safe while Proclaim runs.

`find` searches item titles across every presentation and shows how often and when each was used. Use it to check that every song in the order of service already exists, and to get its exact title.

## Set up a service from an order of service

The order of service usually comes as an email or a document from the service leader. It lists each part, who leads it, and what goes on the big screen.

1. Run `show` on the presentation for the date. If none exists, pick the most recent service as a base.
2. Map each part that needs the screen to a step:
   - A song: `find` it, then `{"reuse": "<exact title>", "kind": "SongLyrics"}`.
   - A Bible reading or sermon text: `{"bible": "<Book> <chapter>:<verses>"}`. Use English book names and an en dash in ranges, as Proclaim does: `John 11:1–7`, `1 John 1:9`, `Philippians 4:6–7`.
   - A video file the user has, such as a mission film: `{"video": "<absolute path>"}`. Download it first if it is a link.
   - A fixed item already in the presentation, such as the creed, the Lord's Prayer, the giving slide or recording cues: `{"keep": "<title or id>"}`. Use the id when two items share a title.
   - Text that must change, such as a date on an announcement: add `"replace": {"old": "new"}` to the step.
3. Leave out what the order of service drops. A section you list holds only the steps you give it. A section you leave out is kept as it is.
4. Write the plan to a file and preview it.

```bash
proclaimctl apply plan.json
```

5. Show the user the preview next to the order of service. Point out anything you could not place: a song that does not exist, a reading that is not decided yet, media you could not add.
6. When the user agrees, ask them to quit Proclaim, then write.

```bash
proclaimctl apply plan.json --yes
```

7. Ask the user to start Proclaim and open the presentation. Run `show` until it no longer says `Changed locally` or `is not uploaded yet`. Then the presentation is on every computer. It uploads the change within a minute. Bible text fills in from the church's Bible when the item is shown. A video is uploaded to the church's media library, which can take a few minutes. If macOS asks whether Proclaim may read the video's folder, the user must allow it. Keep the video file in place until the upload is done.

A plan for a presentation that exists:

```json
{
  "presentation": "<id>",
  "start_time": "11:00",
  "service": [
    {"keep": "Countdown to Service Start"},
    {"reuse": "Amazing Grace", "kind": "SongLyrics"},
    {"bible": "John 11:1–7"},
    {"keep": "Creed"},
    {"keep": "Announcements", "replace": {"4 October": "18 October"}}
  ]
}
```

A new presentation copied from an old one takes `"from"`, `"title"` and `"date"` in place of `"presentation"`.

## Delete a presentation

```bash
proclaimctl delete "<id or title>"        # preview
proclaimctl delete "<id or title>" --yes  # with Proclaim closed
```

The deletion reaches every computer in the group. Delete only what the user named.

## Safety

- `apply` without `--yes` only previews. Always preview first.
- `apply --yes` refuses while Proclaim runs. Never work around it. Proclaim would overwrite the change, or the two would conflict.
- Every write backs up the database first and prints the path. To undo, quit Proclaim and copy the backup over `PresentationManager.db`. Tell the user before you do that, because it also undoes any other change made since.
- A written presentation reaches every computer in the church's Proclaim group. Write only what the user has seen in a preview.
- A video step uploads the file to the church's shared media library. Say so in the preview.

## Errors

| Message | Meaning |
| --- | --- |
| `Proclaim is running` | Ask the user to quit Proclaim, then run the command again. |
| `matches N presentations` | Use one of the ids in the message. |
| `no SongLyrics item titled "x"` | The song was never used in Proclaim. Run `find` with part of the title. If it is not there, tell the user it must be added in Proclaim's song library. |
| `no unused item "x" in the presentation` | The title is not in the presentation, or an earlier step already took it. |
| `text "x" not found` | The replace text is not on that item. Check the exact wording with the user. |

Exit code 2 means the command was called wrongly. Exit code 1 means it failed.
