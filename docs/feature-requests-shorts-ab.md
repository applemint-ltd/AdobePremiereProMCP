# Feature Requests: Bulk 9:16 Shorts Production and A/B Testing

**Audience:** maintainers of this MCP server.
**Companion doc:** [SHORTS_AB_TESTING_PLAYBOOK.md](SHORTS_AB_TESTING_PLAYBOOK.md) — the user-facing workflow that works with *today's* tool surface. Every limitation that playbook routes around appears below as a feature request.

## Purpose and target workflow

The target user mass-produces 9:16 short videos (Instagram Reels / TikTok) and runs creative A/B experiments: one base cut, N variants differing in exactly one dimension (hook text, music, pacing, CTA), exported in a batch, delivered via Slack, posted manually, judged against platform analytics. Today's golden path (storyboard → validate → assemble → preview → export via AME → Slack) covers the single-video case well; this document lists what is missing for the *bulk, vertical, experiment-driven* case. [competitive-analysis.md](competitive-analysis.md) already frames social auto-reframe and Reels/TikTok caption tooling as competitive gaps — these requests are the concrete backlog behind that positioning.

What already works well and should be built on, not around: the honest audit spine (correlation IDs, pre-mutation snapshots, `get_session_digest`), the storyboard validator's dry-run clip resolution, `warm_encoder`, and `export_preview`'s honest `completed` vs `queued_not_confirmed` reporting.

## Summary table

| ID | Title | Priority | Type | Effort | Risk | Depends on |
|----|-------|----------|------|--------|------|------------|
| FR-01 | Vertical-first sequences: storyboard honors declared width/height | P0 | Bug fix | M | Low-Med | — |
| FR-02 | Verify and promote `premiere_auto_reframe` (16:9 → 9:16) | P0 | Verify & promote | S | High | — |
| FR-03 | Variant matrix: base storyboard + overrides → N storyboards | P0 | New build | S-M | Low | FR-01 |
| FR-04 | Batch export with honest per-file completion | P0 | New build (Go) + bug fix | M | Med | FR-05 |
| FR-05 | File-watch export status primitive (`await_exports`) | P0 | New build | S | Low | — |
| FR-06 | LUFS measurement via media engine (ebur128) | P1 | New build | M | Low | — |
| FR-07 | Auto-captions: Whisper transcription → SRT → import | P1 | New build | M-L | Med | — |
| FR-08 | Storyboard v2: color, speed, transform, end-card, cover frame, hook refs | P1 | New build | M | Med | FR-01 |
| FR-09 | Text layer v2: styling params, wrap, box, platform safe-area presets | P1 | New build | S-M | Low | — |
| FR-10 | Idempotent assembly: replace-mode re-assembly | P1 | New build | M | Med | — |
| FR-11 | Experiment manifest, tracking-sheet rows, filename convention | P1 | New build | S-M | Low | FR-03, FR-04 |
| FR-12 | Slack delivery fixes: NFC/NFD path bug, >512MB preflight | P1 | Bug fix | S | Low | — |
| FR-13 | Cheap multi-frame capture, source thumbnails, 3s hook previews | P2 | New build | S-M | Low-Med | — |
| FR-14 | Variant A/B review composite (rescoped from `setup_ab_comparison`) | P2 | New build | S | Low | FR-03 |
| FR-15 | `deliver_short` macro; review watermarks; delete fake-success social stubs | P2 | New build + cleanup | M | Low | FR-01, 04, 06, 12 |
| FR-16 | Slack metrics-snapshot reminders (72h / 7d) | P2 | New build | S | Low | FR-11 |

Effort: S = under a day, M = 1–3 days, L = a week-plus. Risk flags throughout: Premiere 2026 ExtendScript API uncertainty, CEP-panel-reload cost for `.jsx` changes, modal-dialog engine freezes.

---

## Wave 1 — P0: unblocks bulk 9:16 production

### FR-01 — Vertical-first sequences: storyboard must honor declared width/height

