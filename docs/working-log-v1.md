# Working Log v1 — Premiere Pro 2026 MCP overhaul

_A reasoning/decision reference for whoever picks this up next (including future me). Written 2026-07-13, covering the session that took the server from "1,065 tools, a third broken on 2026" to a verified golden path running end-to-end on live Premiere 26.3._

---

## 1. The problem, restated

Three requirements drove everything:

1. Target users are **non-editors** — they can't be handed track indices, EDL columns, or stack traces.
2. The workflow is **storyboard + clips in → cut out**.
3. There must be **traceability** — a way to reconstruct what the AI did and where it broke.

Underneath those, exploration found the real starting condition: ~1,065 hand-written MCP tools, of which a quarter to a third were broken or lying on Premiere 2026 (removed APIs, undocumented QE DOM, fake-success placeholders, ~41 tools calling ExtendScript functions that don't exist). The headline pipeline (`auto_edit`, `generate_rough_cut`) was among the broken. So the job wasn't "add features" — it was **make the surface honest, then build one workflow on the honest part.**

## 2. Overall strategy: honesty first, then the happy path

The ordering was deliberate and I'd repeat it:

- **WS0 Export hardening** → land the in-flight diff so later work builds on a known base.
- **WS1 Traceability** → build the audit spine *first*, because it's the instrument every later step is debugged with. This paid off enormously: once every tool call was a JSONL record with a correlation ID and honest status, the live-testing phase could diagnose failures from the log instead of guessing.
- **WS3 Curation + drift guard** → shrink 1,065 → ~195 verified tools, and make "tool calls a host function that doesn't exist" a **build failure** forever. Doing this early meant the storyboard work was authored against a surface I trusted.
- **WS2 Storyboard pipeline** → the actual product, built on WS1's audit records and WS3's clean surface.
- **WS4 Review loop + Slack UX**, **WS5 Docs** → make it usable and truthful to the end user.

The throughline: **a wrong answer that looks right is worse than an error.** Nearly every fix this session converts a silent success into either a real result or an honest failure (empty-envelope→error, `addTransition` applied/not-applied, export "completed" vs "queued_not_confirmed", panel non-JSON→failure, health check pinging the real chain instead of hardcoding `connected:true`).

## 3. Key architectural decisions and why

- **Go-orchestrated assembly loop, not a fixed `executeEDL` host fn.** The storyboard executor drives verified-working primitives (`createSubclip`→`overwriteClip`→`trimClipEnd`→`addTransition`) from Go, one honest per-shot result each. Rationale: ES3 is the highest-risk place to add logic, new host functions need a panel reload to take effect, and a single opaque host call can silently half-work. The Go loop also gives the audit layer something to hang records on. `auto_edit`'s broken step was rerouted through this same executor — one engine, three front doors (JSON, CSV, script).

- **Curate by pruning at startup, not by rewriting 45 files.** mcp-go's `ListTools()`/`DeleteTools()` let curation live in one `core_toolset.go` with three lists (core / broken / escape-hatch) plus env flags. Zero churn in the tool-definition files.

- **Drift guard as a Go test, AST-based.** It extracts every literal `EvalCommand` target and asserts the host function exists, with a shrink-only exception list. This is the single highest-leverage thing built this session: the `generateRoughCut` class of bug can never ship again, and the exception list documents exactly what's still missing.

- **Correlation ID via gRPC metadata + AsyncLocalStorage, not a proto field.** One interceptor at the single dial site covers all RPCs with no buf regen; ts-bridge threads it through ALS so no handler signatures change. The ID does NOT go into ExtendScript args (would corrupt `__invoke`'s positional fallback) — it rides as a sibling WS field.

- **JSONL audit over SQLite.** Every headless `claude -p` turn spawns its own server process → concurrent writers. `O_APPEND` single-line writes are atomic on macOS; args capped at 2 KB keep lines under the atomic-write size. Greppable, no CGO.

- **UXP deferred.** The coverage matrix says ~34% of the surface has no UXP path. Stabilizing CEP for 2026 was the fast path to a working product; UXP is a separate future effort (Phase-0 spike noted).

## 4. Multi-agent usage

Used the Workflow tool exactly twice, both for fan-out where it earned its cost: 3 parallel explorers (architecture / observability / user-workflow) then 3 parallel planners (core pipeline / traceability / curation). Everything after — the actual implementation — was done inline, because implementation is sequential and stateful and doesn't parallelize cleanly across agents. The explore→plan fan-out front-loaded the understanding so the inline work rarely backtracked on *design* (it backtracked plenty on *live 2026 behavior* — see §5).

## 5. The live-testing phase is where the real bugs were

This is the biggest lesson. Everything compiled, unit tests passed, and a read-only smoke test (195 tools, ping, health, sequence list) passed — and then the **first golden-path run against live Premiere still failed**, repeatedly, on things no offline check could catch:

- `getProjectItems` with `{}` → `__invoke` fed the literal `"{}"` as a bin path.
- `createNewSequence`/`createNewSequenceFromClips` return the **Sequence DOM object**, not an id string, on 2026 → serializing it produced a giant non-JSON blob.
- `createNewSequence(name, "")` with an empty preset **pops a modal dialog that blocks the entire single-threaded ExtendScript engine** — every subsequent call times out at 30 s. Diagnosed by noticing ping went from instant to 30 s.
- The **ES3 JSON polyfill emitted literal `undefined`** for undefined object members → invalid JSON. This was the root cause of a whole *class* of "non-JSON result" failures, only visible because a real 2026 DOM getter returned an unexpected shape.
- `exportAsMediaDirect` with a still preset returns "No Error" and **writes nothing** on 26.3; the working path is the AME queue.
- 2026's encoder collections are array-like (`.length`) and preset objects have **no `.path`** — so the in-flight preset resolver (written blind) could never work; had to resolve against the on-disk `.epr` bundle.
- Pre-mutation snapshots grabbed `sequences[0]` instead of the **active** sequence, so `what_changed` was blind to edits on new sequences.

Takeaways for next time:
- **Nothing about a scripting host's live behavior can be trusted from docs or from the previous version.** Introspect the actual objects (`for (k in obj)`, `typeof`, `.length` vs `.numX`) on the running app before writing against them.
- **A blocked engine looks like a hang, not an error.** When calls start timing out uniformly while the process is up, suspect a modal dialog, not a crash. Ping-timing is the cheap tell.
- The **audit log + correlation IDs were the debugger.** I read exact failures out of `scripts/logs/audit/*.jsonl` rather than re-deriving them. Building observability first repaid itself here many times over.

## 6. The hot-patch technique (how live fixes were applied without endless panel reloads)

`.jsx` edits only take effect on a CEP panel reload, which needs a human at the hub. To keep iterating, I extracted the fixed functions from source and reassigned them onto `$.global` in the running engine via `premiere_execute_script` (escape hatch, `MCP_ENABLE_ESCAPE_HATCHES=1`). Two rules made this safe:
- The hot-patch was **generated from the committed source**, so it's byte-identical to what the next reload loads from disk — no drift between "what I tested" and "what ships".
- Verified each patch took (`toString()` probe / behavior check) before relying on it.

This let the golden path reach 16/16 without asking for a reload between every fix. The commits are the source of truth; the hot-patches were scaffolding.

## 7. Testing philosophy that worked

The golden-path harness asserts **against ground truth, not the tool's self-report**: after `assemble_storyboard` it calls `get_all_clips` and checks real clip starts/durations; it checks transitions are applied-or-honestly-reported; it confirms files actually land in the Previews folder and actually upload to Slack. Two test-harness bugs (reading `.sequence_name` off the `GenericResult` wrapper instead of the nested `message`, and the duplicate-import "ambiguity") were themselves caught this way and fixed — one in the harness, one a real product improvement. **Distinguish harness bugs from product bugs explicitly** so you don't "fix" working code.

Every test run **cleans up after itself** (deletes the test sequence, bins and deletes test media) because it runs against the user's real open project. Synthetic ffmpeg clips in a scratch dir kept the test hermetic.

## 8. What's solid vs. what to watch

Solid: the audit spine, drift guard, curation, storyboard schema/compiler (pure + unit-tested), and the golden path itself (verified live).

Watch:
- **AME cold-launch timing.** The first frame capture / preview after Premiere starts can be slow; timeouts were widened (frame ops 3 min, exportFrame poll on the Go side) but a truly cold AME on a heavy sequence is the fragile edge.
- **The dialog trap is latent elsewhere.** Any host call that can pop a modal (preset dialogs, "save changes?", missing-media prompts) will block the engine. `createSequence` still uses the dialog path for callers who pass no preset — the golden path avoids it, but other tools may hit it.
- **The `~90s cold frame render` figure** was observed on a 4K client sequence; the synthetic 720p test is much faster, so don't calibrate timeouts on the test alone.
- Several believed-real core tools (roll_trim, slip_clip, etc.) are in the curated set on the strength of the drift guard (host fn exists) but weren't each individually driven on 2026 — worth a one-time smoke pass or demotion.

## 9. If I did it again

- Same order (honesty → observability → curation → product). It's what made the messy live-testing phase tractable.
- I'd introspect the live 2026 encoder/sequence/DOM API **before** writing the export/frame/preset code, not after — the in-flight hardening diff was written blind and most of it had to be redone once I saw the real object shapes.
- I'd add a "does this call risk a modal?" check to the mental model earlier; the dialog-blocks-engine failure cost the most diagnosis time.

---

_Commits from this session, oldest first: export hardening → UXP matrix → audit spine → timeline diff/digest tools → honest errors/health → curation + drift guard → storyboard pipeline → review loop + Slack → docs → typed-path envelope fix → sequence-seeding fix → JSON polyfill/ticks/preset/capture fixes → AME-queue frame render. Golden path verified 16/16 live on Premiere 26.3._
