# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Added
- `premiere_await_exports` / `premiere_export_status` — file-truth export
  primitives: completion means the file exists, held a stable size, and (for
  video) probes as readable; timeouts return honest partial state (FR-05)
- `premiere_storyboard_expand_variants` — one base storyboard + per-variant
  field overrides → N validated, name-stamped storyboards, with a
  one-variable-per-experiment diff gate for A/B discipline (FR-03)
- `premiere_batch_export_sequences` — batch export rebuilt Go-side over the
  verified AME queue path with per-file file-truth completion (FR-04)
- Docs: `docs/feature-requests-shorts-ab.md` (FR-01..FR-16 backlog) and
  `docs/SHORTS_AB_TESTING_PLAYBOOK.md` (bulk 9:16 A/B workflow)

### Fixed
- `premiere_post_file_to_slack` no longer rejects files under NFD-normalized
  (e.g. Japanese-named) project folders — the upload allowlist NFC-normalizes
  both sides of the path comparison (FR-12)
- `premiere_export` with no `sequence_id` now genuinely falls back to the
  active sequence, as its schema always claimed (FR-04)

### Removed
- Dead host-side batch export paths (`batchExportSequences` /
  `exportAllSequences` orchestrator methods and the `premiere_export_all_sequences`
  tool): their `exportAsMediaDirect` call returns success but writes nothing
  on Premiere 2026; batch-with-no-targets replaces export-all

## [0.1.0] - 2026-03-19

### Added
- Initial release with 1,060 MCP tools
- Go orchestrator with MCP server (stdio + SSE transport)
- Rust media engine (scanning, probing, waveform, thumbnails, scene detection)
- Python intelligence (script parsing, EDL generation, asset matching, pacing)
- TypeScript bridge to Premiere Pro (CEP panel + standalone)
- Interactive CLI with Claude and OpenAI support
- Cross-platform launchers (macOS, Windows, Linux)
- MCP Resources (4) and MCP Prompts (5)
- Frame capture as base64 for visual AI feedback
- Secure ExtendScript execution with blocklist
- Professional CEP panel with dark/light mode
- Full Lumetri Color control (30 tools)
- Audio mixing and effects (62 tools)
- Effects, transitions, and keyframing (66 tools)
- Batch operations (30 tools)
- AI-powered editing pipeline (script → EDL → auto-edit)
- Adobe Stock search integration
- Media Browser filesystem access
- Project analytics and reporting
- Competitive analysis (beats all 7 competitors)

### Architecture
- Go: MCP server, health checks, circuit breaker, gRPC orchestration
- Rust: Media processing, FFmpeg integration, SHA-256 fingerprinting
- Python: NLP, embeddings, AI decision making
- TypeScript: CEP/ExtendScript bridge, WebSocket, gRPC server