- **Priority:** P0 — every Reel/TikTok is 9:16; today the golden storyboard path silently produces sequences at the first clip's (usually 16:9) dimensions, so the core use case fails at step one.
- **Pain today:** The editor declares `sequence: {width: 1080, height: 1920}` in the storyboard JSON, assembles, and gets a landscape sequence. Text PNGs are sized to the *declared* dims while clips follow the *actual* dims, so overlays mis-scale too. The workaround is manual sequence surgery per video — untenable at 20+ variants/day.
- **Current state:** `storyboard/v1` declares `sequence.width`/`height` (`go-orchestrator/internal/storyboard/schema.json:17-18`) but the assembler seeds sequence dims from the first clip via `createSequenceFromClips` (`go-orchestrator/internal/orchestrator/storyboard_ops.go:199-221`), using declared dims only for the baked-PNG text canvas (`storyboard_ops.go:568-569`). Meanwhile `create_sequence` is now dialog-free (qe `newSequence` + bundled 1080p `.sqpreset` + best-effort resize), so vertical sequences are creatable today. Related: `set_sequence_settings` is registered (`internal/mcp/sequence_tools.go`) but **not** in the curated surface — only `get_sequence_settings` is exposed.
- **Proposed behavior:** When `sequence.width/height` are declared, the assembler creates the sequence via the dialog-free `create_sequence` path at declared dims, then inserts clips. Add optional `sequence.fit_mode: "scale_to_fill" | "scale_to_fit" | "center"` (default `scale_to_fill` for 9:16 from 16:9 sources), applied per clip after insert by reusing the exposed `scale_all_clips_to_frame` / `fit_clip_to_frame` logic. `storyboard_validate` warns when clip aspect differs from sequence aspect and states which fit_mode will apply. Sub-item: run the standard verification protocol on `set_sequence_settings` and promote it if sound (it is the natural post-hoc rescue tool). Document (verified) that Match-Source export presets inherit sequence dims.
- **Acceptance criteria:** (1) A storyboard declaring 1080×1920 with 16:9 sources produces a sequence whose `get_sequence_settings` reports 1080×1920. (2) `capture_frame_base64` shows full-bleed vertical framing under `scale_to_fill`. (3) Text layers land at the declared relative positions. (4) `premiere_export` of that sequence with an H.264 Match-Source preset yields a 1080×1920 file (media-engine probe). (5) Omitting `sequence.width/height` preserves current first-clip behavior — no regression.
- **Effort/Risk:** M. Low-Med — mostly Go-side reordering; a host-side change would cost a `.jsx` edit + CEP panel reload. Verify 2026's auto-scale behavior when inserting into a differently-sized sequence.
- **Type:** Bug fix (declared schema contract not honored) + small enhancement (`fit_mode`).

### FR-02 — Verify and promote `premiere_auto_reframe` (Sensei 16:9 → 9:16)

- **Priority:** P0 — `scale_to_fill` center-crops; Sensei auto-reframe tracks the subject, which is the difference between usable and unusable vertical crops of 16:9 masters. [competitive-analysis.md](competitive-analysis.md) names this as a competitor feature.
- **Pain today:** Converting existing 16:9 masters to 9:16 requires manual Auto Reframe in the UI per sequence — the most time-consuming step when repurposing a horizontal library into Reels.
- **Current state:** The tool exists end-to-end (`go-orchestrator/internal/mcp/sequence_tools.go:288-305, 802-821` → `internal/orchestrator/sequence.go:342` → host `autoReframeSequence` in `cep-panel/src/host/premiere.jsx`) but is long-tail (not in the curated set in `internal/mcp/core_toolset.go`) and **unverified on Premiere 2026**.
- **Proposed behavior:** Run a verification pass on 2026: does the call complete headlessly, does it open a modal (engine-freezing trap), is it synchronous or does it return before Sensei analysis completes, does it create a new sequence or mutate in place? If it passes: promote into the curated set with params `{sequence_id, target_aspect: "9:16"|"1:1"|"4:5", motion_preset: "slower"|"default"|"faster", nest_clips?: bool}`, returning the new sequence id, with documented expected duration and an explicit timeout param. If it fails or modals: record the verdict in `docs/working-log-v2.md` and make FR-01's `scale_to_fill` the supported path.
- **Acceptance criteria:** (1) A 60s 16:9 sequence auto-reframes to 9:16 with no dialog and no engine freeze beyond the documented duration. (2) Result is 1080×1920 and the subject remains framed across at least one hard cut (checked via `capture_frame_base64` at two timecodes). (3) Tool registered in `core_toolset.go` with a verification date, **or** a written verdict lands in working-log-v2.
- **Effort/Risk:** S effort, HIGH risk — unverified 2026 surface, likely modal/async traps. The verification outcome itself is the deliverable.
- **Type:** Verify & promote existing long-tail tool.

### FR-03 — Variant matrix: `premiere_storyboard_expand_variants`

- **Priority:** P0 — this is the A/B mechanism itself; without it every variant is a hand-edited JSON copy with drift and naming chaos.
- **Pain today:** To test 3 hooks × 2 music beds the editor hand-edits 6 storyboard JSONs, mis-numbers one, and can no longer tell which exported file carried which treatment. There is **no** variant/experiment concept anywhere in the codebase.
- **Current state:** No infrastructure. Assembly is `storyboard_schema → storyboard_validate → assemble_storyboard` (`internal/orchestrator/storyboard_ops.go`); each assemble creates a new sequence with no naming discipline.
- **Proposed behavior:** Server-side *expansion*, client-side *assembly loop*. New tool `premiere_storyboard_expand_variants` — a pure-Go JSON transform, no host calls. Input: `{base: <storyboard>, experiment_id, variants: [{variant_id, label, overrides: <JSON merge patch or [{path, value}] ops>}], naming_template: "{experiment_id}-{variant_id}"}`. Output: N fully-validated storyboards, each stamped `meta.experiment_id`/`meta.variant_id` with a deterministic sequence name. The agent then loops `assemble_storyboard` (assembly is serialized by the single ExtendScript engine anyway, so a host-side batch tool buys nothing and risks long uninterruptible host jobs).
  **Included: the variant-diff gate.** A validation mode that checks the variants of one experiment differ from base in **exactly one field path** (configurable allowlist, e.g. `sequence.name` always exempt) and errors with the full diff list otherwise. This turns "one variable per experiment" from a guideline into a gate — and specifically catches an AI agent "improving" trims or transitions between variants, which silently destroys the experiment.
