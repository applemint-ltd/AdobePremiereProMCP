# Bulk Short-Video A/B Testing Playbook (9:16 — Reels / TikTok)

How to produce a **batch of vertical short-video variants** with the Premiere MCP
server, get them exported and posted with clean naming, and actually learn
something from the results.

This is for editors and growth teams running creative experiments. It assumes
you drive the system through an AI agent (Claude via CLI or the Slack bot —
see [SLACK_VIDEO_EDITING_GUIDE.md](SLACK_VIDEO_EDITING_GUIDE.md)). For the
storyboard format itself, see [STORYBOARD_SCHEMA.md](STORYBOARD_SCHEMA.md);
this playbook only shows worked examples.

Everything below uses **only tools that work today**. Where the platform has a
sharp edge (vertical sequences, text rendering, export semantics, Slack
uploads), the workaround is written inline — nothing here requires
auto-reframe, auto-captions, or any "TikTok export" tool, because those do
not currently work. The limits, and the feature requests that would remove
them, are collected in
[feature-requests-shorts-ab.md](feature-requests-shorts-ab.md).

---

## 1. Experiment design: what to test, in what order

### One variable per experiment. Always.

If variant B has a different hook line *and* different music, and it wins, you
learned nothing you can reuse. Every batch changes exactly one dimension; the
control (your current best performer) is always one of the posted variants.

Common violations to watch for by name:

- Changing hook text **and** music ("while we're in there").
- Changing the opening shot — which **silently changes the default cover
  frame** (the cover derives from the video). That's two variables. Pin the
  cover manually at post time when the cover is not the thing you're testing.
- Trimming for length, which also changes pacing rhythm — decide which one
  you're testing.
- Letting the AI agent "improve" trims or transitions between variants
  unprompted. Agents love to fix things; it destroys the experiment
  invisibly. Tell it explicitly: *variants must differ only in the tested
  field.* Diff the storyboard files to verify (§4).

### Order of leverage

Test the things that gate everything downstream first. A better CTA on a video
nobody watches past second two is wasted work.

| Priority | Variable | Why | Primary metric |
|---|---|---|---|
| 1 | **Hook text + first-2s visual** | Decides whether the viewer exists at all; between-hook differences of 2–5× on watch time are routine | 3-second hold rate |
| 2 | **Cover frame** | Drives profile-grid, search, and Explore clicks — invisible if you only watch FYP views | Cover CTR; traffic-source split |
| 3 | **CTA (what/when/where)** | Converts attention you already earned; effects are small — expect to need paid confirmation | Profile visits / follows / link clicks per reach |
| 4 | **Music / sound** | Real but trend-confounded — a "winning" sound may just have been trending that week; date-stamp conclusions | Completion rate, shares, traffic-source split |
| 5 | **Pacing & length** | Big effect but expensive to vary; completion is length-confounded — compare absolute watch seconds too | Completion rate + avg watch seconds |
| 6 | **Caption style** (burned vs native, density) | Matters for sound-off viewers (most of IG feed); interacts with safe areas | Sound-off completion, avg watch % |
| 7 | **Color grade / LUT** | Smallest effect; usually undetectable at organic sample sizes. Easy to automate ≠ worth testing | Hold rate + saves; expect ties |

**Content angle (a different story entirely) is not an A/B variable.** A
different angle is a new video, not a variant — run it as its own post and
judge it by saves + shares per reach. Don't "A/B test" two unrelated videos
and call it science.

Hook tests are also the cheapest to produce here: text swaps are pure
storyboard `text[]` changes, no re-editing.

### How many variants per batch

The pipeline is **one Premiere, one lane**: all timeline assembly is serial
(single ExtendScript engine). Only Adobe Media Encoder renders run in parallel
with your next assembly. Realistic per-variant cost for a 20–30s short: 2–4
minutes of assembly + QC, plus a render that overlaps the next variant's
assembly (warm AME renders a short in seconds; a cold start costs 1–2 minutes
once — see `premiere_warm_encoder` below).

