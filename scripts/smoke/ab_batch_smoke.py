#!/usr/bin/env python3
"""
A/B batch smoke test — the live gate for the bulk-shorts wave-1 tools.

Drives the real MCP server binary over stdio and asserts against GROUND
TRUTH: files on disk, ffprobe dimensions, and timeline read-back — never a
tool's self-report.

Covers: storyboard_expand_variants (one-variable diff gate + live
sequence-name collision check) -> assemble both variants (from 9:16 sources,
so the sequences and exports are vertical) -> batch_export_sequences with
file-truth await -> re-run converges (skipped, not re-rendered) ->
await_exports / export_status honesty -> premiere_export active-sequence
fallback -> Slack upload from an NFD-named (Japanese) directory, then cleans
up everything it created.

CANNOT run in CI — needs live Premiere Pro 2026 + ts-bridge + rust/python
services + AME, same prerequisites as golden_path_smoke.py.

Usage:
  set -a; source .env; set +a   # export SLACK_BOT_TOKEN to child processes
  python3 scripts/smoke/ab_batch_smoke.py
Exit code 0 iff every check passed.
"""

import json
import os
import shutil
import subprocess
import sys
import tempfile
import unicodedata

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
BIN = os.path.join(ROOT, "go-orchestrator", "bin", "premierpro-mcp")
MEDIA = os.path.join(ROOT, "scripts", "smoke", ".media")
CHANNEL = os.environ.get("SMOKE_SLACK_CHANNEL", "C0BEC9VBLP4")
HAVE_SLACK = bool(os.environ.get("SLACK_BOT_TOKEN"))
EXPERIMENT = "SMKAB"  # sequences SMKAB-* are created and deleted by this test
BIN_NAME = "AB smoke bin — safe to delete"
OUT_DIR = os.path.join(tempfile.gettempdir(), "ab_batch_smoke")

# Vertical finals need a dimension-preserving preset: h264_1080p resolves to
# High Quality 1080 HD.epr, which forces 1920x1080 (verified live 2026-07-17).
# Until the h264_match_source host key is live (needs a CEP panel reload),
# pass the Match Source .epr path directly — the resolver uses .epr paths
# as-is.
MATCH_SOURCE_EPR = os.environ.get(
    "SMOKE_MATCH_SOURCE_EPR",
    "/Applications/Adobe Premiere Pro 2026/Adobe Premiere Pro 2026.app"
    "/Contents/MediaIO/systempresets/4E49434B_48323634/01 - Match Source - High bitrate.epr")

passed, failed, notes = [], [], []


def check(name, ok, detail=""):
    (passed if ok else failed).append(name)
    print(("PASS " if ok else "FAIL ") + name + (f" — {detail}" if detail else ""), flush=True)


def ffprobe_dims(path):
    try:
        out = subprocess.run(
            ["ffprobe", "-v", "error", "-select_streams", "v:0",
             "-show_entries", "stream=width,height", "-of", "csv=p=0", path],
            check=True, capture_output=True, text=True).stdout.strip()
        w, h = out.split(",")[:2]
        return int(w), int(h)
    except Exception:
        return 0, 0


def ensure_media():
    """Synthesize two 9:16 clips — vertical sources make vertical sequences."""
    os.makedirs(MEDIA, exist_ok=True)
    for name, color, dur, freq in [("ab_red.mp4", "red", 5, 440), ("ab_blue.mp4", "blue", 6, 220)]:
        p = os.path.join(MEDIA, name)
        if not os.path.exists(p):
            subprocess.run(
                ["ffmpeg", "-y", "-f", "lavfi", "-i", f"color=c={color}:s=720x1280:r=30:d={dur}",
                 "-f", "lavfi", "-i", f"sine=frequency={freq}:duration={dur}",
                 "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", p],
                check=True, capture_output=True)


BASE_STORYBOARD = {
    "version": "storyboard/v1",
    "title": "AB smoke base",
    "scenes": [
        {"name": "Hook", "shots": [
            {"clip": os.path.join(MEDIA, "ab_red.mp4"), "duration_seconds": 3,
             "text": [{"content": "HOOK A", "style": "title"}],
             "caption": "Hook line one."},
        ]},
        {"name": "Body", "shots": [
            {"clip": os.path.join(MEDIA, "ab_blue.mp4"), "duration_seconds": 4,
             "caption": "Body line."},
        ]},
    ],
}

