#!/usr/bin/env node

/**
 * PremierPro Slack Bot
 *
 * Listens for messages in one dedicated Slack channel (Socket Mode -- no
 * public webhook needed) and forwards each one to the real `claude` CLI in
 * headless mode, scoped to the premierpro-mcp tools. This runs against
 * whatever the operator is already logged into via `claude login` (Pro/Max/
 * Team subscription usage), NOT the Anthropic API/console -- unlike the
 * interactive CLI (index.ts), it never touches ANTHROPIC_API_KEY.
 *
 * Conversation continuity across Slack messages is done via `claude`'s own
 * session resume (--resume <session_id>), so one video project's whole
 * back-and-forth is a single ongoing Claude Code session.
 */

import "./load-env.js";
import { App } from "@slack/bolt";
import { spawn } from "node:child_process";
import * as path from "node:path";
import { printError, printInfo, printSuccess } from "./ui.js";

const RESET_COMMANDS = new Set(["new project", "reset", "start over"]);
const PROJECT_ROOT = path.resolve(import.meta.dirname, "..", "..");
const ALLOWED_TOOLS = "mcp__premierpro-mcp__*";

const SYSTEM_PROMPT_ADDITION = `You are responding inside a Slack channel where teammates ask you to make
edits in Adobe Premiere Pro. You have access only to the premierpro-mcp
tools (project, timeline, effects, export, etc.) -- no Bash, file, or web
tools. Use them to fulfill each request. If Premiere Pro is not running,
launch it first. Keep replies short, plain, and conversational (this is a
Slack message, not a report) -- summarize what you did rather than dumping
tool output.`;

interface ClaudeResult {
  result?: string;
  is_error: boolean;
  subtype?: string;
  session_id: string;
  total_cost_usd?: number;
  permission_denials?: unknown[];
}

interface IncomingMessage {
  text?: string;
  ts: string;
  channel: string;
  subtype?: string;
  bot_id?: string;
  user?: string;
}

let currentSessionId: string | null = null;

/** Spawn `claude -p` for one turn. No shell involved, so Slack message text can't inject flags. */
function runClaude(text: string, resumeSessionId: string | null): Promise<ClaudeResult> {
  return new Promise((resolve, reject) => {
    const args = [
      "-p",
      "--output-format", "json",
      "--allowedTools", ALLOWED_TOOLS,
      "--append-system-prompt", SYSTEM_PROMPT_ADDITION,
    ];
    if (resumeSessionId) {
      args.push("--resume", resumeSessionId);
    }
    args.push(text);

    const child = spawn("claude", args, { cwd: PROJECT_ROOT });

    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (chunk) => (stdout += chunk));
    child.stderr.on("data", (chunk) => (stderr += chunk));
    child.on("error", reject);
    child.on("close", (code) => {
      if (!stdout.trim()) {
        reject(new Error(stderr.trim() || `claude exited with code ${code} and no output`));
        return;
      }
      try {
        resolve(JSON.parse(stdout) as ClaudeResult);
      } catch {
        reject(new Error(`Failed to parse claude output as JSON: ${stdout.slice(0, 500)}`));
      }
    });
  });
}

/** Run a turn, transparently retrying once without --resume if the session was invalid/expired. */
async function runClaudeWithRetry(text: string): Promise<ClaudeResult> {
  try {
    return await runClaude(text, currentSessionId);
  } catch (err) {
    if (currentSessionId) {
      const msg = err instanceof Error ? err.message : String(err);
      printError(`Resume failed (${msg}), retrying as a fresh session...`);
      currentSessionId = null;
      return runClaude(text, null);
    }
    throw err;
  }
}

async function main(): Promise<void> {
  const botToken = process.env.SLACK_BOT_TOKEN;
  const appToken = process.env.SLACK_APP_TOKEN;
  const channelId = process.env.SLACK_CHANNEL_ID;

  if (!botToken || !appToken || !channelId) {
    printError("Missing Slack configuration.");
    console.log();
    console.log("  Set these in your .env (see .env.example):");
    console.log("    SLACK_BOT_TOKEN=xoxb-...");
    console.log("    SLACK_APP_TOKEN=xapp-...");
    console.log("    SLACK_CHANNEL_ID=C0123456789");
    console.log();
    process.exit(1);
  }

  printInfo("  Starting PremierPro Slack bot (headless `claude`, subscription auth)...");

  const app = new App({
    token: botToken,
    appToken,
    socketMode: true,
  });

  // Serialize all requests -- only one Premiere Pro instance is being driven,
  // so two Slack messages arriving close together must not fire concurrent
  // tool calls against the same live project.
  let queue: Promise<void> = Promise.resolve();

  app.message(async ({ message, say, client }) => {
    const msg = message as IncomingMessage;

    if (msg.channel !== channelId) return;
    if (msg.subtype || msg.bot_id) return; // ignore edits, joins, and our own messages
    const text = msg.text?.trim();
    if (!text) return;

    queue = queue
      .then(() => handleMessage(text, msg, say, client))
      .catch((err) => {
        printError(`Slack handler error: ${err instanceof Error ? err.message : String(err)}`);
      });
    await queue;
  });

  async function handleMessage(
    text: string,
    msg: IncomingMessage,
    say: (args: { text: string; thread_ts: string }) => Promise<unknown>,
    client: App["client"],
  ): Promise<void> {
    try {
      await client.reactions.add({ channel: msg.channel, timestamp: msg.ts, name: "eyes" });
    } catch {
      // Non-fatal -- reactions can fail (e.g. missing scope) without blocking the request.
    }

    if (RESET_COMMANDS.has(text.toLowerCase())) {
      currentSessionId = null;
      await say({
        text: "Started a fresh conversation. What project are we working on?",
        thread_ts: msg.ts,
      });
      return;
    }

    try {
      const result = await runClaudeWithRetry(text);
      if (result.session_id) currentSessionId = result.session_id;

      const sessionLabel = result.session_id ? result.session_id.slice(0, 8) : "?";
      const logText = text.replace(/\s+/g, " ").slice(0, 60);
      printInfo(
        `  [session ${sessionLabel}] ${logText} -- ${result.subtype ?? "ok"}` +
          (result.total_cost_usd ? ` (~$${result.total_cost_usd.toFixed(3)} plan usage)` : ""),
      );

      let reply = result.result || (result.is_error ? `Failed: ${result.subtype ?? "unknown error"}` : "(done)");
      if (result.permission_denials && result.permission_denials.length > 0) {
        reply += `\n_(${result.permission_denials.length} action(s) were blocked by tool permissions)_`;
      }
      await say({ text: reply, thread_ts: msg.ts });
    } catch (err) {
      const errMsg = err instanceof Error ? err.message : String(err);
      printError(`Chat error: ${errMsg}`);
      await say({ text: `Something went wrong: ${errMsg}`, thread_ts: msg.ts });
    }
  }

  const shutdown = async (): Promise<void> => {
    printInfo("  Shutting down Slack bot...");
    await app.stop();
    process.exit(0);
  };

  process.on("SIGINT", () => void shutdown());
  process.on("SIGTERM", () => void shutdown());

  await app.start();
  printSuccess(`  Slack bot connected. Listening in channel ${channelId}.`);
}

main().catch((err) => {
  printError(`Fatal: ${err instanceof Error ? err.message : String(err)}`);
  process.exit(1);
});