- **Acceptance criteria:** (1) Base + 3 override sets → 3 storyboards that each pass `storyboard_validate`. (2) Overrides can target shot clip, trim, duration, text content/style, music path/level, transitions via path addressing. (3) Sequence names follow the template and collide-check against `get_sequence_list`. (4) Invalid override paths fail loudly at expansion time, not assembly time. (5) A variant set where B differs from base in two field paths fails the diff gate with both paths named. (6) Experiment/variant ids survive into sequence names and the audit log.
- **Effort/Risk:** S-M, low — pure Go, no ExtendScript, no panel reload.
- **Type:** New build.

### FR-04 — Batch export over sequences with honest per-file completion

- **Priority:** P0 — at 5+ variants per experiment, export is the throughput bottleneck; today there is no exposed batch export and the hidden ones lie about completion.
- **Pain today:** The editor exports variants one `premiere_export` call at a time, babysits AME, and cannot trust any success signal.
- **Current state:** the hidden long-tail `batch_export_sequences` / `export_all_sequences` (`internal/mcp/batch_tools.go`) are **completely dead on 2026, not merely dishonest**: their host functions (`premiere.jsx:9102+`) call `seq.exportAsMediaDirect`, which returns success but writes nothing (verified live, working-log-v2 §2). So the rework is a **pure-Go rebuild** over the one verified render path (the AME-queueing `exportSequence` host call) — no host edit, no panel reload. `get_render_queue_status` is near-stub; `get_export_progress` has no real percentage. The reliable pattern already exists internally: `awaitExportedFile`, used by `export_preview`. Separate bug: `premiere_export`'s orchestrator hard-requires `sequence_id` (`orchestrator.go:353`) though its schema implies it's optional.
- **Proposed behavior:** Promote and rework `premiere_batch_export_sequences`: `{sequence_ids: [] | name_glob, preset, output_dir, filename_template: "{sequence_name}.mp4", warm_encoder: bool = true, await: "all"|"none", per_file_timeout_s, overwrite: bool}`. Semantics: warm AME once up front, queue all jobs, then confirm completion per file via FR-05 — success is reported **only** when the output file exists and is size-stable. Returns per-sequence `{status: done|timeout|failed, path, bytes, wall_s}`. Included fix: make `premiere_export`'s `sequence_id` genuinely optional (default: active sequence) to match its schema.
- **Acceptance criteria:** (1) Exporting 5 sequences returns only after all 5 files exist and have stopped growing, or per-file timeout. (2) A deliberately failing job (bad output dir) reports `failed` for that file and `done` for the rest — no fake success. (3) Warm-encoder path absorbs the 1–2 min cold start before the first job. (4) `premiere_export` without `sequence_id` exports the active sequence. (5) Results audit-logged with file sizes.
- **Effort/Risk:** M. Medium — AME queue behavior on 2026 and the known success-before-write failure mode; mitigated by trusting only the filesystem.
- **Type:** New build (Go), replacing the dead host batch tools + bug fix (`sequence_id`).

### FR-05 — File-watch export status primitive: `premiere_await_exports`

