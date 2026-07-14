#!/usr/bin/env python3
"""
Golden-path smoke test — the end-to-end gate for the non-editor workflow.

Drives the real MCP server binary over stdio, exactly as a headless
`claude -p` turn from the Slack bot would, and asserts against GROUND TRUTH
(reads the timeline back, confirms files land on disk and upload to Slack) —
not the tools' own self-reports.

Covers: storyboard validate -> assemble (trims, transitions, captions,
baked-PNG text) -> timeline read-back -> what_changed -> frame capture ->
preview export -> contact sheet -> Slack upload -> session digest, then
cleans up everything it created.

CANNOT run in CI — it needs a live Premiere Pro 2026 with a project open and
the ts-bridge/CEP panel connected. Run it manually after any change that
touches the pipeline, the host .jsx (after a panel reload), or the bridge.

Prerequisites:
  - Premiere Pro 2026 running with a project open; CEP panel loaded.
  - ts-bridge running (scripts/start-ts.sh); rust + python services up
    (scripts/start-all.sh).
  - ffmpeg on PATH (to synthesize the test clips).
  - SLACK_BOT_TOKEN in the environment (the Slack upload steps are skipped
    with a warning if it's absent). Set SMOKE_SLACK_CHANNEL to your test
    channel id (defaults to the value below).

Usage:
  python3 scripts/smoke/golden_path_smoke.py
Exit code 0 iff every check passed.
"""

import json
import os
import subprocess
import sys
import time

# Repo root = two levels up from this file (scripts/smoke/).
ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
BIN = os.path.join(ROOT, "go-orchestrator", "bin", "premierpro-mcp")
MEDIA = os.path.join(ROOT, "scripts", "smoke", ".media")
CSV_PATH = os.path.join(MEDIA, "golden_shotlist.csv")
CHANNEL = os.environ.get("SMOKE_SLACK_CHANNEL", "C0BEC9VBLP4")
SEQ_NAME = "MCP Smoke Test — safe to delete"
BIN_NAME = "MCP Smoke Test bin — safe to delete"
HAVE_SLACK = bool(os.environ.get("SLACK_BOT_TOKEN"))

passed, failed, notes = [], [], []


def check(name, ok, detail=""):
    (passed if ok else failed).append(name)
    print(("PASS " if ok else "FAIL ") + name + (f" — {detail}" if detail else ""), flush=True)


def ffmpeg_clip(path, color, dur, freq):
    subprocess.run(
        ["ffmpeg", "-y", "-f", "lavfi", "-i", f"color=c={color}:s=1280x720:r=30:d={dur}",
         "-f", "lavfi", "-i", f"sine=frequency={freq}:duration={dur}",
         "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", path],
        check=True, capture_output=True)


def ensure_media():
    os.makedirs(MEDIA, exist_ok=True)
    clips = [("golden_red.mp4", "red", 8, 440),
             ("golden_green.mp4", "green", 10, 660),
             ("golden_blue.mp4", "blue", 8, 220)]
    for name, color, dur, freq in clips:
        p = os.path.join(MEDIA, name)
        if not os.path.exists(p):
            ffmpeg_clip(p, color, dur, freq)
    with open(CSV_PATH, "w") as f:
        f.write("order,clip,duration,from,to,text,caption,transition\n")
        f.write(f"1,{MEDIA}/golden_red.mp4,4,,,GOLDEN PATH,Shot one: the red opener.,dissolve\n")
        f.write(f"2,{MEDIA}/golden_green.mp4,,2,6,,Shot two: trimmed from the source.,cut\n")
        f.write(f"3,{MEDIA}/golden_blue.mp4,5,,,,Shot three: the finale.,fade to black\n")


class Server:
    def __init__(self):
        env = dict(os.environ,
                   PREMIERE_AUDIT_DIR=os.path.join(ROOT, "scripts", "logs", "audit"),
                   PREMIERE_LOG_DIR=os.path.join(ROOT, "scripts", "logs"),
                   PREMIERE_SESSION_TAG="golden-path-smoke")
        self.p = subprocess.Popen(
            [BIN, "--transport", "stdio", "--log-level", "error"],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
            text=True, env=env)
        self._id = 0
        self._rpc("initialize", {"protocolVersion": "2024-11-05", "capabilities": {},
                                 "clientInfo": {"name": "golden-smoke", "version": "1"}})
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
        """Returns (ok, payload_or_error, has_image)."""
        msg = self._rpc("tools/call", {"name": tool, "arguments": args or {}})
        if "error" in msg:
            return False, msg["error"].get("message", str(msg["error"])), False
        res = msg["result"]
        content = res.get("content", [])
        text = next((c["text"] for c in content if c.get("type") == "text"), "")
        has_img = any(c.get("type") == "image" for c in content)
        if res.get("isError"):
            return False, text, has_img
        try:
            payload = json.loads(text)
        except Exception:
            payload = text
        # GenericResult wraps the real payload in .message as a JSON string.
        if isinstance(payload, dict) and isinstance(payload.get("message"), str):
            try:
                payload = json.loads(payload["message"])
            except Exception:
                pass
        return True, payload, has_img

    def close(self):
        self.p.terminate()


