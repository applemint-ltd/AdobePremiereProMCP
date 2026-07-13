# PremierPro MCP Server -- AI-Powered Video Editing for Adobe Premiere Pro

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](https://github.com/ayushozha/AdobePremiereProMCP/pulls)
[![Premiere Pro 2020-2026](https://img.shields.io/badge/Premiere%20Pro-2020--2026-9999FF.svg)](https://www.adobe.com/products/premiere.html)
[![MCP Protocol](https://img.shields.io/badge/MCP-Model%20Context%20Protocol-blue.svg)](https://modelcontextprotocol.io)
[![GitHub stars](https://img.shields.io/github/stars/ayushozha/AdobePremiereProMCP?style=social)](https://github.com/ayushozha/AdobePremiereProMCP/stargazers)

**The open-source MCP server for Adobe Premiere Pro.** Control every aspect of video editing -- timeline, color grading, audio mixing, effects, graphics, and export -- through natural language using Claude, GPT, or any AI assistant that supports the [Model Context Protocol](https://modelcontextprotocol.io).

> Give it a script and your footage. It handles the rest.

```
"Edit this 5-minute video using script.pdf with the footage in /media/"
```

The server parses your script, scans your media library, generates an edit decision list, and assembles the timeline in Premiere Pro -- all from a single prompt.

---

## Why This Exists

Video editors spend hours on repetitive tasks: syncing clips, rough cuts, color matching, audio leveling, exporting variants. This MCP server turns Adobe Premiere Pro into an AI-controllable tool, so you can describe edits in plain English and let your AI assistant execute them.

**No plugins. No subscriptions. Fully open source.**

## Features

**By default the server exposes a curated set of ~190 tools verified to work
on Premiere Pro 2026.** Over a thousand tools are registered in the codebase,
but a large share of the long tail depends on APIs Adobe removed or the
undocumented QE DOM; exposing them made AI agents fail unpredictably.
Verified-broken tools are never registered; the unverified long tail is
available behind `MCP_EXPOSE_ALL_TOOLS=1`, and arbitrary-execution escape
hatches (`premiere_execute_extendscript`, `premiere_execute_system_command`,
file writers, …) require `MCP_ENABLE_ESCAPE_HATCHES=1`. **Set neither flag on
a shared hub** — the Slack bot allowlists every registered tool. See
`go-orchestrator/internal/mcp/core_toolset.go` for the exact policy, enforced
by tests.

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

The full registered catalog (including the flagged long tail):

| Category | Tools | What You Can Do |
|---|---|---|
| **Core/Foundation** | 14 | Ping, get project state, create sequences, import media, place clips, export |
| **App Lifecycle** | 3 | Launch, quit, and check Premiere Pro process status |
| **Project Management** | 23 | Create, open, save, close projects; manage bins, scratch disks, metadata |
| **Sequence Management** | 26 | Create, duplicate, delete sequences; playhead, in/out points, markers, nesting |
| **Clip Operations** | 29 | Insert, overwrite, move, trim, split, slip, slide, speed, link/unlink clips |
| **Effects & Transitions** | 36 | Apply/remove effects and transitions, keyframe animation, motion, Lumetri basics |
| **Audio (basic)** | 32 | Levels, gain, mute/solo, effects, Essential Sound, track management |
| **Audio (advanced)** | 30 | Mixer state, EQ, compressor, limiter, de-esser, loudness, sync, waveform analysis |
| **Color Grading** | 30 | Full Lumetri Color: exposure, contrast, curves, HSL, color wheels, LUTs, vignette |
| **Graphics & Titles** | 21 | MOGRTs, titles, lower thirds, captions, color mattes, time remapping |
| **Export (basic)** | 14 | Direct export, AME queue, frame export, AAF/OMF/FCPXML, audio-only export |
| **Advanced Editing** | 31 | Ripple/roll/slip/slide trims, gap management, grouping, snapping, navigation |
| **Batch Operations** | 30 | Batch import/export, apply effects to multiple clips, auto-organize, markers |
| **AI/ML Workflows** | 25 | Smart cut, auto color match, rough cut, B-roll suggestions, social cuts, analysis |
| **Workspace & Multicam** | 25 | Multicam, proxy management, workspaces, undo/redo, source monitor, cache |
| **Playback & Navigation** | 30 | Play/pause/stop, shuttle, step, loop, timecode navigation, render status |
| **Transform & Masking** | 30 | Crop, PIP, fade, stabilizer, noise reduction, blur, sharpen, distortion |
| **Metadata & Labels** | 30 | XMP metadata, labels, footage interpretation, smart bins, media management |
| **Preferences** | 30 | Still/transition durations, auto-save, playback resolution, cache, renderer, codecs |
| **Templates & Presets** | 30 | Sequence/effect/export presets, project templates, batch rename, macros |
| **Motion Graphics** | 30 | Essential Graphics, scrolling titles, shapes, watermarks, split screen, subtitles |
| **Collaboration & Review** | 30 | Review comments, version history, snapshots, EDL/AAF/XML import, delivery checks |
| **VR/Immersive** | 30 | VR projection, HDR, stereoscopic 3D, frame rates, letterboxing, timecode, captions |
| **App Integration** | 28 | Dynamic Link (AE), Photoshop, Audition, Media Encoder, Team Projects |
| **Diagnostics** | 30 | Performance metrics, disk space, plugins, render status, health checks, debug logs |
| **Monitoring & Events** | 30 | Event listeners, playhead/render watchers, state snapshots, notifications |
| **UI Control** | 30 | Panel management, window control, track display, label filters, dialogs, console |
| **Compound Operations** | 30 | Montage, slideshow, highlight reel, music bed, social exports, project setup |
| **Encoding & Formats** | 30 | Codec conversion (ProRes, H.264/265, DNxHR, GIF), thumbnails, render queue |
| **Timeline Assembly** | 30 | EDL/CSV assembly, clip sorting/shuffling, compositing, generators, timeline reports |
| **Scripting** | 30 | ExtendScript execution, global variables, conditionals, scheduling, file I/O |
| **Analytics** | 30 | Project/sequence summaries, codec/resolution breakdowns, pacing, comparison reports |
| **Effect Chains** | 30 | Effect chain management, visual presets (sepia, vintage, glow), transition control |

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
| 14.x | 2020 | Community tested |
| 15.x | 2021 | Community tested |
| 22.x | 2022 | Community tested |
| 23.x | 2023 | Supported |
| 24.x | 2024 | Supported |
| 25.x | 2025 | Primary target |
| 26.x | 2026 | Beta support |

Works on **macOS** and **Windows**. The bridge uses Adobe's CEP (Common Extensibility Platform) and ExtendScript, which are supported across all modern Premiere Pro versions.

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
| **Python** | AI & NLP | Script parsing, edit decisions, shot matching via embeddings |
| **TypeScript** | Premiere Pro bridge | Native access to Adobe's ExtendScript/CEP DOM |

Full architecture diagram: [`docs/architecture.md`](docs/architecture.md)

## Project Structure

```
PremierProMCP/
+-- go-orchestrator/          # Go -- MCP server & task orchestrator
|   +-- cmd/server/           #   Entry point
|   +-- internal/             #   Core packages
|   |   +-- mcp/              #     MCP protocol handler (1,060 tool definitions)
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

- [Go](https://go.dev/) 1.22+
- [Rust](https://rustup.rs/) 1.77+
- [Python](https://python.org/) 3.12+
- [Node.js](https://nodejs.org/) 20+
- [just](https://github.com/casey/just) (command runner)
- [buf](https://buf.build/) (protobuf toolchain)
- [FFmpeg](https://ffmpeg.org/) (media processing)
- Adobe Premiere Pro (2020 or later)

## Quick Start

```bash
# Clone the repository
git clone https://github.com/ayushozha/AdobePremiereProMCP.git
cd PremierProMCP

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

### Via CLI

```bash
# Start the server
just go-run

# Or run directly
./go-orchestrator/bin/premierpro-mcp --transport stdio
```

### One-Click Launchers

Platform-specific launchers are included for quick setup:

- **macOS:** `./PremierPro.command`
- **Windows:** `PremierPro.bat`
- **Linux/Universal:** `./PremierPro.sh`

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
4. **TypeScript bridge** executes in Premiere Pro -- creates sequence, places clips, adds transitions, text
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