- **Priority:** P0 (as FR-04's dependency; standalone value P1) — polling output files is the only ground truth for export completion on 2026.
- **Pain today:** After queuing an export the agent has no honest way to answer "is it done?", so agents guess with sleeps.
- **Current state:** `awaitExportedFile` exists internally for `export_preview` (bounded wait, 3 stable polls, ffmpeg-probeable check); `get_render_queue_status` ≈ stub; `get_export_progress` has no real percent.
- **Proposed behavior:** New Go-side tool `premiere_await_exports`: `{paths: [], stable_for_s: 3, timeout_s, poll_interval_s}` → per path `{exists, bytes, growing, stable, done}`. Non-blocking sibling `premiere_export_status` returning a single snapshot (bytes so far + growth rate — honest even without a percent). No ExtendScript involvement. FR-04 consumes this internally; exposed standalone so single `premiere_export` calls can also be awaited.
- **Acceptance criteria:** (1) `done` only after the file has existed and been size-stable for `stable_for_s`. (2) `growing` while AME writes. (3) Times out cleanly with partial state, not an error. (4) Works for files outside the project dir (export target dirs).
- **Effort/Risk:** S, low — pure Go filesystem watching, generalizing an existing internal pattern.
- **Type:** New build.

---

## Wave 2 — P1: makes A/B testing real and trustworthy

### FR-06 — LUFS measurement via the Rust media engine (ffmpeg ebur128)

- **Priority:** P1 — platforms normalize to about −14 LUFS; the editor can *set* loudness but cannot *verify* it, so pre-post QC is blind.
- **Pain today:** After assembling music + dialogue there is no way to confirm the mix; over-loud uploads get crushed by platform normalization, under-loud ones sound weak. `get_sequence_loudness` returns stub data (track counts + a "requires media engine" note), and `set_audio_gain`/`set_audio_track_volume` are broken on 2026, so blind correction is doubly risky.
- **Current state:** The pieces all exist: the Rust engine already shells ffmpeg (scan/thumbnails/scene-detect) and `export_audio_only` is exposed.
- **Proposed behavior:** New tool `premiere_measure_loudness`: `{sequence_id? | file_path?, target_lufs: -14, tolerance_lu: 1.0}`. Sequence path: `export_audio_only` → temp WAV → FR-05 await → media-engine `ffmpeg -filter_complex ebur128` → `{integrated_lufs, true_peak_dbtp, lra, pass, delta_lu}`. File path: analyze the exported MP4 directly — the cheap post-export QC path. Optionally wire `pass/delta` into FR-11's manifest per variant.
- **Acceptance criteria:** (1) Measured integrated LUFS on a reference file matches ffmpeg CLI within 0.5 LU. (2) Sequence round-trip completes in under ~30s for a 60s sequence. (3) `pass:false` carries a signed `delta_lu`. (4) Temp WAVs land in the scratch area and are cleaned up.
- **Effort/Risk:** M, low-med — Rust/Go plumbing; the only host dependency is already exposed.
- **Type:** New build (replaces a stub with a real implementation).

### FR-07 — Auto-captions: Whisper transcription → SRT → caption track

- **Priority:** P1 — captions are near-mandatory for Reels/TikTok retention; today every variant with dialogue needs a hand-authored SRT. Already an open follow-up in `docs/working-log-v2.md`.
- **Current state:** Captions work only via SRT import (`add_subtitles_from_srt`; one new track per import; no read-back on 2026; no transcription anywhere). Storyboard `caption` fields already compile into an SRT.
- **Proposed behavior:** New tool `premiere_transcribe_to_srt`: `{source: file_path | sequence_id, language: "auto"|"ja"|"en"|…, model: "small"|"medium"|"large-v3", max_chars_per_line: 16 (CJK-aware), max_lines: 1, srt_out_path}`. Sequence source = `export_audio_only` + FR-05 await, then whisper.cpp (vendored or discovered binary; model downloaded and cached on first use) in the Python intelligence layer or a sidecar. Convenience wrapper `premiere_auto_caption`: transcribe → `add_subtitles_from_srt` → optional `set_caption_position` into the 9:16 safe area. Japanese transcription quality is a first-class acceptance concern (the primary user's environment).
- **Acceptance criteria:** (1) A 60s dialogue clip yields an SRT usably accurate in both English and Japanese with per-cue timing ±0.3s. (2) Line lengths respect `max_chars_per_line` for CJK. (3) The wrapper lands cues on a caption track inside the 9:16 safe area. (4) Model download happens once, is cached; download failure is a clear error, not a hang. (5) Fully offline after first model fetch.
- **Effort/Risk:** M-L, medium — new binary/model dependency, runtime cost on long clips, CJK segmentation. Zero 2026 API risk (the import path already works).
- **Type:** New build.

### FR-08 — Storyboard v2: per-shot color/speed/transform, end-card, cover frame, hook refs

- **Priority:** P1 — A/B treatments are mostly look/pacing/CTA/cover changes; if the storyboard can't express them, FR-03's override matrix can't vary them.
- **Pain today:** After assembly the editor manually applies LUTs, speed changes, and builds an end-card per variant — exactly the repetitive per-variant work the storyboard exists to eliminate. The schema is closed (`additionalProperties:false`), so nothing can be smuggled in.
- **Current state:** `storyboard/v1` (`internal/storyboard/schema.json`): shots have clip/duration/trim/text (3 hardcoded styles)/caption/transition; music at sequence level. No color, speed, transform, end-card, or cover concept. The primitives all exist as exposed tools: `lumetri_apply_lut`, `lumetri_set_*`, `set_clip_speed`, `set_scale`/`set_position`, `add_text_layer`, `freeze_frame`, `export_frame`.
- **Proposed behavior:** Bump to `storyboard/v2` (assembler accepts both versions):
  - Per-shot `color: {lut_path?, exposure?, contrast?, saturation?, temperature?}` + sequence-level `color.lut_path` default with per-shot override.
  - Per-shot `speed: {rate: 1.0, maintain_pitch: bool}` and `transform: {scale?, position?: [x,y]}`.
  - Scene-level `end_card: {duration_s, background: color|clip, text: [...], cta_text}` assembled from existing text-layer + freeze/color primitives.
  - **`cover: {shot_id, offset_seconds}`** — the intended cover/thumbnail frame. On export (or via FR-15), the pipeline also emits a 1080×1920 cover PNG per variant and delivers it alongside the MP4. Rationale (from the growth side): covers drive grid/search traffic, the platform default derives from early frames, and an un-pinned cover is the #1 accidental second variable in hook tests — changing the opening shot silently changes the cover.
  - **`text[].hook_ref`** — optional stable ID referencing a reusable hook/CTA snippet bank (a simple JSON/CSV library). Lets the tracking layer (FR-11) accumulate a win/loss league table *per hook ID* across experiments, which is what makes "promote a pattern after 3 independent wins" possible without archaeology.
  Each new field maps 1:1 onto an already-exposed tool call during assembly. `storyboard_validate` checks LUT/cover/hook-ref existence and speed bounds at dry-run.
- **Acceptance criteria:** (1) v1 storyboards assemble unchanged. (2) A v2 storyboard with per-shot LUT + 1.15× speed + end-card assembles with no manual follow-up; `dump_sequence_state` confirms speed and Lumetri per clip. (3) FR-03 overrides can target every new field. (4) Validation rejects missing LUT paths at dry-run, not mid-assembly. (5) `cover` produces a PNG whose frame matches the declared shot/offset. (6) Per-shot assembly cost stays bounded and documented (each field = a few serialized host calls).
- **Effort/Risk:** M, medium — more serialized host calls per assemble; Lumetri scripting quirks on 2026 (working-log-v2 tracks a `lumetri_get_all` escaping issue); schema versioning discipline.
- **Type:** New build (composition of verified tools under the schema).

### FR-09 — Text layer v2: parameterized styling, wrap, background box, platform safe-area presets

- **Priority:** P1 — hook text is the highest-leverage A/B variable, and today's renderer cannot produce platform-native-looking text or guarantee it clears TikTok/Instagram UI chrome.
- **Pain today:** Multi-line hooks must be baked as separate stacked layers; every layer carries a forced black stroke and fixed shadow; there is no rounded background box (the dominant TikTok caption look); canvas dims must be passed manually; nothing warns when text lands under the like/share rail or the caption zone. Since text is baked, a safe-area mistake is unfixable post-export.
- **Current state:** `go-orchestrator/assets/render_text_layer.swift` (verified): args `outputPath,width,height,text,fontName,fontSize,hexColor,x,y,[orientation]`; stroke hardcoded black −3.0; shadow hardcoded (0.8-alpha black, blur 6); alignment forced center; single-line horizontal (vertical mode = one char per line); no alpha in colors; no wrap. Canvas dims are caller-supplied, not derived from the sequence. Storyboard text styles are 3 hardcoded names. No safe-area logic anywhere.
- **Proposed behavior:** Extend the renderer CLI (structured JSON arg) and `add_text_layer` params: `stroke: {color, width} | none`, `shadow: {…} | none`, `background: {color, opacity, padding, corner_radius} | none`, `align: left|center|right`, `max_width_frac` (wrap via `draw(in:)`), `line_spacing`, alpha-capable colors; auto-default `canvas` to the active/target sequence dims. Ship named presets as bundled JSON — `reels_hook`, `tiktok_caption`, `cta_button` — each pinning font/size/box/position inside a defined safe area. Safe-area definitions per platform on a 1080×1920 canvas (TikTok and Reels chrome differ): keep text out of the top ~220px, bottom ~500px (Reels ~640px — its username/audio/caption overlay reaches higher), and right ~170px action rail; `safe_both` = the intersection. New helper `premiere_check_text_safe_area {sequence_id, platform}` flags layers outside the zone; optional debug flag on `capture_frame_base64` composites safe-area guides onto the returned frame. Wire the presets into the storyboard `text.style` enum (FR-08).
- **Acceptance criteria:** (1) Multi-line wrapped text with a rounded semi-opaque box renders at exact requested pixel dims. (2) `stroke:none, shadow:none` produces clean text — nothing hardcoded remains. (3) Emoji render correctly (explicit test; AppKit fallback assumed but unverified). (4) Canvas defaults to sequence dims with no manual param. (5) A preset-placed hook on 1080×1920 sits fully inside the platform safe area, confirmed by the check tool. (6) Existing calls behave identically (back-compat defaults = current hardcoded values).
- **Effort/Risk:** S-M, LOW — Swift + Go only; no ExtendScript change, no panel reload; renderer testable headlessly.
- **Type:** New build (extending the existing renderer).

### FR-10 — Idempotent assembly: replace-mode / re-assemble in place

- **Priority:** P1 — the A/B loop is iterate-review-tweak; today every tweak spawns a new orphan sequence, and after a morning of iteration the project is a graveyard of near-duplicates.
- **Pain today:** Editor tweaks one hook line, re-assembles, and gets `EXP014-hookB` next to the old `EXP014-hookB`… except it's actually a new sequence with the same name; exports then target the wrong one. Manual sequence deletion is constant housekeeping.
- **Current state:** Assembly is not idempotent — every `assemble_storyboard` creates a NEW sequence (`storyboard_ops.go`); the schema's own description says "Sequence to create **or reuse**" (`schema.json:16`), so this is also a doc/behavior mismatch. Supporting pieces exist: auto pre-mutation snapshots, `snapshot_timeline`/`compare_timeline_snapshots`/`diff_timeline`, `delete_sequence`, `rename_sequence`, FR-03's deterministic naming.
- **Proposed behavior:** Add `mode: "create" | "replace"` (default `create`) and `target: {sequence_id? | sequence_name?}` to `assemble_storyboard`. Replace semantics v1 (simple and safe): snapshot → delete-and-recreate under the SAME name → report `diff_timeline` old-vs-new in the response. Name resolution uses FR-03's naming template so re-running a variant converges on one sequence. True storyboard-diff minimal-edit re-assembly is explicitly deferred as future work — delete-and-recreate is acceptable because assembly is fast relative to review.
- **Acceptance criteria:** (1) Running the same storyboard twice in replace mode leaves exactly one sequence with that name. (2) The pre-replace snapshot is recorded and comparable. (3) Replace of a nonexistent target falls back to create with a warning, not an error. (4) Sequence-id churn is documented (delete-recreate changes ids; the response returns the new id). (5) The audit spine captures the replace as one logical operation.
- **Effort/Risk:** M, medium — delete/recreate ordering on the single-threaded host; must verify `delete_sequence` never trips a modal confirmation on 2026.
- **Type:** New build.

### FR-11 — Experiment manifest and report: variants → sequences → files → tracking rows

- **Priority:** P1 — A/B testing is worthless if you can't map the file you posted to the treatment it carried; this closes the loop FR-03 opens. Engineers tend to treat export as the pipeline's end; the *experiment* starts there.
- **Pain today:** After exporting 6 variants the editor keeps the treatment map in their head; a week later, when `variant_c.mp4` wins on TikTok, nobody can reconstruct what variant C changed.
- **Current state:** No experiment concept. The raw material exists: JSONL audit logs, `get_session_digest`, `what_changed`, FR-03's stamped ids, FR-04's per-file results.
- **Proposed behavior:** New Go-side tool `premiere_experiment_report`: `{experiment_id, output: json_path?, post_to_slack: bool}`. Aggregates audit log + expansion records + export results into: per variant `{variant_id, label, overrides_summary (human-readable diff vs base), sequence_name/id, output_file, bytes, sha256, duration_s, lufs (if FR-06 ran), exported_at}` plus an experiment header `{base_storyboard_digest, created_at, tool_versions}`. Writes the manifest JSON next to the outputs; optionally posts a compact table to Slack (via the fixed FR-12 path).
  **Included (growth side):** (a) **auto-emitted tracking-sheet row** — on each variant export, generate a CSV row pre-filled with everything the pipeline knows (experiment, variant, storyboard hash, duration, filename, the one-field diff) and post it in the experiment's Slack thread, leaving post-time fields (posted_at, caption, hashtags, sound, cover) blank for the human; (b) **filename convention baked into export**: `{experiment_id}-{variant_id}-v{n}.mp4` auto-derived, so the file on disk, the Slack upload, and the sheet row can never disagree.
- **Acceptance criteria:** (1) After a 3-variant run (FR-03 → assemble → FR-04), the report lists all 3 with correct file mappings and checksums. (2) `overrides_summary` reads as prose ("hook text: 'X' → 'Y'"). (3) Missing exports appear as `not_exported`, never omitted. (4) The Slack digest fits one message. (5) Manifest is valid JSON with a declared schema version. (6) The emitted CSV row matches the playbook's tracking-sheet columns.
- **Effort/Risk:** S-M, low — pure Go aggregation over existing logs.
- **Type:** New build.

### FR-12 — Slack delivery fixes: NFC/NFD path normalization + >512MB preflight

- **Priority:** P1 — Slack is the delivery last mile; a known real-world failure (Japanese project folder names) breaks it silently at the end of an otherwise successful pipeline.
- **Pain today:** Projects under Japanese-named folders fail `post_file_to_slack`'s allowlist because macOS stores NFD while the compared path holds NFC — the prefix check misses. The documented workaround is `save_project_as` to an ASCII path, which is absurd.
- **Current state:** `go-orchestrator/internal/orchestrator/slack_upload_ops.go`: path allowlist via `filepath.Rel` prefix comparison (project folder or `os.TempDir()`) with no Unicode normalization — the sole occurrence of this pattern in the repo. The **>512MB preflight already exists** (the file is stat'd before any network I/O and the error names the size, the cap, and a remediation), so this FR is smaller than it looks: the NFC/NFD fix plus regression fixtures.
- **Proposed behavior:** Apply NFC normalization (`golang.org/x/text/unicode/norm`, already in go.mod) to BOTH sides of the allowlist comparison, resolving symlinks consistently first. Add Unicode fixtures so the bug can't return.
- **Acceptance criteria:** (1) A file under a Japanese/NFD-named project dir uploads with no workaround. (2) Regression tests cover NFD-dir/NFC-config and the reverse. (3) The existing >512MB fail-fast behavior is regression-covered. (4) Allowlist semantics otherwise unchanged.
- **Effort/Risk:** S, low — localized Go change; needs Unicode test fixtures.
- **Type:** Bug fix (+ small enhancement).

---

## Wave 3 — P2: polish and throughput

### FR-13 — Cheap multi-frame capture, source thumbnails, and 3-second hook previews

- **Priority:** P2 — review of many variants is throttled by an ~8s-per-frame capture that renders a full H.264 preview each time; valuable but not blocking.
- **Pain today:** QC-ing 6 variants at 3 checkpoints each = 18 captures ≈ 2.5 minutes of serialized engine time, because `exportFramePNG` is gone on 2026 and each `capture_frame_base64` renders a preview via AME then extracts one frame — the same preview is re-rendered and discarded per frame.
- **Current state:** `capture_frame_base64` = AME preview render + ffmpeg extract. `generate_contact_sheet` exists (operates on a file). The Rust engine already does ffmpeg thumbnails for source files.
- **Proposed behavior:** Three cheap paths. (1) `premiere_capture_frames` (plural): `{sequence_id, timecodes: []}` — render ONE preview, ffmpeg-extract N frames from that single render; optionally cache the preview keyed on a timeline-snapshot hash so an unchanged sequence re-captures for free. (2) `premiere_thumbnail_media`: media-engine-only thumbnails of SOURCE files (`{file_path, timestamps, width}`) for shot selection — zero host involvement, runs in parallel with Premiere work. (3) **Hook-preview render** (growth side): `premiere_export_preview` gains `{in_s: 0, out_s: 3}` range params — export just the first 0–3s per hook candidate (seconds each on a warm encoder), enabling the "generate 5 hook candidates, human-review in Slack, post the best 2" pre-filter; companion hook contact sheet = frames at 0/1/2/3s across all candidates.
  Honest framing in docs: sequence-state capture fundamentally requires a render on 2026 — the win is amortization, not elimination.
- **Acceptance criteria:** (1) 5 frames from one sequence cost roughly one preview render, not 5×. (2) Frames match single-capture output for identical timecodes. (3) Source thumbnails return in <1s per file and never touch the ExtendScript engine. (4) The cache invalidates when the timeline changes (via `what_changed`). (5) A 0–3s preview of a 30s sequence renders in a few seconds warm and contains exactly the requested range.
- **Effort/Risk:** S-M, low-med — reuses existing preview + ffmpeg machinery; cache invalidation and work-area/in-out range handling are the subtleties.
- **Type:** New build (paths 1 and 3 refactor existing internals; path 2 is media-engine-only).

### FR-14 — Side-by-side variant review composite (rescoped from `setup_ab_comparison`)

- **Priority:** P2 — a review accelerator once FR-03 exists; the workflow survives without it.
- **Pain today:** Comparing two hook variants means capturing frames from each sequence and eyeballing them separately; no in-Premiere side-by-side exists in the exposed toolset.
- **Current state:** `setup_ab_comparison` exists (`internal/mcp/versioning_tools.go:214-250`, long-tail/unexposed), but its host function `createABComparison` (`premiere.jsx:24823+`) is a **metadata stub**: it returns two timeline snapshots and `status: "comparison_ready"` without building any comparison sequence — there is nothing to capture. No live verification needed; the verdict is statically decided.
- **Proposed behavior:** Rebuild **host-free** as a media-engine composite: `export_preview` both variants, then ffmpeg `hstack`/`vstack` (top_bottom suits 9:16) the two files into one review MP4 with optional labels — non-destructive by construction, no modal risk, runs in parallel with Premiere work. Pairs naturally with FR-13's 0–3s hook previews ("compare two hooks in one Slack MP4").
- **Acceptance criteria:** (1) Two 1080×1920 variant previews produce a composite MP4 without touching either sequence (`diff_timeline` on both sources is empty). (2) The composite probes at the expected combined dimensions. (3) Output lands in `Previews/` and is Slack-postable. (4) The stub host path is left unexposed.
- **Effort/Risk:** S, low — pure media-engine/ffmpeg composition; no host, no modal, no reload.
- **Type:** New build (host-free media-engine composite), replacing a stub.

### FR-15 — `deliver_short` macro, review watermarks, and deletion of the fake-success social stubs

- **Priority:** P2 — the capstone convenience once FR-01/04/06/12 land; also removes a latent honesty hazard from the codebase.
- **Pain today:** Even with all pieces working, shipping one variant is a 5–6 call ritual (verify aspect → export → await → loudness QC → Slack) repeated dozens of times daily. Meanwhile `export_for_tiktok`/`export_for_instagram`/`create_vertical_version`/`create_square_version` sit in the code as fake-success stubs (unregistered `brokenTools`; `internal/orchestrator/delivery_ops.go`; `cep-panel/src/host/premiere.jsx:21929` — bogus preset, no reframe) waiting to be accidentally resurrected.
- **Proposed behavior:** New orchestrator-side macro `premiere_deliver_short`: `{sequence_id, preset, slack_channel?, require_916: true, require_lufs: {target: -14, tolerance: 1.5} | none, manifest_experiment_id?}`. Pipeline: assert the sequence is 9:16 (fail with a pointer to the FR-01/FR-02 paths) → export → FR-05 await → FR-06 LUFS on the file → post to Slack → append the FR-11 manifest/tracking row. Each step reported individually; QC failures stop before Slack.
  **Included (growth side):** (a) **review watermark mode** — burn the variant code (e.g. `EXP014-B`) in a corner of *review* renders only, and render reviews at 720×1280 so they physically can't be confused with the clean 1080×1920 master (two-render discipline: review cut vs posted master — a variant-coded watermark must never reach a posted asset); (b) **clean-master + SRT delivery mode** — for burned-vs-native-caption experiments, one variant exports with burned captions, the other as a clean master accompanied by the `.srt` file posted to Slack so the poster can use native platform captions.
  Cleanup: DELETE the `brokenTools` social stubs from `delivery_ops.go` and the host, replacing internal references with this macro — a stub that pretends success is worse than no tool.
- **Acceptance criteria:** (1) One call takes a finished 9:16 sequence to a Slack-posted, loudness-checked MP4 with a manifest row. (2) A 16:9 sequence fails the preflight with an actionable message, exporting nothing. (3) An out-of-tolerance mix stops before Slack with the measured delta. (4) Watermarked review renders are visually distinct (resolution + overlay) from masters. (5) `grep` for the old stub names finds no callable code path. (6) Failure at any stage leaves prior artifacts intact and reported.
- **Effort/Risk:** M, low — pure composition of verified pieces; risk concentrated in its dependencies.
- **Type:** New build (macro) + cleanup/deletion.

### FR-16 — Slack metrics-snapshot reminders (72h / 7d)

- **Priority:** P2 (growth side) — experiment verdicts are only comparable if metrics are read at a **fixed offset from posting**; humans are terrible at this, and inconsistent windows are the most common self-inflicted error in manual tracking.
- **Pain today:** Posting is manual and metrics are read manually from platform analytics; nothing reminds the editor to capture the 72h decision snapshot or the 7d sanity snapshot, so sheets end up comparing one post's 24h numbers to another's 5-day numbers.
- **Current state:** The Slack bot (`cli/src/slack-bot.ts`) is thread-scoped and already the delivery surface; there is no post-tracking or scheduling concept.
- **Proposed behavior:** A lightweight "mark as posted" interaction in the experiment thread (message or command with the post URL and timestamp). The bot schedules two reminders per variant — +72h ("capture decision metrics now: views, 3s hold, avg watch %, completion, saves, shares, follows") and +7d (sanity snapshot) — pinging the thread with the variant's tracking row ready to fill. Persist schedules so bot restarts don't drop them.
- **Acceptance criteria:** (1) Marking a variant posted schedules both reminders with the correct offsets. (2) Reminders name the variant and list the exact metrics to capture. (3) Schedules survive a bot restart. (4) Un-posting or re-posting reschedules cleanly.
- **Effort/Risk:** S, low — Slack bot + a small persistent scheduler; no Premiere involvement.
- **Type:** New build.

---

## Implementation waves and dependencies

- **Wave 1 (P0 — unblocks the use case):** FR-05 → FR-04; FR-01; FR-03; FR-02 in parallel (its verification outcome only selects between reframe and scale-to-fill).
- **Wave 2 (P1 — makes A/B trustworthy):** FR-12 first (small), then FR-09, FR-08, FR-10, FR-06, FR-07, FR-11.
- **Wave 3 (P2 — polish/throughput):** FR-13, FR-14, FR-15, FR-16.

**Cross-cutting constraints (state these in every design):**

- One Premiere = **one serialized work lane** (single-threaded ExtendScript engine). Variant assembly and host-side work are inherently sequential; FR-03 deliberately avoids pretending otherwise. Only AME renders overlap with host work.
- Any `.jsx` host change costs a **CEP panel reload** (FR-01/FR-02/FR-10 may incur this; FR-03/05/06/09/11/12/16 do not touch the host at all).
- **Modal dialogs freeze the engine** — the verification protocol for FR-02/FR-10 must test for them first, headlessly, before anything else. (FR-14 was rescoped host-free after a static read of its stub host function.)
- `get_export_progress` / `get_render_queue_status` expose no real queue state on 2026 — **file polling is the only export truth** (hence FR-05 underneath everything).

## GitHub issue mapping

These map cleanly onto `.github/ISSUE_TEMPLATE/feature_request.md`: FR title → `[Feature]` title; "Pain today" → Use Case; "Current state" → Premiere Pro Reference; P0/P1/P2 → priority checkboxes; proposed tool names → Proposed Tool Name. Exceptions: FR-01 and FR-12 are partly bug fixes and should file their fix portions via `bug_report.md`; FR-08 bundles several schema changes and should be one tracking issue plus per-field sub-issues. Issues are intentionally **not** filed yet — this document is the reviewable superset.
