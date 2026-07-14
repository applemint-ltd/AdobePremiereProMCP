# Smoke tests

Manual end-to-end gates that need a **live Premiere Pro 2026** and therefore
cannot run in CI.

## golden_path_smoke.py

Drives the real MCP server over stdio (like a Slack-bot `claude -p` turn) and
asserts against ground truth: storyboard CSV → validate → assemble (trims,
transitions, captions, baked-PNG text) → read the timeline back → what_changed
→ frame capture → preview export → contact sheet → Slack upload → session
digest, then cleans up everything it created.

**Run it after** any change to the storyboard/review pipeline, the host
`.jsx` (after reloading the CEP panel), or the ts-bridge.

### Prerequisites

- Premiere Pro 2026 running, a project open, CEP panel loaded.
- Services up: `scripts/start-all.sh` (rust + python) and `scripts/start-ts.sh`
  (bridge on :50054).
- Adobe Media Encoder installed. **Warm it first** — the first AME render of a
  session can be slow/cold; run the script once to warm it, or trigger any
  export before relying on frame-capture timing (see task #14).
- `ffmpeg` on PATH (synthesizes the test clips into `scripts/smoke/.media/`,
  which is git-ignored).
- Server binary built: `cd go-orchestrator && go build -o bin/premierpro-mcp ./cmd/server`.

### Environment

- `SLACK_BOT_TOKEN` — if set, the Slack upload steps run; otherwise they're
  skipped with a note. `source .env` before running to pick it up.
- `SMOKE_SLACK_CHANNEL` — test channel id (defaults to the office
  `#premiere-edits` channel).

### Run

```sh
source .env            # for SLACK_BOT_TOKEN
python3 scripts/smoke/golden_path_smoke.py
```

Exit 0 iff every check passed. It runs against whatever project is open and
cleans up after itself (deletes its test sequence, bins and removes its test
media), but it does touch the live project — don't run it mid-edit on
something you care about.
