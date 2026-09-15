# PremierPro MCP Server -- AI-Powered Video Editing for Adobe Premiere Pro

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](https://github.com/ayushozha/AdobePremiereProMCP/pulls)
[![Premiere Pro 2020-2026](https://img.shields.io/badge/Premiere%20Pro-2020--2026-9999FF.svg)](https://www.adobe.com/products/premiere.html)
[![MCP Protocol](https://img.shields.io/badge/MCP-Model%20Context%20Protocol-blue.svg)](https://modelcontextprotocol.io)
[![GitHub stars](https://img.shields.io/github/stars/ayushozha/AdobePremiereProMCP?style=social)](https://github.com/ayushozha/AdobePremiereProMCP/stargazers)

**An open-source MCP server for Adobe Premiere Pro.** Automate supported timeline, media, audio, effect, caption, graphics-template, and export workflows through Claude, GPT, or another [Model Context Protocol](https://modelcontextprotocol.io) client.

> Give it a script and your footage, review the proposed edit, then let it assemble a recoverable rough cut on a duplicated sequence.

```
"Edit this 5-minute video using script.pdf with the footage in /media/"
```

The server can parse a script, scan media, generate an edit decision list, and assemble a Premiere Pro timeline. Mutations are checked with timeline/DOM readback where the host API exposes it.

---

## Why This Exists

Video editors spend hours on repetitive tasks: syncing clips, rough cuts, color matching, audio leveling, exporting variants. This MCP server turns Adobe Premiere Pro into an AI-controllable tool, so you can describe edits in plain English and let your AI assistant execute them.

**No separate MCP subscription. Local CEP bridge. Fully open source.**

## Features

**Tool surface.** 1,073 tool schemas are registered. The default `standard`
profile exposes **128** verified, readback-first tools — sized to fit the
128-function limit most model APIs enforce — covering project/timeline
inspection, import, editing, transitions, audio, Lumetri basics, SRT captions,
baked-PNG text, frame capture, the storyboard pipeline, and export.

`MCP_TOOL_PROFILE` selects a different catalog: `curated` (all 190
verified-on-2026 tools), a workflow profile (`dialogue`, `captions`, `social`,
`transitions`, `effects`, `proxies`, `delivery`, `metadata`, `audit`), or `all`
for the unverified long tail. Tools known broken on 2026 are **never** exposed
by any profile, including `all`. Arbitrary-execution escape hatches require
`MCP_TOOL_PROFILE=unsafe` or `MCP_ENABLE_ESCAPE_HATCHES=1` — **set neither on a
shared hub**, since the Slack bot can call every registered tool. See
`go-orchestrator/internal/mcp/core_toolset.go` and `tool_profiles.go` for the
exact policy, enforced by tests.

Highlights of the curated surface:

- **Storyboard pipeline** — hand over a script, a simple shot-list CSV, or
  storyboard JSON plus clips; get an assembled sequence with a per-shot
  report ([`docs/STORYBOARD_SCHEMA.md`](docs/STORYBOARD_SCHEMA.md)).
- **Audit trail** — every tool call is persisted with a correlation ID that
  spans all layers; `premiere_what_changed` diffs the timeline against
  pre-edit snapshots and `premiere_get_session_digest` answers "what did the
  AI do?" in plain language.
- **Remote review loop** — low-bitrate preview exports, ffmpeg contact
  sheets, frame captures, and Slack uploads so a remote user can see the cut.
- **Honest failures** — empty/hollow ExtendScript responses, unapplied
  transitions, and unconfirmed exports surface as errors or explicit
  warnings, never silent success.
- **Media analysis (Rust)** — probe, thumbnail, waveform/silence, scene
  detection, and asset scanning over files on disk.
- **Script intelligence (Python)** — script parsing, asset matching, EDL
  generation, and pacing analysis.
- **Workflow skill packs** — `skills/` ships dialogue-cut, captions, social
  reframe, transitions, look/effects, proxy-conform, and batch-delivery
  recipes, each naming the profile it needs.

The full registered catalog (including the flagged long tail):

The curated surface includes:

**Registered: 1,000+ tools** across ~45 source files; **exposed by default:
the ~190-tool verified core set** (see above). [View the full feature plan](docs/feature-plan.md).

> **Premiere 2026 reality check:** scripted native text does not render
> (text works via the baked-PNG `premiere_add_text_layer`), captions come
> from SRT import (no speech-to-text), frame stills render via a
> still-export preset (`seq.exportFramePNG` was removed), and exports queue
> in Adobe Media Encoder. A CEP→UXP migration was assessed and deferred —
> see [`docs/UXP_MIGRATION_COVERAGE.md`](docs/UXP_MIGRATION_COVERAGE.md);
> a Phase-0 spike against the live UXP module is the suggested next step.

## Supported Premiere Pro Versions

| Version | Year | Support |
|---|---|---|
| 14.x | 2020 | Manifest target; live matrix pending |
| 15.x | 2021 | Manifest target; live matrix pending |
| 22.x | 2022 | Manifest target; live matrix pending |
| 23.x | 2023 | Manifest target; live matrix pending |
| 24.x | 2024 | Manifest target; live matrix pending |
| 25.x | 2025 | Primary development target; live matrix pending |
| 26.x | 2026 | Beta manifest target; live matrix pending |

The CEP bridge targets **macOS** and **Windows** and declares Premiere Pro 14.0+ in its extension manifest.

The manifest range is not a live compatibility certification. Run the host smoke workflow against your exact Premiere build before trusting project mutations.

Help us expand compatibility -- [report your setup](https://github.com/ayushozha/AdobePremiereProMCP/issues/4).

## Architecture

Four languages, each playing to their strengths:

```
CLI / MCP Client (Claude, GPT, any AI)
       | stdio / JSON-RPC
       v
+-------------------------------------+
|     Go -- MCP Server & Orchestrator  |
|  Protocol handling . Concurrency     |
|  Service mesh . Health & recovery    |
+------+------------+------------+-----+
       | gRPC       | gRPC       | gRPC
       v            v            v
+------------+ +----------+ +----------------+
|   Rust     | |  Python  | |  TypeScript     |
|   Media    | |  Intel   | |  Premiere Pro   |
|   Engine   | |  Layer   | |  Bridge         |
+------------+ +----------+ +-------+--------+
                                    | CEP / ExtendScript
                                    v
                             Adobe Premiere Pro
```

| Language | Role | Why |
|---|---|---|
| **Go** | MCP server, orchestration | Goroutines for concurrency, fast startup, low memory |
| **Rust** | Media processing | Raw performance for scanning, indexing, waveform analysis |
| **Python** | Edit intelligence | Deterministic script parsing, asset matching, EDL generation, pacing analysis |
| **TypeScript** | Premiere Pro bridge | Native access to Adobe's ExtendScript/CEP DOM |

Full architecture diagram: [`docs/architecture.md`](docs/architecture.md)

## Project Structure

```
PremierProMCP/
+-- go-orchestrator/          # Go -- MCP server & task orchestrator
|   +-- cmd/server/           #   Entry point
|   +-- internal/             #   Core packages
|   |   +-- mcp/              #     MCP protocol handler (1,064 registered schemas)
|   |   +-- orchestrator/     #     Task orchestration
|   |   +-- health/           #     Health checks
|   |   +-- grpc/             #     gRPC client/server
|   +-- configs/              #   Configuration files
|
+-- rust-engine/              # Rust -- Media processing engine
|   +-- src/
|       +-- media/            #   Media probe & metadata
|       +-- assets/           #   Asset indexing & fingerprinting
|       +-- waveform/         #   Waveform & silence detection
|       +-- thumbnails/       #   Thumbnail generation
|
+-- python-intelligence/      # Python -- AI intelligence layer
|   +-- src/
|   |   +-- parser/           #   Script parsing & NLP
|   |   +-- edl/              #   Edit Decision List generation
|   |   +-- matching/         #   Shot-to-asset matching
|   |   +-- analysis/         #   Pacing & timing analysis
|   +-- tests/
|   +-- models/               #   ML model configs
|
+-- ts-bridge/                # TypeScript -- Premiere Pro bridge
|   +-- src/
|       +-- extendscript/     #   ExtendScript API layer
|       +-- cep/              #   CEP Panel bridge (primary)
|       +-- standalone/       #   Node.js fallback bridge
|       +-- timeline/         #   Timeline operations
|
+-- cep-panel/                # CEP Panel -- Premiere Pro extension
|   +-- src/
|   +-- assets/
|   +-- CSXS/                 #   Adobe extension manifest
|
+-- proto/                    # Shared protobuf definitions
+-- docs/                     # Documentation
+-- scripts/                  # Build & setup scripts
+-- Justfile                  # Unified build system
+-- .env.example              # Environment template
```

## Prerequisites

- [Go](https://go.dev/) 1.26.1+
- [Rust](https://rustup.rs/) 1.85+
- [Python](https://python.org/) 3.12+
- [Node.js](https://nodejs.org/) 20+
- [just](https://github.com/casey/just) (command runner)
- [buf](https://buf.build/) (protobuf toolchain)
- `rsync` (generated-client synchronization on macOS/Linux)
- [FFmpeg](https://ffmpeg.org/) (media processing)
- Adobe Premiere Pro (2020 or later)

## Quick Start

```bash
# Clone the repository
git clone https://github.com/ayushozha/AdobePremiereProMCP.git
cd AdobePremiereProMCP

# Copy env template
cp .env.example .env

# Copy the MCP config template, then edit .mcp.json to set the
# absolute path to the built premierpro-mcp binary (see below)
cp .mcp.json.example .mcp.json

# Install dependencies
just install

# Generate protobuf stubs
just proto

# Build all components
just build

# Run tests
just test

# Install the CEP panel into Premiere Pro
just install-panel
```

Those commands use a POSIX shell. Native Windows users should follow the
platform-specific build/start commands in
[`docs/USER_MANUAL.md`](docs/USER_MANUAL.md#windows-native-setup); the current
`PremierPro.bat` is a fail-fast CLI launcher, not a service supervisor.

## Usage

### As an MCP Server (Claude Code, Claude Desktop, Cursor, etc.)

This repo ships a project-scoped template at `.mcp.json.example`. Copy it to
`.mcp.json` (which is gitignored, since it holds a machine-specific absolute
path) and replace the placeholder with the absolute path to your built binary:

```bash
cp .mcp.json.example .mcp.json
# then edit .mcp.json: set "command" to the absolute path of
# go-orchestrator/bin/premierpro-mcp on your machine
```

Or add it directly to your MCP client configuration:

```json
{
  "mcpServers": {
    "premiere-pro": {
      "command": "./go-orchestrator/bin/premierpro-mcp",
      "args": ["--transport", "stdio"]
    }
  }
}
```

The complete tool catalog is returned in cursor-paginated pages of 100 by
default. MCP clients should continue requesting `tools/list` with the returned
cursor until `nextCursor` is absent. Set `MCP_PAGE_SIZE` to tune the page size.

The default `standard` profile keeps the normal editing surface small enough
for model tool limits. Set `MCP_TOOL_PROFILE` to `core`, `dialogue`, `captions`,
`social`, `transitions`, `effects`, `proxies`, `delivery`, `standard`, or a
comma-separated combination. Specialized profiles always include core
inspection and duplicate-sequence recovery tools. `all` exposes the safe full catalog; arbitrary
script, shell, URL, and file-I/O tools additionally require `unsafe` (for
example `MCP_TOOL_PROFILE=all,unsafe`) and should only be used with trusted
prompts and explicit human review.

### Workflow Skills

Seven reusable Agent Skills live in [`skills/`](skills/): dialogue cutting,
captions, social reframing, transition recipes, look/effect chains, proxy
conform, and batch delivery. Mutation skills preserve an untouched duplicate
sequence (or a tool-created derivative) as the recovery boundary and require
readback verification. Timeline snapshots and saved sequence versions are
audit/comparison records, not whole-sequence rollback points. The same catalog is available to MCP clients through
the `config://workflow-skills` resource.

The sources evaluated, patterns adopted, and integrations intentionally left
out are recorded in [`docs/workflow-research.md`](docs/workflow-research.md).
The exact test matrix and remaining host boundaries are recorded in
[`docs/repository-audit-2026-08-05.md`](docs/repository-audit-2026-08-05.md).

### Via CLI

```bash
# Start the server
just go-run

# Or run directly
./go-orchestrator/bin/premierpro-mcp --transport stdio
```

### Platform Launchers

Platform-specific CLI launchers are included:

- **macOS:** `./PremierPro.command` starts the three backends, installs the CEP
  panel if needed, waits for readiness, and launches the CLI.
- **Linux/Unix:** `./PremierPro.sh` starts the three backends, waits for
  readiness, and launches the CLI. Premiere/CEP itself must run on macOS or
  Windows.
- **Windows:** `PremierPro.bat` prepares the Node/Go CLI and launches it. Start
  the Rust, Python, and TypeScript services separately and install the panel
  with `scripts\install-cep-panel-win.bat`; full Windows supervision is not yet
  implemented by the batch launcher.

### CEP Bridge Security and Docker

The CEP panel accepts authenticated WebSocket connections only on loopback.
On first start, the Go orchestrator, TypeScript bridge, or panel creates a
shared token at `~/.premierpro-mcp/cep-token`. It authenticates both the
Go-to-TypeScript gRPC hop and the TypeScript-to-CEP WebSocket hop. To override
it, set `BRIDGE_CEP_TOKEN` or `MCP_CEP_TOKEN` consistently, or point all three
processes at the same file with `PREMIERE_MCP_TOKEN_FILE`.

Because Premiere Pro and its CEP panel run on the host, do not run the
TypeScript bridge solely inside Docker: a container cannot reach the panel's
loopback-only WebSocket. The supported live-editing layout keeps the Go
orchestrator and TypeScript bridge on the host; Docker Compose runs only the
Rust and Python analysis backends.

Before `docker compose up`, set `PREMIERE_MEDIA_ROOT` to the narrowest absolute
directory containing the input media and `PREMIERE_OUTPUT_ROOT` to an absolute
writable output directory. Compose mounts each directory at the identical
container path, so file paths sent over gRPC remain meaningful on both sides.
The input mount is read-only. Service-specific Docker targets avoid building a
duplicate all-language runtime for each analysis backend.

The SSE transport also binds to `127.0.0.1` by default. It has no application-
level authentication, so do not set `MCP_SSE_HOST` (or `--host`) to a public or
LAN interface unless an authenticated TLS reverse proxy protects it.

### Slack Bot (drive edits from Slack, no terminal needed)

Premiere Pro is single-instance, so this whole stack is designed to run on
one "hub" machine. The Slack bot lets everyone else in the office trigger
edits on that hub from a dedicated Slack channel, instead of typing into the
CLI directly.

Unlike the interactive CLI (`index.ts`), the bot does not use the Anthropic
SDK or an `ANTHROPIC_API_KEY` -- it shells out to the real `claude` CLI in
headless mode (`claude -p`), so it runs against whatever the operator is
already logged into via `claude login` (Pro/Max/Team plan usage), not
metered API billing. Tool access is scoped to just the premierpro-mcp
server (`--allowedTools "mcp__premierpro-mcp__*"`) -- no Bash, file, or web
tools. Conversation continuity across Slack messages uses `claude`'s own
session resume (`--resume <session_id>`), so one video project's whole
back-and-forth is a single ongoing Claude Code session; say "new project" or
"reset" in the channel to start a fresh one.

**1. Create a Slack app** at [api.slack.com/apps](https://api.slack.com/apps):
- Enable **Socket Mode** (the bot connects outbound to Slack -- no public
  webhook or open port needed on this Mac).
- Bot token scopes: `chat:write`, `channels:history`, `app_mentions:read`,
  `reactions:write` (for the 👀 acknowledgement reaction -- optional, the
  bot still works without it, it just won't react).
- Subscribe to the `message.channels` event.
- Install the app to your workspace, create a dedicated channel (e.g.
  `#premiere-edits`), and invite the bot into it.
- Copy the **Bot Token** (`xoxb-...`) and **App-Level Token** (`xapp-...`,
  needs the `connections:write` scope).

**2. Configure `.env`:**

```bash
SLACK_BOT_TOKEN=xoxb-...
SLACK_APP_TOKEN=xapp-...
SLACK_CHANNEL_ID=C0123456789   # the dedicated channel's ID
```

**3. Run it** (make sure the backend services and Premiere Pro are up first):

```bash
npm run --prefix cli slack-bot
```

Anyone in `#premiere-edits` can now type requests ("cut a 30s promo from
clip_04.mp4, add a lower-third with the speaker's name") and the bot will
run them against the live Premiere Pro project on this machine and reply
in-thread. Say "new project" (or "reset") in the channel to clear the bot's
conversation memory when starting the next project.

**4. Keep it running** without a terminal open, with auto-restart on crash
or reboot:

```bash
./scripts/install-slack-bot-service.sh
```

This installs a per-user `launchd` LaunchAgent (a LaunchDaemon won't work
here -- Premiere Pro needs a logged-in GUI session). Logs land in
`scripts/logs/slack-bot.log`.

**Exports:** point your export preset's output folder at a directory synced
by the Google Drive desktop app (e.g. a shared drive folder). No extra code
is needed -- Drive's own sync picks up the finished file, and the bot's
Slack reply will name it.

**Guardrails:**
- Don't run the interactive CLI and the Slack bot at the same time against
  the same project -- both spawn independent MCP connections and would issue
  uncoordinated commands against the one open Premiere Pro project.
- This hub can only usefully work one video project at a time (one Premiere
  Pro project open). A second concurrent project needs a second physical hub
  (another machine with its own Premiere Pro + this stack).

## How It Works

1. **You send a prompt** -- "Edit this video using the script with footage from /media/"
2. **Go orchestrator** receives the MCP tool call and fans out:
   - **Rust engine** scans `/media/`, indexes all assets (codec, duration, resolution, waveforms)
   - **Python intelligence** parses the script, generates an Edit Decision List, matches shots to assets
3. **Go merges results** and sends the EDL to the TypeScript bridge
4. **TypeScript bridge** executes in Premiere Pro -- creates the sequence,
   places clips, and applies supported transitions and graphics templates
5. **Premiere Pro renders** the final output

## Build Commands

| Command | Description |
|---|---|
| `just build` | Build all components |
| `just test` | Run all test suites |
| `just lint` | Lint all code |
| `just ci` | Full CI pipeline (lint + build + test) |
| `just proto` | Generate protobuf stubs |
| `just clean` | Remove all build artifacts |
| `just go-build` | Build Go orchestrator only |
| `just rust-build` | Build Rust engine only |
| `just py-test` | Run Python tests only |
| `just ts-build` | Build TypeScript bridge only |
| `just cep-build` | Build CEP panel only |
| `just install-panel` | Install CEP panel into Premiere Pro |
| `just start` | Start all backend services |
| `just stop` | Stop all backend services |
| `just status` | Check service status |

## Use Cases

- **Automated rough cuts** -- Parse a script and assemble a timeline from raw footage
- **Batch color grading** -- Apply Lumetri Color adjustments across clips via natural language
- **Audio post-production** -- Set levels, apply effects, and mix tracks through AI prompts
- **Template-based editing** -- Generate videos from MOGRTs and data using AI
- **Multi-format export** -- Queue multiple export presets from a single command
- **Review workflows** -- Add markers, comments, and metadata programmatically
- **AI-assisted editing** -- Let Claude or GPT analyze your footage and suggest edits

## Community

We are actively looking for testers and contributors!

- **Test the server** with your Premiere Pro setup and [report results](https://github.com/ayushozha/AdobePremiereProMCP/issues/1)
- **Request features** you need for your workflow in [the feature tracker](https://github.com/ayushozha/AdobePremiereProMCP/issues/2)
- **Report bugs** with reproduction steps in [the bug tracker](https://github.com/ayushozha/AdobePremiereProMCP/issues/3)
- **Confirm your Premiere Pro version** works in [the compatibility tracker](https://github.com/ayushozha/AdobePremiereProMCP/issues/4)
- **Start or join a discussion** in [GitHub Discussions](https://github.com/ayushozha/AdobePremiereProMCP/discussions)
- **Read the [Contributing Guide](CONTRIBUTING.md)** to get started with development

If this project is useful to you, please **star the repository** to help others find it.

## Related

- [Model Context Protocol](https://modelcontextprotocol.io) -- The open protocol for AI tool use
- [Adobe Premiere Pro Scripting Guide](https://ppro-scripting.docsforadobe.dev/) -- ExtendScript API reference
- [Adobe CEP Resources](https://github.com/nicmangroup/CEP-Resources) -- CEP panel development

## License

MIT
