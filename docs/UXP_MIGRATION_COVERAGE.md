# ExtendScript → UXP Coverage Matrix

> Assessment of migrating the CEP/ExtendScript host layer (`cep-panel/src/host/{core,premiere}.jsx`,
> **1,041 functions** backing **1,047 MCP tools**) to Adobe's UXP platform for Premiere Pro.
>
> **Confidence legend:** the verdicts below are a *desk assessment* of the UXP-for-Premiere API
> surface. Per-function certainty requires the **Phase 0 spike** against the actual Premiere
> 26.x `premierepro` UXP module. Treat ❓ as "must verify before committing."

## Verdict summary

| Verdict | Meaning | ~Tools | ~% |
|---|---|---:|---:|
| ✅ **Supported** | Clear UXP API exists; port is mechanical (sync→async, new API names) | ~175 | ~17% |
| ⚠️ **Partial** | Some operations map; others need workarounds or are missing | ~510 | ~49% |
| ❌ **Gap** | No UXP equivalent today (QE-DOM, host-UI control, ExtendScript-eval, sandboxed I/O) | ~360 | ~34% |

**Headline:** roughly a third of the surface has **no UXP path today**, concentrated in transitions/effects,
host-UI/workspace/menu control, and the arbitrary-eval/system escape hatches. A *full* 1:1 port is not
currently feasible — the realistic targets are an **MVP subset** (~core editing) or a **hybrid** (UXP for
what it supports, CEP retained for the gaps).

## Matrix by functional area

| Area | Tool files (count) | UXP backing API | Verdict | Notes / gaps |
|---|---|---|---|---|
| **Project** | project (23) | `Project`, `ProjectItem`, `Project.importFiles`, bins, `Project.save` | ✅ | Async + locked-access model. Import is well-supported. |
| **Sequence** | sequence (26) | `Project.getActiveSequence`, `createSequence`, `Sequence` settings | ✅ | Settings read/write supported. |
| **Clip editing (core)** | clip (29) | `Sequence` editing, `TrackItem`, in/out, `VideoClipTrackItem` | ⚠️ | Place/insert/overwrite OK; **razor/ripple/slip/slide** APIs are newer/partial. |
| **Advanced edit** | advanced_edit (31), assembly (30) | editing API + `TrackItemCollection` | ⚠️ | Inherits clip-editing limits; bulk assembly depends on per-op support. |
| **Import** | (in project + media_browser) | `Project.importFiles`, `importSequences`, `importAEComps` | ✅ | Strong. |
| **Export / Encoding** | export (14), encoding (30), delivery (30) | `Encoder`, `Exporter`, `EncoderManager` | ✅ / ⚠️ | Encoder API is a UXP **strength**. Delivery *checks* (loudness, black-frame) may need media-engine, not host. |
| **Frame capture** | capture (3) | `Exporter.exportSequenceFrame` | ✅ | Replaces the buggy `exportFrame` ExtendScript. |
| **Markers** | (in sequence/clip) | `Marker`, `MarkerCollection` | ✅ | Supported. |
| **Metadata** | metadata (30) | XMP / `ProjectItem` metadata | ✅ | Supported. |
| **Color / Lumetri** | color (30) | component params on `VideoComponentChain` | ⚠️❓ | Reading/writing Lumetri params via component API is **partial**; verify each control. |
| **Transform / motion** | transform (30) | Motion component params | ⚠️ | position/scale/opacity/anchor via component params — feasible but verify keyframes. |
| **Audio** | audio (32), audio_advanced (30) | track/clip volume, audio component params | ⚠️ | Gain/volume OK; **Essential Sound** is UI-driven → likely gap. |
| **Text / Graphics** | graphics (21), motion_graphics (30) | MOGRT import (`importMGT`), Essential Graphics text | ⚠️ | MOGRT import + text edit supported; **drawing primitives** (rect/line/circle) limited. |
| **Effects** | effects (36), effect_chain (30) | `VideoFilterFactory` / component add | ⚠️❓ | Adding *named* effects partial; **arbitrary effect catalog** access uncertain. |
| **Transitions** | (in effects/clip) | — | ❌ | Transitions were **QE-DOM**; no confirmed UXP transition API. **Critical gap.** |
| **Playback / monitor** | playback (30) | limited player control | ⚠️❓ | `setPlayerPosition`/play may be partial; verify. |
| **Templates / presets** | template (30) | sequence/export preset apply | ⚠️ | Preset *application* partial; preset *authoring* unlikely. |
| **Versioning / backup** | versioning (30) | `Project.save`/`saveAs` + UXP `fs` | ✅ | File-level + project save; rewrite `fs` calls. |
| **Diagnostics / Monitoring / Analytics** | diagnostics (30), monitoring (30), analytics (30) | derived from state reads | ⚠️ | OK where the underlying reads exist; some rely on QE/perf hooks → gap. |
| **Batch / Compound** | batch (30), compound (30) | orchestration over other ops | ⚠️ | Support == support of the ops they wrap. |
| **Camera / Multicam** | camera (30) | multicam creation | ⚠️❓ | Multicam was QE-assisted; verify. |
| **AI / intelligence** | ai (25) | host-side reads; logic in Python svc | ✅ | Host part is mostly reads; unaffected by ES→UXP. |
| **Media browser / stock** | media_browser (15) | UXP `fs` (`localFileSystem`) | ⚠️ | Drive browse via `fs` OK; CC Libraries/stock limited. |
| **Integration (AE/Audition/PS, AAF/OMF/EDL/XML)** | integration (28) | partial import; dynamic link | ❌⚠️ | Import of EDL/XML/AAF partial; **app round-trip / dynamic link** control likely gap. |
| **Host UI / Workspace / Panels** | ui (30), workspace (25), panel_ops (15) | — | ❌ | Controlling host window layout/panels/workspaces is **not a UXP capability**. |
| **Preferences** | preferences (30) | — | ❌ | App preference read/write **not exposed** in UXP. |
| **Keyboard shortcuts** | shortcut (30) | — | ❌ | No UXP shortcut API. |
| **Menu / key simulation / system cmd** | (in scripting/app) | — | ❌ | `executeMenuCommand`, AppleScript menu-click, `execute_system_command` — **sandboxed out** of UXP. |
| **Scripting escape hatches** | scripting (30) | JS only | ❌ | `execute_extendscript` / `evaluate_expression` / `run_qe_script` have **no meaning in UXP** (no ExtendScript, no QE). Replace with JS eval (different semantics) or drop. |
| **Immersive / VR / 360 / HDR** | immersive (30) | — | ❌❓ | VR/stereoscopic sequence control likely **not in UXP**; verify. |
| **Collaboration / Team Projects** | collaboration (30) | limited Team Projects API | ⚠️❓ | Production/Team Projects coverage uncertain. |

