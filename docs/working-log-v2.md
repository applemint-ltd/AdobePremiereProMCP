# Working Log v2 — live verification, 2026 API reverse-engineering, and review hardening

_Continues [working-log-v1.md](working-log-v1.md). v1 covered the build-out (audit spine, curation + drift guard, storyboard pipeline, review loop, Slack UX) up to the first golden-path pass — which ran against a **hot-patched** engine. v2 is everything after: verifying that against a clean panel reload, the Premiere-2026 encoder/frame reverse-engineering that forced, curating the tool set against live behavior, auditing the modal-dialog trap, and fixing what a workflow code review found. Written 2026-07-16._

---

## 1. The through-line of this session: offline-green is not done

Everything compiled, unit tests passed, and the first golden pass was 16/16 — but almost every bug fixed in v2 was invisible until the code ran against a **clean load of committed source on the real app**. The recurring shape:

1. Hot-patched runs (v1) proved the *logic*, but hot-patches were generated from committed source and could mask "did the committed file actually load and behave" questions.
2. A clean panel reload (#9) immediately exposed bugs the hot-patched runs hid.
3. Each fix that touched the encoder/frame subsystem exposed the next wrong assumption about the 2026 API, because the in-flight export-hardening diff (v1's starting point) had been written *blind*, without introspecting the live objects.

If there's one lesson to carry forward: **introspect the live host API before writing against it, and verify from a clean load, not just a hot-patch.**

## 2. Premiere 2026 encoder/frame reality (hard-won, all verified live)

The single most expensive discovery area. What's actually true on 26.3:

- **No working scripted single-still export.** `seq.exportAsMediaDirect(path, stillPreset, …)` returns `"No Error"` and writes **nothing** (no file appears, ever — checked minutes later). `encoder.encodeSequence(seq, path, stillPreset, …)` returns a job id but AME **also never writes the still** (confirmed for both in-to-out and entire-sequence work areas, warm AME). The **only** render path that works is H.264 via AME.
  - Consequence: `capture_frame` and `export_frame` were rerouted to render a short preview via the working export path, then extract the frame with the rust/ffmpeg thumbnailer (the same extractor the contact sheet uses). Heavier (renders the sequence once) but reliable.
- **The encoder collections are array-like.** `app.encoder.getExporters()` and `exporter.getPresets()` use `.length`, not `.numExporters`/`.numPresets` (the blind in-flight diff assumed the latter → "0 exporters" forever). Preset objects expose only `{id, matchName, name}` — **no `.path`** — so you cannot get an `.epr` path from the AME collection at all. Export presets resolve against the app's on-disk `Contents/MediaIO/systempresets/` bundle instead (H.264 dir is `4E49434B_48323634`).
- **`getExporters` returns `classID`/`fileType` as numeric fourcc codes**, not strings — strict Go string unmarshalling broke `GetExporters` whenever exporters existed (fixed with a number-or-string `FlexString`).
- **Cold AME is the real production edge.** The session's first render eats the 1–2 min launch and can false-timeout; a warm render is ~4s. `EnsureEncoderReady` now warms AME *before* the render clock starts (separate bound), plus a `premiere_warm_encoder` tool the bot calls once early. Mitigating real-world fact discovered while testing: **with Premiere open, Dynamic Link keeps AME alive**, so cold-start is mostly first-launch-of-day.
- **`createNewSequence(name, "")` pops a modal** that freezes the whole single-threaded ExtendScript engine. The dialog-free path is `qe.project.newSequence(name, presetPath)` with a bundled `.sqpreset`. (Also: it returns the Sequence DOM object, not an id string — never serialize it; and the ES3 JSON polyfill emitted literal `undefined` for undefined members, which was the root cause of a whole class of "non-JSON result" failures.)

## 3. The verification discipline that worked

- **Two in-repo smoke gates, both asserting ground truth** (read the timeline back, confirm files land and upload — never trust a tool's self-report): `scripts/smoke/golden_path_smoke.py` (the non-editor workflow) and `scripts/smoke/core_tools_smoke.py` (the curated editing tools). Self-contained (synthesize their own ffmpeg clips), self-cleaning, skip Slack without a token. Re-run after any pipeline/jsx/bridge change.
- **Hot-patch from committed source to iterate, clean-reload to verify.** Hot-patching `$.global.fn = <committed body>` let me test jsx fixes without asking for a panel reload each time; the authoritative check was always a reload loading the file from disk. Keep both in the toolkit but never conflate them.
- **Distinguish harness bugs from product bugs explicitly.** Two "failures" were the test harness reading a `GenericResult.message` wrapper wrong / mis-calling a `clip_id`-based tool — not product defects. Fixing the wrong layer would have "fixed" working code.
- **Don't force-test a hazard you can't safely trigger.** The modal-dialog audit (#12) was static — actually popping a dialog would freeze the hub. The cold-AME path couldn't be forced either (Dynamic Link respawns AME), so it was reasoned about and mitigated rather than reproduced.

## 4. Curation against live behavior (#11) and the drift-guard gap it exposed

Smoke-testing the *believed-real* core tools found the whole `lumetri_set_{exposure,contrast,saturation,temperature,tint}` family broken: the Go ops called `lumetriSet*2` host functions that don't exist (a stray "2"; the host has the correct non-"2" versions). One-character fix per op.

The important meta-finding: **a `coreTools` entry can route to a `knownMissingHostFns` target and the drift guard won't flag it.** The drift guard proves *called* ES functions exist (or are known-missing); it does not prove *core tools* route to *existing* ones. The runtime guard for that is now `core_tools_smoke.py`. Five genuinely-broken-on-2026 tools were demoted to the long tail with per-line reasons: `reverse_clip`, `get_captions`, `lumetri_get_all`, `set_audio_gain`, `set_audio_track_volume` (per-clip `normalize_audio` covers "fix audio"; per-track volume and source-item gain aren't scriptable on 2026).

## 5. The code review (#13) — and why the smokes missed its top finding

A workflow-backed review (parallel finders per angle + independent per-location verifiers, high effort) over 21 commits returned **7 verified findings, 0 refuted**. The standout was a **real timeline-corruption bug**: a text-only title card incremented the track-0 positional clip index without placing a track-0 clip during the loop, so the next real clip failed verification and the one after **clobbered** it — breaking any storyboard with an intro card (exactly what script parsing produces).

Why did 16/16 golden + 22/22 core miss it? **The smoke fixtures never had a clip-less shot.** The golden CSV's first shot has a clip *and* text; a standalone title card was never exercised. Lesson: the smoke fixtures encode assumptions about input shape — the bugs live in the shapes you didn't fixture (title-only cards, end-of-sequence playhead, batch frames). The other six (near-end frame → middle-frame relocation, `batch_export_frames` reporting success with no files, dead-but-broken host `captureFrameAsBase64`, trimmed-*or*-moved diff dropping the move, `awaitExportedFile` false-positive on a mux pause) were the same theme: correct on the happy path, wrong on a shape the tests didn't cover. All fixed; the title-card fix verified live; a regression test added for the diff.

## 6. This work through the lens of loops

Reflecting on the session against Anthropic's ["Getting started with loops"](https://claude.com/blog/getting-started-with-loops) — loops being _"agents repeating cycles of work until a stop condition is met"_ — most of what made v2 work was loop-shaped, and its worst bugs were bad stop conditions.

- **The core debugging cycle was a goal-based loop.** Fix → rebuild → run the smoke → read the audit log → fix again, with an explicit, measurable stop condition: **the smoke is all-green** (16/16 golden, 22/22 core). The clean-reload verification (#9) was exactly this loop and it earned its keep by _not_ stopping early — the first clean-load run was 15/16, so the loop kept going and each iteration surfaced the next 2026 API bug (§2). The audit trail was the loop's observation channel: I read failures out of `scripts/logs/audit/*.jsonl` rather than re-deriving them each turn.

- **The smoke gates are the "encode verification so the agent self-validates" principle, made concrete.** `golden_path_smoke.py` and `core_tools_smoke.py` are the machine-checkable success criteria that let each loop iteration decide "done or keep going" without me eyeballing the timeline. They assert **ground truth** (read the timeline back, confirm files land) precisely so the stop condition can't be faked by a tool's self-report.

- **Premature stopping was the actual failure mode — twice, literally.** The post's caution _"define explicit success criteria to prevent premature stopping"_ cut both ways here:
  - `awaitExportedFile` was a loop whose stop condition ("size stable across one 2s repeat") fired mid-render on a mux pause → a truncated file reported as complete. The fix (#13, finding 7) hardened the stop condition to _three_ stable polls **plus** a `ProbeMedia` readable-stream check.
  - The whole smoke suite passing was itself a stop condition that was satisfiable without full coverage — it had no clip-less title-card fixture, so the code review's text-card corruption bug slipped past a green loop (§5). Weak success criteria stop too soon; that's a loop failure, not just a test gap.

- **Poll-until-condition loops live inside the product too**, each a small "repeat until stop," each deliberately bounded to avoid hanging or false stops: `EnsureEncoderReady` (poll `getExporters` until AME reports ready, 4-min bound, cached once-ready), `awaitFrameFile` / `awaitExportedFile` (poll disk until the render lands, ctx-bounded → honest `queued_not_confirmed` on timeout rather than a lie), and the ExtendScript `_waitForExporters`. The cold-AME warm-up (#14) is essentially moving one of these loops _earlier_ so the user isn't the one waiting on it.

- **The bigger investigations were composed loops** — the "compose multiple primitives" idea. The explorations, the design pass, and the code review (#13) each ran as a background **workflow**: fan out finders/agents → independently verify each candidate → synthesize, i.e. a verify-loop wrapped around parallel worker-loops, with "verified findings, most-severe first" as the stop. The **drift guard in CI** is the proactive, automated end of the spectrum: a guard-loop that re-checks every EvalCommand target against the host on every PR so the `generateRoughCut` class of bug can't reappear.

The meta-takeaway matches the post's "start simple; complexity serves necessity": the loops that paid off were the cheap ones (a smoke test as the stop condition), and the loops that bit were the ones with a **too-weak stop condition** — so most of v2's hardening was, in effect, tightening stop conditions until "green" actually meant "correct."

## 7. State at end of v2

- All planned workstreams + follow-ups (#9–#14) complete. Golden smoke 15/15, core smoke 22/22, `go test`/`vet` green.
- ~25 commits ahead of `origin/main`, **unpushed** (never asked), no PR. Working tree clean.
- **Two jsx fixes await a CEP panel reload to go live** (both hot-patch-verified against committed source, so the passing smokes can't have regressed from them): the dialog-free `create_sequence` (#12) and the honest-error host `captureFrameAsBase64` (#13). Reload + re-run both smokes to confirm from a clean load, as in #9.

## 8. Open follow-ups (not blocking)

- `batch_export_frames` is demoted but its host loop still lies if exposed via `MCP_EXPOSE_ALL_TOOLS`; wants a Go-side reroute (one preview → ffmpeg-extract N frames), mirroring `capture_frame`.
- `lumetri_get_all` demoted: its `LUTAsset`/`LookAsset` values carry a control char that breaks JSON re-parse in transit — needs escaping in the ES3 polyfill or the host getter if it's ever wanted back.
- Auto-transcription (whisper) and the UXP Phase-0 spike remain the two deliberately-deferred larger efforts (see v1 §3 and `UXP_MIGRATION_COVERAGE.md`).
- A stronger drift guard could map `coreTools` → op → ES target statically and fail if a core tool routes to a `knownMissingHostFns` entry — closing the gap §4 describes without relying on the runtime smoke.

---

_v2 commits, oldest first: clean-reload verification → frame reroute (AME still export is dead) → thumbnail real-dimensions → in-repo golden smoke → AME warm-up + FlexString → Lumetri stray-"2" fix + core-tool demotions + core smoke → dialog-free create_sequence (+ modal audit) → 7 code-review fixes. Two smoke gates green; two jsx fixes pending the next panel reload._
