#!/usr/bin/env python3
"""
Core-tools smoke test — exercises the curated editing tools that the golden
path doesn't directly cover (transforms, Lumetri, trims, audio leveling,
caption editing) against a real test sequence, so a tool that quietly breaks
on a Premiere update gets caught.

Complements golden_path_smoke.py. Same prerequisites (see README). Builds a
test bed via the storyboard, exercises each tool, cleans up. Every listed
tool is EXPECTED to succeed — a failure means either a real regression or a
tool that should be demoted from coreTools (internal/mcp/core_toolset.go).

Usage:  python3 scripts/smoke/core_tools_smoke.py
Exit 0 iff all expected-core tools pass.
"""
import json, os, subprocess, sys

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
BIN = os.path.join(ROOT, "go-orchestrator", "bin", "premierpro-mcp")
MEDIA = os.path.join(ROOT, "scripts", "smoke", ".media")
CSV = os.path.join(MEDIA, "golden_shotlist.csv")
SEQ = "MCP Core Smoke — safe to delete"
BIN_NAME = "MCP Core Smoke bin — safe to delete"

results = []
def rec(tool, ok, detail=""):
    results.append((tool, ok))
    print(f"{'PASS' if ok else 'FAIL'} {tool}{(' — ' + detail) if detail and not ok else ''}", flush=True)

proc = subprocess.Popen([BIN, "--transport", "stdio", "--log-level", "error"],
    stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True,
    env=dict(os.environ, PREMIERE_AUDIT_DIR=os.path.join(ROOT, "scripts", "logs", "audit"),
             PREMIERE_SESSION_TAG="core-smoke"))
_id = 0
def call(t, a=None):
    global _id; _id += 1
    proc.stdin.write(json.dumps({"jsonrpc": "2.0", "id": _id, "method": "tools/call",
                                 "params": {"name": t, "arguments": a or {}}}) + "\n")
    proc.stdin.flush()
    while True:
        line = proc.stdout.readline()
        if not line:
            return False, "server exited"
        m = json.loads(line)
        if m.get("id") == _id:
            res = m.get("result", m.get("error"))
            if isinstance(res, dict) and "content" in res:
                txt = next((c["text"] for c in res["content"] if c.get("type") == "text"), "")
                if res.get("isError"):
                    return False, txt
                try:
                    p = json.loads(txt)
                    if isinstance(p, dict) and isinstance(p.get("message"), str):
                        try: p = json.loads(p["message"])
                        except Exception: pass
                    return True, p
                except Exception:
                    return True, txt
            return False, str(res)

_id += 1
proc.stdin.write(json.dumps({"jsonrpc": "2.0", "id": _id, "method": "initialize",
                             "params": {"protocolVersion": "2024-11-05", "capabilities": {},
                                        "clientInfo": {"name": "core-smoke", "version": "1"}}}) + "\n")
proc.stdin.flush(); proc.stdout.readline()
proc.stdin.write(json.dumps({"jsonrpc": "2.0", "method": "notifications/initialized"}) + "\n")
proc.stdin.flush()

def run(tool, args):
    ok, r = call(tool, args)
    rec(tool, ok, "" if ok else str(r)[:110])

try:
    ok, _ = call("premiere_ping")
    if not ok:
        print("Premiere not reachable; abort"); sys.exit(2)
    if not os.path.exists(CSV):
        print("run golden_path_smoke.py first (or generate .media/); missing test CSV"); sys.exit(2)
    ok, rep = call("premiere_assemble_storyboard", {"csv_path": CSV, "sequence_name": SEQ})
    if not ok or len([s for s in rep.get("shots", []) if s.get("status") == "placed"]) < 3:
        print("test bed assembly failed:", str(rep)[:200]); sys.exit(2)

    V = {"track_type": "video", "track_index": 0}
    # Transforms
    run("premiere_set_opacity", {"track_index": 0, "clip_index": 0, "opacity": 55})
    run("premiere_set_scale", {"track_index": 0, "clip_index": 0, "scale": 80})
    run("premiere_set_rotation", {"track_index": 0, "clip_index": 0, "degrees": 12})
    run("premiere_set_position", {"track_index": 0, "clip_index": 0, "x": 900, "y": 500})
    run("premiere_set_blend_mode", {"track_index": 0, "clip_index": 0, "mode": "Screen"})
    run("premiere_get_motion_properties", {"track_index": 0, "clip_index": 0})
    run("premiere_reset_transform", {"track_index": 0, "clip_index": 0})
    # Lumetri (the family that had the stray-"2" bug)
    run("premiere_lumetri_set_exposure", {"track_index": 0, "clip_index": 0, "value": 0.5})
    run("premiere_lumetri_set_contrast", {"track_index": 0, "clip_index": 0, "value": 10})
    run("premiere_lumetri_set_temperature", {"track_index": 0, "clip_index": 0, "value": 15})
    run("premiere_lumetri_set_saturation", {"track_index": 0, "clip_index": 0, "value": 120})
    run("premiere_lumetri_set_highlights", {"track_index": 0, "clip_index": 0, "value": 10})
    run("premiere_lumetri_set_tint", {"track_index": 0, "clip_index": 0, "value": 5})
    # Trims / slip / slide (test bed has 3 adjacent clips)
    run("premiere_roll_trim", {**V, "clip_index": 0, "delta_seconds": 0.3})
    run("premiere_ripple_trim", {**V, "clip_index": 1, "trim_end": True, "delta_seconds": -0.3})
    run("premiere_slip_clip", {**V, "clip_index": 1, "delta_seconds": 0.2})
    run("premiere_slide_clip", {**V, "clip_index": 1, "delta_seconds": 0.2})
    run("premiere_freeze_frame", {"track_index": 0, "clip_index": 0, "time": 0.5, "duration": 1.0})
    # Audio leveling — index-based per-clip path works on 2026 (set_audio_gain
    # and set_audio_track_volume don't; set_audio_level uses a clip_id, tested
    # elsewhere). normalize_audio is the verified index-based leveling tool.
    run("premiere_normalize_audio", {"track_type": "audio", "track_index": 0, "clip_index": 0})
    run("premiere_add_audio_crossfade", {"track_index": 0, "clip_index": 0, "duration": 1.0, "type": "Constant Power"})
    # Captions write/adjust (read is unavailable on 2026)
    run("premiere_adjust_subtitle_timing", {"track_index": 0, "offset_seconds": 0.3})
    run("premiere_set_caption_position", {"track_index": 0, "caption_index": 0, "x": 0.5, "y": 0.88})

finally:
    print("\n--- cleanup ---", flush=True)
    ok, sl = call("premiere_get_sequence_list")
    if ok:
        for s in (sl.get("sequences") or sl.get("items") or []):
            if s.get("name") == SEQ:
                call("premiere_delete_sequence", {"sequence_index": s["index"]})
    call("premiere_create_bin", {"name": BIN_NAME})
    ok, items = call("premiere_get_project_items")
    if ok:
        for it in items.get("items", []):
            n = it.get("name", "")
            if n.startswith("golden_") or n.startswith("[SB]") or n.startswith("storyboard_captions"):
                call("premiere_move_bin_item", {"item_path": n, "dest_bin": BIN_NAME})
    call("premiere_delete_bin", {"bin_path": BIN_NAME})
    print("cleaned up", flush=True)
    proc.terminate()

fails = [t for t, ok in results if not ok]
print("\n===== CORE TOOLS SMOKE =====")
print(f"PASS {sum(1 for _, ok in results if ok)} / FAIL {len(fails)} of {len(results)}")
for t in fails:
    print("  FAIL:", t)
sys.exit(1 if fails else 0)