## The critical gaps (what kills a full port)

1. **No QE DOM.** UXP has no `qe` object. Everything built on it — most **transitions**, some effects, multicam, certain low-level ops — has no UXP path. This is the single biggest blocker.
2. **No host-UI/menu/workspace/preferences/shortcut control.** ~165 tools assume app-level automation (`executeMenuCommand`, panel/workspace/window control, prefs, shortcuts) that UXP deliberately does not expose.
3. **No ExtendScript / no arbitrary system access.** The `scripting_*` escape-hatch tools and `execute_system_command` are sandboxed away. (Today's `core.jsx` even shells out to AppleScript for menu clicks — impossible in UXP.)

## What gets *easier* / free wins

- **Modern JS** — delete the ES5 polyfills we just added (`toISOString`, `Array.indexOf/filter`, `Object.keys`); UXP is V8/ES2020+.
- **No reserved-word traps** — the `package:` class of compile errors disappears.
- **Cleaner transport** — UXP can't host a socket server, so the bridge becomes the WS **server** and the plugin connects out as client; this removes the "panel-loaded-before-bridge" race we hit.
- **Encoder & frame export** — first-class UXP APIs, replacing the flaky `exportFrame`.
- **Async/transactional model** matches the bridge's existing `requestId` request/response design.

## Recommended scope

Given ~34% has no UXP path, do **not** attempt full parity. Two viable strategies:

- **MVP-UXP (recommended):** port the ✅ + high-value ⚠️ areas (~project, sequence, clip editing, import, export, markers, metadata, color, transform, text/MOGRT) — roughly the **100–150 tools** that matter for real editing — and explicitly drop/defer the ❌ areas.
- **Hybrid:** UXP for supported areas, keep the CEP path behind a `bridgeMode` flag for the QE/UI/eval gaps until (if) Adobe closes them.

**Next step:** the Phase 0 spike turns the ❓/⚠️ rows into hard ✅/❌ by testing ~15 representative ops against the live Premiere 26.x UXP API. That converts this desk estimate into a commitment-grade plan.
