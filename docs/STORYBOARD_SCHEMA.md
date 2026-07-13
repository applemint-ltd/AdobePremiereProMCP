# Storyboard format (storyboard/v1)

The storyboard is the contract between "what the user wants" and "what gets
assembled onto the Premiere timeline". It comes from one of three places:

1. **A script or narration document** — `premiere_assemble_storyboard` with
   `script_text`/`script_path` (+ `assets_directory`); the intelligence
   service parses it and matches footage.
2. **A simple shot-list spreadsheet (CSV)** — human-friendly columns, no
   editor jargon (see below).
3. **Direct JSON** — an AI agent emits the schema straight (get it from
   `premiere_storyboard_schema`).

All three normalize to the same document, validated by
`premiere_storyboard_validate` (dry run: shows exactly which clips resolve
against the project before anything is touched) and executed by
`premiere_assemble_storyboard`, which returns a per-shot report — every shot
is `placed`, `text_card`, `skipped_unresolved`, or `failed`, transitions are
applied-or-reported, never silent.

The machine-readable schema lives at
[`storyboard.schema.json`](storyboard.schema.json) (source of truth:
`go-orchestrator/internal/storyboard/schema.json`, embedded in the server).

## JSON example

```json
{
  "version": "storyboard/v1",
  "title": "Summer Promo",
  "scenes": [
    {
      "name": "Intro",
      "shots": [
        {
          "clip": "beach sunset",
          "duration_seconds": 4,
          "text": [{ "content": "SUMMER 2026", "style": "title" }],
          "caption": "It started with one perfect week.",
          "transition_after": { "name": "Cross Dissolve", "duration_seconds": 1 }
        },
        {
          "clip": "/Volumes/footage/drone_042.mp4",
          "trim": { "from_seconds": 12, "to_seconds": 18 }
        }
      ]
    }
  ],
  "music": { "clip": "upbeat_track", "note": "keep it quiet under dialogue" }
}
```

Rules of thumb:

- **Clips are referenced by name or file path — never by index.** Ambiguous
  names fail with a candidate list instead of guessing.
- **Times are seconds.** `trim` selects a source range; `duration_seconds`
  is the target length on the timeline (0/omitted = natural length).
- **A shot with no `clip` but with `text` is a text card** (needs
  `duration_seconds`).
- **On-screen `text` renders as a baked-PNG layer** — the only text that
  renders on Premiere 2026. Styles: `title`, `lower_third`, `caption_card`.
- **`caption` lines become a native SRT caption track** (editable in
  Premiere's Captions panel).
- File paths that aren't in the project yet are imported automatically.

## Shot-list CSV

Header-driven, columns in any order, only `clip` required:

```csv
order,clip,duration,from,to,text,caption,transition,notes
1,beach,4,,,SUMMER 2026,It started with one perfect week.,dissolve,opener
2,drone_042,,0:12,0:18,,,cut,
3,interview_wide,6,,,,So we packed everything.,fade to black,
```

- `duration`, `from`, `to` accept seconds (`4.5`) or `m:ss` (`1:20`).
- `transition` accepts casual names (`dissolve`, `fade to black`, `cut`/`none`
  for a hard cut).
- No track indices, no timeline positions — order is the `order` column (or
  file order).