But generation isn't the real constraint — **posting slots are**. Posting is
manual, and variants need spacing (below). Practical answer: **1 control +
1–2 challengers per experiment (2–3 posted variants)**. Use the pipeline's
cheap generation for *pre-filtering instead of posting volume*: generate 4–5
hook candidates, review them in Slack (frame captures or previews), and post
only the best 2. An account posting daily supports about **one experiment per
week**; don't run two experiments on the same account in the same window.

### Posting protocol: the experiment includes the posting

The posting metadata is part of the control. Between variants keep constant:
caption text, hashtags, sound (unless sound is the variable), cover style,
and the posting slot.

- **Same account, always**, for anything you want to learn about *your*
  audience. Fresh "test accounts" have no seed audience and 2–4 weeks of
  warm-up dynamics that swamp any variant effect. (No experiments in the
  first ~3 weeks of a new account or right after a posting hiatus, either.)
- **Never post variants back-to-back.** They cannibalize the same audience,
  and TikTok fingerprints near-duplicate content. Space variants **48–72h
  apart, same slot ± 1 hour, same day-type** (weekday vs weekend audiences
  differ). Yes: a 2-variant experiment takes ~3–6 days. That's the real cost
  of organic testing.
- **One platform per experiment.** Posting variant A to TikTok and variant B
  to Reels tests nothing — and the platforms *define "view" differently*, so
  never compare counts across them.
- **Freeze cadence during an experiment.** Changing posting frequency
  mid-test changes delivery for reasons unrelated to the variants.

### Judging results: rate metrics, fixed windows, honest thresholds

**Views are a bad primary metric.** A view count = content quality × the
algorithmic delivery lottery; identical videos posted twice routinely differ
5–10× in views. **Primary metrics are always ratios** — hold rate, avg watch
%, completion, saves/reach, follows/reach — never absolute counts.

With one organic post per variant you cannot do statistics, so use decision
rules and be honest that they're heuristics:

- Read metrics at a **fixed offset for every post: 72h** is the decision
  snapshot (both platforms drip-feed distribution through day 2–3), plus a
  **7d** sanity snapshot. Comparing one post's 24h numbers to another's
  5-day numbers is the most common self-inflicted error in manual tracking.
- Don't judge any post with **< 500 views**; rates stabilize around
  **~1,000**.
- Call a winner only on a big gap: **3s hold rate ≥ 10 percentage points**
  apart; **avg watch % ≥ 15% relative**; **completion ≥ 8 pp**
  (length-matched variants only); **follows-per-reach ≥ 2×**. Anything
  closer is a **tie — keep the control and rerun**; don't rationalize.
- If either variant's views exceed **~5× the account's trailing-10-post
  median**, mark the experiment **invalid (outlier delivery)** — viral
  distribution reaches colder audiences whose rates aren't comparable —
  and rerun.
- Promote a pattern to your playbook only after it wins **3 independent
  experiments**. One win is an anecdote.

Organic testing at this sample size can rank-order *big* levers — hooks,
opening visuals, gross length classes — and nothing else. **When the decision
matters** (a hook for a whole campaign, a brand CTA), confirm with paid A/B:
TikTok Spark Ads on the organic posts, or Meta's A/B test tool with Reels
placement. $50–150 per variant over 3–5 days buys a randomized,
delivery-controlled read that organic can never give you. The organic
program's job is to generate and cheaply pre-filter hypotheses; paid is the
confirmatory instrument.

**The free lunch:** read the **retention curve** of every post before
designing the next experiment. The exact second people leave *is* the
diagnosis — a cliff at 0–2s is a hook problem, a slow bleed is pacing, a
cliff at the CTA is the CTA. One curve often teaches more than a two-post
A/B.

---

## 2. Naming and tracking conventions

Discipline here is what makes "bulk" possible. The rule: **the experiment ID
and variant code appear in the storyboard filename, the sequence name, and
the exported filename** — so any artifact can be traced back without a
lookup.

### IDs and codes

- **Experiment ID:** `EXP` + zero-padded number: `EXP014`. One ID = one
  hypothesis = one variable.