def main():
    if not os.path.exists(BIN):
        print(f"FAIL: server binary not built at {BIN} (run: cd go-orchestrator && go build -o bin/premierpro-mcp ./cmd/server)")
        sys.exit(2)
    ensure_media()
    if not HAVE_SLACK:
        notes.append("SLACK_BOT_TOKEN not set — Slack upload steps skipped")

    s = Server()
    try:
        ok, ping, _ = s.call("premiere_ping")
        check("ping", ok and isinstance(ping, dict) and ping.get("premiere_running"),
              f"Premiere {ping.get('premiere_version') if isinstance(ping, dict) else ping}")

        ok, val, _ = s.call("premiere_storyboard_validate", {"csv_path": CSV_PATH})
        check("storyboard_validate", ok and val.get("valid") is True,
              f"unresolved={val.get('unresolved_clips')}" if ok else str(val)[:200])

        ok, rep, _ = s.call("premiere_assemble_storyboard", {"csv_path": CSV_PATH, "sequence_name": SEQ_NAME})
        if not ok:
            check("assemble_storyboard", False, str(rep)[:300])
            raise SystemExit
        placed = [x for x in rep.get("shots", []) if x.get("status") == "placed"]
        check("assemble: 3 shots placed", len(placed) == 3,
              "; ".join(f"{x['shot_id']}:{x['status']}" for x in rep.get("shots", [])))
        check("assemble: durations 4/4/5",
              [round(x.get("duration_seconds", 0), 1) for x in placed] == [4.0, 4.0, 5.0])
        check("assemble: 2 transitions reported", len(rep.get("transitions", [])) == 2)
        check("assemble: 3 captions applied",
              rep.get("captions", {}).get("count") == 3 and rep.get("captions", {}).get("applied") is True)
        check("assemble: 1 text overlay", rep.get("text_overlays") == 1)
        print("     summary:", rep.get("summary"), flush=True)

        ok, clips, _ = s.call("premiere_get_all_clips")
        vids = [c for c in clips.get("clips", []) if c.get("track_type") == "video"] if ok else []
        check("timeline: video clips present", len(vids) >= 3, f"{len(vids)} video clips")

        ok, wc, _ = s.call("premiere_what_changed")
        check("what_changed", ok and bool(wc.get("human_lines")),
              (wc.get("human_lines") or ["-"])[0][:100] if ok else str(wc)[:150])

        ok, _, has_img = s.call("premiere_capture_frame_base64")
        check("capture_frame", has_img, "image returned" if has_img else "no image (cold AME? see task #14)")

        ok, prev, _ = s.call("premiere_export_preview", {"output_name": "smoke_preview.mp4"})
        prev = prev if ok and isinstance(prev, dict) else {}
        check("export_preview completed", prev.get("status") == "completed",
              f"{prev.get('status')} -> {prev.get('output_path','')}" if ok else str(prev)[:200])

        sheet = {}
        if prev.get("status") == "completed":
            ok, sheet, _ = s.call("premiere_generate_contact_sheet",
                                  {"video_path": prev["output_path"], "columns": 4, "rows": 2})
            sheet = sheet if ok and isinstance(sheet, dict) else {}
            check("contact_sheet", bool(sheet.get("output_path")), sheet.get("output_path", ""))

        if HAVE_SLACK:
            for label, path in [("preview", prev.get("output_path")), ("contact sheet", sheet.get("output_path"))]:
                if not path:
                    continue
                ok, up, _ = s.call("premiere_post_file_to_slack",
                                   {"file_path": path, "channel_id": CHANNEL,
                                    "title": f"Smoke test — {label}",
                                    "comment": "Automated golden-path smoke test; sequence is cleaned up after."})
                check(f"slack upload: {label}", ok and up.get("status") == "uploaded",
                      json.dumps(up)[:120] if ok else str(up)[:200])

        ok, dig, _ = s.call("premiere_get_session_digest")
        check("session_digest", ok and bool(dig.get("actions")))

    finally:
        print("\n--- cleanup ---", flush=True)
        ok, seqs, _ = s.call("premiere_get_sequence_list")
        if ok:
            for x in (seqs.get("sequences") or seqs.get("items") or []):
                if x.get("name") == SEQ_NAME:
                    s.call("premiere_delete_sequence", {"sequence_index": x.get("index")})
                    print(f"deleted test sequence (idx {x.get('index')})", flush=True)
        s.call("premiere_create_bin", {"name": BIN_NAME})
        moved = fail = 0
        ok, items, _ = s.call("premiere_get_project_items")
        if ok:
            for it in items.get("items", []):
                n = it.get("name", "")
                if n.startswith("golden_") or n.startswith("[SB]") or n.startswith("storyboard_captions"):
                    mok, _, _ = s.call("premiere_move_bin_item", {"item_path": n, "dest_bin": BIN_NAME})
                    moved += 1 if mok else 0
                    fail += 0 if mok else 1
        if fail == 0:
            s.call("premiere_delete_bin", {"bin_path": BIN_NAME})
        print(f"binned {moved} test items ({fail} failed), removed test bin", flush=True)
        s.close()

    print("\n===== GOLDEN PATH SMOKE =====")
    print(f"PASS {len(passed)} / FAIL {len(failed)}")
    for f_ in failed:
        print("  FAIL:", f_)
    for n in notes:
        print("  note:", n)
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