VARIANTS = [
    {"variant_id": "hookA", "overrides": {}},
    {"variant_id": "hookB",
     "overrides": {"scenes[0].shots[0].text[0].content": "HOOK B?"}},
]


class Server:
    def __init__(self):
        env = dict(os.environ,
                   PREMIERE_AUDIT_DIR=os.path.join(ROOT, "scripts", "logs", "audit"),
                   PREMIERE_LOG_DIR=os.path.join(ROOT, "scripts", "logs"),
                   PREMIERE_SESSION_TAG="ab-batch-smoke")
        self.p = subprocess.Popen(
            [BIN, "--transport", "stdio", "--log-level", "error"],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
            text=True, env=env)
        self._id = 0
        self._rpc("initialize", {"protocolVersion": "2024-11-05", "capabilities": {},
                                 "clientInfo": {"name": "ab-smoke", "version": "1"}})
        self.p.stdin.write(json.dumps({"jsonrpc": "2.0", "method": "notifications/initialized"}) + "\n")
        self.p.stdin.flush()

    def _rpc(self, method, params):
        self._id += 1
        self.p.stdin.write(json.dumps({"jsonrpc": "2.0", "id": self._id,
                                       "method": method, "params": params}) + "\n")
        self.p.stdin.flush()
        while True:
            line = self.p.stdout.readline()
            if not line:
                raise RuntimeError("server exited")
            msg = json.loads(line)
            if msg.get("id") == self._id:
                return msg

    def call(self, tool, args=None):
        msg = self._rpc("tools/call", {"name": tool, "arguments": args or {}})
        if "error" in msg:
            return False, msg["error"].get("message", str(msg["error"]))
        res = msg["result"]
        text = next((c["text"] for c in res.get("content", []) if c.get("type") == "text"), "")
        if res.get("isError"):
            return False, text
        try:
            payload = json.loads(text)
        except Exception:
            payload = text
        if isinstance(payload, dict) and isinstance(payload.get("message"), str):
            try:
                payload = json.loads(payload["message"])
            except Exception:
                pass
        return True, payload

    def close(self):
        self.p.terminate()


def delete_smoke_sequences(s):
    """Delete every SMKAB-* sequence, one at a time (indices shift)."""
    deleted = 0
    while True:
        ok, seqs = s.call("premiere_get_sequence_list")
        if not ok:
            return deleted
        target = next((x for x in seqs.get("sequences", [])
                       if (x.get("name") or "").startswith(EXPERIMENT + "-")), None)
        if not target:
            return deleted
        s.call("premiere_delete_sequence", {"sequence_index": target["index"]})
        deleted += 1