- **Variant code:** the variable plus a letter: `hookA`, `hookB`, `musicA`,
  `paceA`, `ctaB`, `coverA`. The control reuses the previous winner's code
  or is marked in the sheet.
- **Version:** `v1`, bumped only when you re-export the *same* variant after
  a fix. Never reuse a filename for different content.

| Artifact | Convention | Example |
|---|---|---|
| Storyboard file | `storyboards/EXP014/<variant>.json` | `storyboards/EXP014/hookB.json` |
| Sequence name (`sequence.name` in the storyboard) | `EXP014-hookB` | `EXP014-hookB` |
| Export file | `exports/EXP014/EXP014-hookB-v1.mp4` | `EXP014-hookB-v1.mp4` |
| Slack thread | one thread per experiment | "EXP014 — hook text test" |

### The tracking sheet

One CSV (or Google Sheet) for the whole program, one row per posted variant.
The sheet is the experimental record; platform analytics decay and memory
lies. Capture the post-time context **when you post** — every one of those
fields is a known confounder you'll want when a result looks weird.

Columns, in four groups:

- **Identity:** `experiment`, `hypothesis`, `variable`, `variant`,
  `is_control`, `storyboard_file`, `sequence_name`, `output_file`,
  `duration_s`
- **Post-time context:** `platform`, `account`, `follower_count_at_post`,
  `posted_at` (local time), `day_of_week`, `caption_text`, `hashtags`,
  `sound_used` (+ trending y/n), `cover_choice`, `other_posts_within_24h`
- **Metrics** (`_72h` decision snapshot; repeat at `_7d`): `views`, `reach`,
  `view_3s_rate`, `avg_watch_pct`, `completion_rate`, `likes`, `comments`,
  `shares`, `saves`, `follows`, `profile_visits`, `link_clicks`,
  `traffic_source_split`
- **Outcome:** `verdict` (winner / tie / loser / invalid-outlier /
  invalid-protocol), `decision_note`, `date_judged`

Example rows (abridged):

```csv
experiment,variable,variant,is_control,sequence_name,output_file,platform,posted_at,view_3s_rate_72h,avg_watch_pct_72h,verdict
EXP014,hook_text,hookA,true,EXP014-hookA,EXP014-hookA-v1.mp4,tiktok,2026-07-16T18:00+09:00,0.61,0.44,tie
EXP014,hook_text,hookB,false,EXP014-hookB,EXP014-hookB-v1.mp4,tiktok,2026-07-19T18:05+09:00,0.74,0.51,winner
```

### The server keeps receipts for you

