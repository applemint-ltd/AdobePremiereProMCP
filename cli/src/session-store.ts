/**
 * Persistence for the Slack bot's thread -> Claude session mapping and a
 * per-thread turn log.
 *
 * The mapping used to live in an in-memory Map, so every bot restart
 * (`launchctl kickstart -k` after any code change) orphaned all live Slack
 * threads — replies in existing threads silently started fresh sessions
 * with no memory. Write-through JSON on disk keeps threads resumable.
 */

import * as fs from "node:fs";
import * as path from "node:path";

interface SessionEntry {
  sessionId: string;
  updatedAt: string;
}

const MAX_ENTRY_AGE_DAYS = 30;

export class SessionStore {
  private readonly filePath: string;
  private readonly entries = new Map<string, SessionEntry>();

  constructor(filePath: string) {
    this.filePath = filePath;
    this.load();
  }

  get(threadKey: string): string | null {
    return this.entries.get(threadKey)?.sessionId ?? null;
  }

  set(threadKey: string, sessionId: string): void {
    this.entries.set(threadKey, { sessionId, updatedAt: new Date().toISOString() });
    this.save();
  }

  delete(threadKey: string): void {
    if (this.entries.delete(threadKey)) {
      this.save();
    }
  }

  private load(): void {
    let raw: string;
    try {
      raw = fs.readFileSync(this.filePath, "utf-8");
    } catch {
      return; // first run
    }
    try {
      const data = JSON.parse(raw) as Record<string, SessionEntry>;
      const cutoff = Date.now() - MAX_ENTRY_AGE_DAYS * 24 * 3600 * 1000;
      for (const [key, entry] of Object.entries(data)) {
        if (entry?.sessionId && Date.parse(entry.updatedAt) > cutoff) {
          this.entries.set(key, entry);
        }
      }
    } catch {
      // Corrupt store: start clean rather than crash the bot.
    }
  }

  private save(): void {
    const data = Object.fromEntries(this.entries);
    const tmp = `${this.filePath}.tmp`;
    try {
      fs.mkdirSync(path.dirname(this.filePath), { recursive: true });
      fs.writeFileSync(tmp, JSON.stringify(data, null, 2));
      fs.renameSync(tmp, this.filePath);
    } catch (err) {
      // Persistence is best-effort; the in-memory map still works.
      console.error(`session store save failed: ${err instanceof Error ? err.message : String(err)}`);
    }
  }
}

/** One line per bot turn, appended to a per-thread JSONL file. */
export interface TurnRecord {
  ts: string;
  prompt: string;
  session_id?: string;
  subtype?: string;
  is_error?: boolean;
  duration_ms?: number;
  total_cost_usd?: number;
  permission_denials?: number;
  error?: string;
}

export function appendTurnLog(dir: string, threadKey: string, rec: TurnRecord): void {
  try {
    fs.mkdirSync(dir, { recursive: true });
    // Thread keys are Slack timestamps ("1234567890.123456"); keep filenames tame.
    const safeKey = threadKey.replace(/[^0-9A-Za-z._-]/g, "_");
    fs.appendFileSync(path.join(dir, `${safeKey}.jsonl`), JSON.stringify(rec) + "\n");
  } catch (err) {
    console.error(`turn log append failed: ${err instanceof Error ? err.message : String(err)}`);
  }
}