def main():
    if not os.path.exists(BIN):
        print(f"FAIL: server binary not built at {BIN}")
        sys.exit(2)
    ensure_media()
    shutil.rmtree(OUT_DIR, ignore_errors=True)
    os.makedirs(OUT_DIR, exist_ok=True)
    if not HAVE_SLACK:
        notes.append("SLACK_BOT_TOKEN not set — Slack NFD-path leg skipped")

    s = Server()
    try:
        ok, ping = s.call("premiere_ping")
        check("ping", ok and isinstance(ping, dict) and ping.get("premiere_running"),
              f"Premiere {ping.get('premiere_version') if isinstance(ping, dict) else ping}")

        # --- FR-03: expansion + diff gate + live collision check ---
        base_json = json.dumps(BASE_STORYBOARD)
        # hookA is deliberately identical to base, so run without the gate for
        # it; the gate legs get their own calls below.
        ok, exp = s.call("premiere_storyboard_expand_variants", {
            "storyboard_json": base_json, "experiment_id": EXPERIMENT,
            "variants_json": json.dumps(VARIANTS), "enforce_single_variable": False})
        names = [v["sequence_name"] for v in exp.get("variants", [])] if ok else []
        check("expand: 2 variants named", ok and names == [f"{EXPERIMENT}-hookA", f"{EXPERIMENT}-hookB"],
              ", ".join(names) if ok else str(exp)[:200])
        hookB_changed = exp["variants"][1]["changed_paths"] if ok else []
        check("expand: hookB changed exactly the hook text",
              hookB_changed == ["scenes[0].shots[0].text[0].content"], str(hookB_changed))

        ok, gate = s.call("premiere_storyboard_expand_variants", {
            "storyboard_json": base_json, "experiment_id": EXPERIMENT + "GATE",
            "variants_json": json.dumps([{
                "variant_id": "sloppy",
                "overrides": {"scenes[0].shots[0].text[0].content": "X",
                              "scenes[1].shots[0].duration_seconds": 5},
            }])})
        check("expand: two-field variant rejected, both paths named",
              not ok and "text[0].content" in str(gate) and "duration_seconds" in str(gate),
              str(gate)[:160])

        # --- assemble both variants (storyboard carries its stamped name) ---
        # Guard on expansion having produced variants: a failed expand leaves
        # `exp` as an error payload, and iterating it would crash the harness
        # instead of recording the FAILs above.
        expanded = exp.get("variants", []) if isinstance(exp, dict) else []
        if not expanded:
            check("expand produced variants to assemble", False, "no variants — skipping dependent steps")
        for v in expanded:
            ok, rep = s.call("premiere_assemble_storyboard",
                             {"storyboard_json": json.dumps(v["storyboard"])})
            placed = [x for x in rep.get("shots", []) if x.get("status") == "placed"] if ok else []
            check(f"assemble {v['variant_id']}: 2 shots placed", ok and len(placed) == 2,
                  rep.get("summary", "")[:100] if ok else str(rep)[:200])

        ok, seqs = s.call("premiere_get_sequence_list")
        have = {x["name"] for x in seqs.get("sequences", [])} if ok else set()
        check("sequences exist", {f"{EXPERIMENT}-hookA", f"{EXPERIMENT}-hookB"} <= have)

        # Live collision check: the same expansion must now refuse.
        ok, col = s.call("premiere_storyboard_expand_variants", {
            "storyboard_json": base_json, "experiment_id": EXPERIMENT,
            "variants_json": json.dumps(VARIANTS), "enforce_single_variable": False})
        check("expand: live name collision refused",
              not ok and "already exist" in str(col), str(col)[:140])

        # --- FR-04: batch export with file-truth await ---
        preset = MATCH_SOURCE_EPR if os.path.exists(MATCH_SOURCE_EPR) else "h264_match_source"
        ok, batch = s.call("premiere_batch_export_sequences", {
            "sequence_names": [f"{EXPERIMENT}-hookA", f"{EXPERIMENT}-hookB"],
            "preset": preset,
            "output_dir": OUT_DIR, "per_file_timeout_s": 300})
        items = {i.get("sequence_name"): i for i in batch.get("items", [])} if ok else {}
        check("batch: both done", ok and batch.get("done") == 2,
              json.dumps({k: v.get("status") for k, v in items.items()}) if ok else str(batch)[:250])
        for name, it in items.items():
            path = it.get("path", "")
            exists = os.path.exists(path)
            w, h = ffprobe_dims(path) if exists else (0, 0)
            check(f"batch: {name} on disk, vertical", exists and (w, h) == (720, 1280),
                  f"{w}x{h}, {it.get('bytes', 0)} bytes, {round(it.get('wall_seconds', 0), 1)}s")

        # Re-run must converge: everything skipped, nothing re-rendered.
        ok, rerun = s.call("premiere_batch_export_sequences", {
            "sequence_names": [f"{EXPERIMENT}-hookA", f"{EXPERIMENT}-hookB"],
            "output_dir": OUT_DIR, "per_file_timeout_s": 60})
        check("batch re-run: both skipped", ok and rerun.get("skipped") == 2,
              json.dumps([i.get("status") for i in rerun.get("items", [])]) if ok else str(rerun)[:200])

        # --- FR-05: await/status honesty ---
        done_path = items.get(f"{EXPERIMENT}-hookA", {}).get("path", "")
        ok, aw = s.call("premiere_await_exports",
                        {"paths": [done_path], "timeout_s": 30, "poll_interval_s": 0.5, "stable_for_s": 1})
        check("await_exports: finished file all_done", ok and aw.get("status") == "all_done",
              json.dumps(aw.get("files", [{}])[0])[:140] if ok else str(aw)[:200])

        bogus = os.path.join(OUT_DIR, "never_rendered.mp4")
        ok, aw2 = s.call("premiere_await_exports", {"paths": [bogus], "timeout_s": 4, "poll_interval_s": 0.5})
        f0 = (aw2.get("files") or [{}])[0] if ok else {}
        check("await_exports: bogus path honest timeout",
              ok and aw2.get("status") == "timeout" and not f0.get("done") and not f0.get("exists"),
              f0.get("note", "")[:80] if ok else str(aw2)[:200])

        ok, st = s.call("premiere_export_status", {"paths": [done_path]})
        f0 = (st.get("files") or [{}])[0] if ok else {}
        check("export_status: snapshot never certifies done",
              ok and f0.get("exists") and f0.get("stable") and not f0.get("done"),
              f"{f0.get('bytes', 0)} bytes")

        # --- FR-04: premiere_export active-sequence fallback ---
        fallback_out = os.path.join(OUT_DIR, "active_fallback.mp4")
        ok, ex = s.call("premiere_export", {"output_path": fallback_out, "preset": "h264_1080p"})
        check("export: no sequence_id accepted (active fallback)", ok, str(ex)[:160])
        if ok:
            ok, aw3 = s.call("premiere_await_exports", {"paths": [fallback_out], "timeout_s": 240})
            check("export: fallback file confirmed on disk", ok and aw3.get("status") == "all_done",
                  json.dumps((aw3.get("files") or [{}])[0])[:140] if ok else str(aw3)[:200])

        # --- FR-12: Slack upload from an NFD-named (Japanese) directory ---
        if HAVE_SLACK and done_path:
            nfd_dir = os.path.join(tempfile.gettempdir(),
                                   unicodedata.normalize("NFD", "ABスモーク検証"))
            os.makedirs(nfd_dir, exist_ok=True)
            nfd_file = os.path.join(nfd_dir, os.path.basename(done_path))
            shutil.copy(done_path, nfd_file)
            ok, up = s.call("premiere_post_file_to_slack", {
                "file_path": nfd_file, "channel_id": CHANNEL,
                "title": "AB smoke — NFD path upload",
                "comment": "Automated ab_batch_smoke: upload from an NFD-normalized Japanese directory (FR-12)."})
            check("slack: upload from NFD-named dir", ok and up.get("status") == "uploaded",
                  json.dumps(up)[:120] if ok else str(up)[:200])
            shutil.rmtree(nfd_dir, ignore_errors=True)

    finally:
        print("\n--- cleanup ---", flush=True)
        n = delete_smoke_sequences(s)
        print(f"deleted {n} smoke sequences", flush=True)
        s.call("premiere_create_bin", {"name": BIN_NAME})
        moved = failed_moves = 0
        ok, proj_items = s.call("premiere_get_project_items")
        if ok:
            for it in proj_items.get("items", []):
                nm = it.get("name", "")
                if nm.startswith("ab_") or nm.startswith("[SB]") or nm.startswith("storyboard_captions"):
                    mok, _ = s.call("premiere_move_bin_item", {"item_path": nm, "dest_bin": BIN_NAME})
                    moved += 1 if mok else 0
                    failed_moves += 0 if mok else 1
        if failed_moves == 0:
            s.call("premiere_delete_bin", {"bin_path": BIN_NAME})
        print(f"binned {moved} test items ({failed_moves} failed)", flush=True)
        shutil.rmtree(OUT_DIR, ignore_errors=True)
        s.close()

    print("\n===== A/B BATCH SMOKE =====")
    print(f"PASS {len(passed)} / FAIL {len(failed)}")
    for f_ in failed:
        print("  FAIL:", f_)
    for note in notes:
        print("  note:", note)
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