Every tool call is audit-logged. At the end of a batch, ask the agent for
`premiere_get_session_digest` — it summarizes what was assembled, exported,
and posted — and `premiere_what_changed` for a timeline diff since the
session started. Paste the digest into the experiment's Slack thread: it is
your reproducibility record ("which storyboard produced which sequence
produced which file") if the tracking sheet is ever wrong.

---

## 3. Project and asset layout

### One directory tree per program, ASCII paths only

**Known bug:** posting files to Slack fails when the project lives under a
path with Japanese (or other NFD-normalized Unicode) characters — the upload
path check breaks. Keep the whole tree ASCII
(`/Users/you/shorts-lab/...`, not `/Users/you/ショート動画/...`). If you
inherit a non-ASCII project, `premiere_save_project_as` to an ASCII path
before exporting.

```
shorts-lab/
  shorts-lab.prproj          <- the Premiere project (ASCII name)
  raw/                       <- source footage (SHOT OR CROPPED TO 1080x1920 — see §6)
    product-demo/
    talking-head/
  music/                     <- licensed tracks, trimmed to ~35s loops
  srt/                       <- externally authored captions (see appendix)
  storyboards/
    EXP014/
      base.json
      hookA.json
      hookB.json
  exports/
    EXP014/                  <- deterministic output paths land here
```

### Bins mirror experiments

Inside the Premiere project, keep one bin per experiment plus shared media
bins. `premiere_import_files` with `target_bin` auto-creates the bin
hierarchy, so the agent can do this in one call:

1. `premiere_scan_assets` on `raw/product-demo/` — inventories the folder
   (durations, dimensions, codecs) **without importing anything**. Use this
   to confirm every clip is 1080×1920 before you touch the timeline.
2. `premiere_import_files` with `target_bin: "footage/product-demo"` for the
   clips the experiment actually uses. Music goes to a `music` bin.
3. Slack-uploaded assets arrive via `premiere_fetch_slack_attachment` and get
   imported into the same bins.

Importing only what you use keeps clip-name resolution unambiguous — the
storyboard resolves clips by name, and a project full of `IMG_0231.mov`
duplicates will produce ambiguity errors.

---

## 4. The base + variant storyboard pattern

The storyboard schema (`storyboard/v1`) has **no variant or override
concept**, and it rejects unknown keys (`additionalProperties: false` — you
can't smuggle in an `"experiment"` field). So variants are plain file copies
of a base storyboard, differing in exactly the lines that implement the
experiment variable. This is a feature: `diff base.json hookB.json` *is* the
experiment definition — run that diff before assembling and confirm it only
touches the tested field (plus `sequence.name`).

Convention:

- `storyboards/EXP014/base.json` — the control cut, fully authored.
- `storyboards/EXP014/hookA.json`, `hookB.json`, … — copies of base with
  **only** the tested dimension changed, and `sequence.name` updated to the
  variant code.
- Never edit `base.json` mid-experiment. Promote a winner by copying it over
  base for the *next* experiment ID (§8).

What changes per test type:

| Test | What differs between variant files |
|---|---|
| Hook text | `text[].content` of the first shot (and nothing else) |
| Music | `music.clip` (and possibly `music.level_db`) |
| Pacing / length | shot order, `trim` ranges, `duration_seconds` |
| CTA | `text[]` on the last shot, or the final text-card shot |
| Caption style | per-shot `caption` lines vs a burned `text[]` treatment |

### Worked example: a hook-text A/B pair

Both files below are identical except `sequence.name` and the first shot's
`text[0].content`. Sources are already vertical (1080×1920); the `sequence`
block sets the **text render canvas** to 1080×1920 (critical — see §6) and
names the sequence with the variant code.

**`storyboards/EXP014/hookA.json`** (control — statement hook):

```json
{
  "version": "storyboard/v1",
  "title": "EXP014 onboarding demo - hook test A",
  "sequence": { "name": "EXP014-hookA", "width": 1080, "height": 1920, "fps": 30 },
  "scenes": [
    {
      "name": "Hook",
      "shots": [
        {
          "clip": "demo_screen_vertical.mp4",
          "trim": { "from_seconds": 2.0, "to_seconds": 4.5 },
          "text": [
            {
              "content": "Set up in 60 seconds.",
              "style": "title",
              "start_offset_seconds": 0,
              "duration_seconds": 2.5
            }
          ],
          "caption": "Set up in sixty seconds."
        }
      ]
    },
    {
      "name": "Demo",
      "shots": [
        {
          "clip": "demo_screen_vertical.mp4",
          "trim": { "from_seconds": 10.0, "to_seconds": 22.0 },
          "caption": "Connect your account, pick a template, done.",
          "transition_after": { "name": "Cross Dissolve", "duration_seconds": 0.3 }
        },
        {
          "clip": "founder_talking_vertical.mp4",
          "trim": { "from_seconds": 5.0, "to_seconds": 11.0 },
          "caption": "No credit card, no sales call."
        }
      ]
    },
    {
      "name": "CTA",
      "shots": [
        {
          "duration_seconds": 2.5,
          "text": [
            { "content": "Link in bio.", "style": "title" }
          ]
        }
      ]
    }
  ],
  "music": { "clip": "upbeat_loop_35s.wav", "level_db": -18 }
}
```

**`storyboards/EXP014/hookB.json`** (challenger — question hook). The entire
diff against hookA:

```json
  "sequence": { "name": "EXP014-hookB", "width": 1080, "height": 1920, "fps": 30 },
  ...
              "content": "Still doing this manually?",
```

Notes on the example:

- **Text-only CTA cards work** — a shot with `text` and `duration_seconds`
  but no `clip` renders a card.
- `caption` lines become **one native SRT caption track** during assembly —
  editable in Premiere's Captions panel. That covers sound-off viewers
  without authoring an external SRT for short pieces.
- Ducking is **not** applied automatically. After each assemble, run
  `premiere_duck_music_under_dialogue` (default 12 dB) — see the loop in §5.
- The assembler seeds sequence dimensions from the **first clip**, not from
  `sequence.width/height` (those only size the text canvas). Vertical
  sources make this a non-issue; see §6 if yours aren't.

### The alternative: `duplicate_sequence` (post-assembly variation)

Instead of N storyboards, assemble the base **once**, then per variant:

1. `premiere_duplicate_sequence` (fast clone) → `premiere_rename_sequence`
   to `EXP014-hookB`.
2. On the copy: remove the hook text clip (`premiere_remove_clip` on the
   text track) and `premiere_add_text_layer` with the new line — canvas
   `width: 1080, height: 1920`, same position and timing. (Baked text is
   never editable in place; replace means delete + re-add.)
3. For a music test: `premiere_remove_clip` on the music track +
   `premiere_add_music_bed` with the other track, then re-duck.

**When to prefer which:**

- **Storyboards (default):** the variant set is your reproducible source of
  truth; re-running the whole batch later is one command per file; diffs are
  reviewable. Use for anything you might repeat, and always for pacing tests
  (re-trimming a timeline by hand is slower than editing `trim` values).
- **duplicate_sequence:** you already hand-polished the base cut beyond what
  the storyboard expresses (color, keyframes, transform tweaks) and don't
  want to lose that polish, or you need one quick extra variant mid-session.
  Costs: the variant exists only in the project (no diffable source), and
  surgical clip swaps are more error-prone than a fresh assemble. Snapshot
  before and after (`premiere_snapshot_timeline` / `premiere_what_changed`)
  so the change is auditable.

Remember: **every `premiere_assemble_storyboard` call creates a brand-new
sequence** — it never updates one in place. Re-running a fixed storyboard
leaves the broken sequence behind; delete it (§9).

---

## 5. The batch run loop

The exact tool order for one experiment batch. Steps marked **[serial]**
occupy the single Premiere lane; **[async]** runs in AME/background while you
continue.

### Phase 0 — pre-flight (once per session)

1. `premiere_ping` / `premiere_health_check` — bridge alive, project open.
2. `premiere_warm_encoder` — **do this first, always.** Cold AME takes 1–2
   minutes on the first job; warm, a short vertical render is seconds.
   Warming during setup means your first export doesn't stall the batch.
3. `premiere_get_project` (or `premiere_open_project` /
   `premiere_new_project` at an ASCII path).
4. Ingest per §3: `premiere_scan_assets` → confirm every source is
   1080×1920 → `premiere_import_files` with `target_bin`.

### Phase 1 — validate everything before assembling anything

5. For **every** variant file: `premiere_storyboard_validate`. It dry-runs
   clip resolution and reports unresolved or ambiguous references without
   touching the timeline. Fix all storyboards until all validate clean — a
   name that's ambiguous in hookA is ambiguous in all five variants, so
   catch it once, before you've assembled anything.

### Phase 2 — per-variant loop (control first)

For each variant, in order:

6. **[serial]** `premiere_assemble_storyboard` with the variant file. Read
   the **per-shot report**: every shot must be `placed` (or `text_card`);
   transitions are applied-or-reported. Any `skipped_unresolved` or `failed`
   shot = stop, fix, re-assemble (and delete the bad sequence — each run
   makes a new one).
7. **[serial]** `premiere_duck_music_under_dialogue` — ducking is never
   auto-applied by assembly.
8. **[serial, cheap]** First-frame check: `premiere_capture_frame_base64`
   at t=0 (~8s). This frame is the default cover on both platforms — hook
   text must be legible and inside safe areas (§6).
9. **[serial → async]** Full QC on the *first* variant of the batch only
   (the others differ by one known dimension):
   `premiere_export_preview` (low-bitrate MP4; treat `queued_not_confirmed`
   honestly — poll the file) → `premiere_generate_contact_sheet` on the
   preview file, and eyeball pacing, captions, text timing.
10. **[serial]** `premiere_get_sequence_list` → find the `sequence_id` for
    `EXP014-hookB`. **Never guess the id** — export requires it, and
    assembling has just changed the sequence list.
11. **[async]** `premiere_export` with preset `h264_1080p`, the explicit
    `sequence_id`, and the deterministic output path
    `exports/EXP014/EXP014-hookB-v1.mp4`. The preset is Match-Source:
    because the sequence is 1080×1920, the file is 1080×1920. **"Success"
    means QUEUED in Adobe Media Encoder, not rendered.** Don't report done
    yet.
12. **[serial — overlaps step 11]** Start step 6 for the next variant. This
    is the throughput trick: AME renders variant N while Premiere assembles
    variant N+1. Assembly is the bottleneck; renders on a warm encoder
    essentially never make you wait.

### Phase 3 — collect, verify, deliver

13. Poll each output file until it exists and its size is stable — the file
    on disk is the only ground truth (`premiere_get_export_progress` and
    `premiere_get_render_queue_status` exist but expose no real queue state
    on Premiere 2026).
14. Per file: confirm duration and size (§7), then
    `premiere_post_file_to_slack` into the experiment thread (512 MB cap —
    never a concern for a 30s short; the file must live under the project
    dir or temp, which `exports/` satisfies; the ASCII-path rule from §3
    applies here).
15. `premiere_get_session_digest` → paste into the thread. Fill the
    production columns of the tracking sheet (§2).
16. `premiere_save_project`.

### Error handling quick reference

| Symptom | Meaning | Fix |
|---|---|---|
| `storyboard_validate`: unresolved clip | Name matches nothing (exact → case-insensitive → basename → substring all failed) | Fix the name, or reference the on-disk path (paths auto-import) |
| `storyboard_validate`: ambiguous, lists candidates | Substring matches multiple project items | Use a longer unique name or the full path; consider renaming project items |
| Assemble report: `skipped_unresolved` / `failed` shot | A bad reference slipped past validation (e.g., project changed since) | Fix storyboard, **delete the partial sequence**, re-assemble |
| Export returns success but no file appears | Job is queued or died in AME | Wait and re-poll the file; if AME died, `premiere_warm_encoder` and re-export |
| Preview says `queued_not_confirmed` | Render not verified complete | Poll the preview file before contact-sheeting it |
| Slack post fails on the path check | Non-ASCII / NFD project path bug | `premiere_save_project_as` to an ASCII path, re-export or move the file under it |

---

## 6. Vertical (9:16) gotchas checklist

Exports follow the **sequence**, and the storyboard assembler follows the
**first clip**. Everything on this list flows from that.

- [ ] **Sources are already 1080×1920.** The storyboard path seeds sequence
  dimensions from the first clip and *ignores* `sequence.width/height` for
  the timeline. There is no working auto-reframe. Shoot vertical, or crop to
  1080×1920 before import (external tools are fine). Verify with
  `premiere_scan_assets` before assembling.
- [ ] **If you're stuck with horizontal sources**, skip the assembler:
  `premiere_create_sequence(name, 1080, 1920, fps)` creates a vertical
  sequence dialog-free; build into it manually with `premiere_insert_clip` /
  `premiere_overwrite_clip`, then `premiere_fit_clip_to_frame` or
  `premiere_scale_all_clips_to_frame` (accept the center-crop), then the
  text/caption/music tools. Slower, and you lose the storyboard
  conveniences — prefer fixing the sources.
- [ ] **Text canvas = 1080×1920.** `premiere_add_text_layer` bakes a PNG at
  the given `width`/`height`; if that doesn't match the sequence, text lands
  scaled or misplaced. In storyboards, the `sequence` block sets this — keep
  it `1080 × 1920` even though the timeline ignores it.
- [ ] **Safe areas** (no guides in the tool — enforce by convention, and
  note TikTok and Reels chrome differ slightly): keep text out of the top
  ~220px (status bar / notch), the bottom ~500px on TikTok and ~640px on
  Reels (username, audio, caption overlay), and the right ~170px action
  rail. In normalized terms: `x` between ~0.1 and ~0.85, hook headline
  around `y` 0.2–0.35.
- [ ] **Audio target −14 LUFS** for social. The server can set levels
  (`premiere_normalize_all_audio`, `music.level_db`,
  `premiere_set_audio_level`) but **cannot measure loudness** — verify on
  the exported file with `ffmpeg -i out.mp4 -af ebur128 -f null -` if
  compliance matters (appendix).
- [ ] **Respect minimum durations**: nothing shorter than ~3 frames / 0.1s —
  trims, text `duration_seconds`, `transition_after.duration_seconds`.
- [ ] **Cover frame = frame zero** unless someone manually picks a cover at
  post time. Design the first frame *as* the cover: hook text visible at
  `start_offset_seconds: 0`, checked via `premiere_capture_frame_base64` at
  t=0 (step 8 of the loop). If the cover is not the tested variable, pin the
  same cover on every variant at post time.
- [ ] **Duration sweet spots**: ~8–40s for TikTok, ~7–35s for Reels,
  regardless of higher platform caps. Videos under ~5s produce
  loop-inflated completion metrics — exclude them from completion
  comparisons.

---

## 7. QC gates before posting

Run per variant; a variant that fails any gate does not get posted:

1. **Contact sheet reviewed** (`premiere_generate_contact_sheet` on the
   preview export) — shot order, pacing, no black gaps, no stray frames.
2. **First frame checked** at t=0 — legible hook, safe areas, this is the
   cover you want.
3. **Ducking applied** — `premiere_duck_music_under_dialogue` ran after
   assembly (check the session digest if unsure); spot-check the preview
   with sound.
4. **Captions present** — the SRT caption track exists and lines match VO.
5. **Duration within platform limits and within the experiment's design** —
   a hook test with variants of different length is a confounded test; keep
   all variants within ±0.5s unless length is the variable.
6. **Sound-off pass** — watch the preview muted; the video must still make
   sense (most feed viewing is muted).
7. **File sanity** — output exists, size stable, plays; H.264 at a healthy
   bitrate (platforms recompress everything — feeding them a thin file
   compounds the loss); under 512 MB for Slack delivery (a 30s short is
   tens of MB).
8. **Named correctly** — sequence name, filename, and tracking-sheet row all
   carry `EXP###-<variant>-v#`.
9. **Clean master** — no TikTok watermark, no variant-code overlay, nothing
   burned in that identifies it as a test asset. (Never repost a
   TikTok-watermarked download to Reels — Instagram downranks it; export
   the clean master for each platform from Premiere instead.)

---

## 8. Reading results and iterating

At the 72-hour read (per the §1 thresholds):

1. **Fill the metrics columns** in the tracking sheet; compute the primary
   metric per variant. Take the 7d sanity snapshot later — decide at 72h;
   if 7d flips the ranking repeatedly for some variable, lengthen that
   variable's judgment window instead of cherry-picking.
2. **Declare** `winner` / `tie` / `loser` / `invalid` per row, using the §1
   thresholds. Ties keep the control. Outlier delivery (≥5× median views on
   either variant) = invalid, rerun.
3. **Read the retention curves** of both posts and write down what second
   people left at — that's the next experiment's hypothesis.
4. **Promote the winner to base:** copy the winning variant file to the next
   experiment's `base.json` (`storyboards/EXP016/base.json`), note the
   lineage in the tracking sheet (`"base = EXP014-hookB"`). The winner is
   the new control.
5. **Archive losers, don't hoard them.** Storyboard files stay under
   `storyboards/EXP014/` forever (cheap, diffable). In the Premiere project,
   delete losing sequences or archive the whole experiment (§9) — a project
   with forty stale sequences makes clip resolution and sequence-index
   handling error-prone.
6. **Write the learning as a sentence,** not a number: "Question hooks beat
   statement hooks by 21% on 3s-hold for demo content." A pattern becomes
   canon after **3 independent wins**; date-stamp anything that won during a
   trend window (trend-contaminated wins don't replicate).
7. **Re-test occasionally.** Creative learnings decay — trends, audience,
   and the algorithm shift. Re-validate a load-bearing learning roughly
   quarterly, when the account's baseline moves, or when entering a new
   content format. Also re-test any winner that was declared near the
   threshold.

Move up the leverage ladder as tests conclude: once the hook is stable, test
cover frames against that hook; then CTA; and so on.

---

## 9. Appendix: limits and workarounds

Each of these limits has a corresponding feature request in
[feature-requests-shorts-ab.md](feature-requests-shorts-ab.md) (FR numbers
below).

| Limit (today) | Workaround | FR |
|---|---|---|
| **No auto-transcription / auto-captions** | Author SRTs externally (e.g., Whisper on the VO audio) into `srt/`, then `premiere_add_subtitles_from_srt`. For shorts, per-shot `caption` lines in the storyboard are usually enough and need no external file. Each SRT import creates a *new* caption track and there is no read-back — import once; correct in the SRT file, not in Premiere. | FR-07 |
| **No loudness measurement** | Set levels via the tools (target ~−14 LUFS), then measure the exported file: `ffmpeg -i EXP014-hookB-v1.mp4 -af ebur128 -f null -` and read the integrated LUFS. Adjust `music.level_db` / `premiere_normalize_all_audio` and re-export if off by more than ~1 LU. | FR-06 |
| **Storyboard ignores declared width/height; no auto-reframe; all "vertical/TikTok" convenience tools broken or unverified** | Vertical sources (§6). Fallback: manual build into a `premiere_create_sequence(1080, 1920)` sequence with `fit_clip_to_frame` / `scale_all_clips_to_frame`. | FR-01, FR-02 |
| **No variant/experiment concept in the schema** | File-copy convention + manual diff discipline (§4). | FR-03, FR-11 |
| **Baked-PNG text is not editable; single-line; fixed stroke/shadow; no safe-area guides** | To change text: remove the text clip and `premiere_add_text_layer` again — or re-assemble the corrected storyboard. Multi-line = stack layers. Safe areas by convention (§6). | FR-09 |
| **Slack upload fails on non-ASCII (NFD) project paths; 512 MB cap** | ASCII-only project trees; `premiere_save_project_as` to an ASCII path to rescue an existing project. | FR-12 |
| **Every assemble creates a new sequence** | Project hygiene: after a failed assemble, delete the orphan. Sequence management is by **0-based index** (`premiere_get_sequence_list` → `premiere_delete_sequence`), and **indices shift after every delete** — re-list before each destructive call, delete one at a time. For closed experiments, `premiere_save_project_as` an archive copy, then prune the working project. | FR-10 |
| **Export "success" = queued in AME; no real progress API** | Always poll the output file (existence + stable size) before declaring done or posting. `premiere_warm_encoder` once per session so the first render isn't a 1–2 minute cold start. | FR-04, FR-05 |
| **Serial timeline engine** | One Premiere = one lane. Don't attempt parallel assembly; overlap AME renders with the next assembly instead (§5, step 12). Frame captures (~8s each) and previews are the expensive QC ops — full QC on the first variant, first-frame-only on the rest. | FR-13 |

---

*Related docs:* [STORYBOARD_SCHEMA.md](STORYBOARD_SCHEMA.md) (full field
reference, CSV shot lists, script parsing) ·
[SLACK_VIDEO_EDITING_GUIDE.md](SLACK_VIDEO_EDITING_GUIDE.md) (driving all of
this from Slack) ·
[feature-requests-shorts-ab.md](feature-requests-shorts-ab.md) (the roadmap
that removes the workarounds above) · `config://premiere-instructions` (the
server's own golden-workflow resource).
