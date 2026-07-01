#!/usr/bin/env node

/**
 * PremierPro AI Editor CLI
 *
 * An interactive, AI-powered command-line interface for controlling
 * Adobe Premiere Pro. Supports both Claude (Anthropic) and GPT/Codex (OpenAI).
 */

import "./load-env.js";
import { ChatLoop } from "./chat.js";
import { bootstrapSession } from "./bootstrap.js";
import {
  banner,
  printAssistant,
  printError,
  printInfo,
  createReadlineInterface,
  prompt,
} from "./ui.js";

// ── Main ──────────────────────────────────────────────────────────────

async function main(): Promise<void> {
  // Show banner without tool count (we don't know yet)
  banner();

  const { mcpClient, auth } = await bootstrapSession();

  console.log();

  // 5. Set up the chat loop with the resolved auth
  const chatLoop = new ChatLoop(mcpClient, auth);
  const rl = createReadlineInterface();

  // 6. Handle graceful shutdown
  const shutdown = async (): Promise<void> => {
    console.log();
    printInfo("  Shutting down...");
    rl.close();
    await mcpClient.disconnect();
    process.exit(0);
  };

  process.on("SIGINT", () => void shutdown());
  process.on("SIGTERM", () => void shutdown());

  // 7. Interactive loop
  while (true) {
    const input = await prompt(rl);

    if (input === null) {
      await shutdown();
      break;
    }

    const trimmed = input.trim();
    if (trimmed === "") continue;

    if (["exit", "quit", "q"].includes(trimmed.toLowerCase())) {
      await shutdown();
      break;
    }

    try {
      const response = await chatLoop.processUserMessage(trimmed);
      if (response) {
        printAssistant(response);
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      printError(`Chat error: ${msg}`);
      if (msg.toLowerCase().includes("api key") || msg.toLowerCase().includes("unauthorized") || msg.toLowerCase().includes("401")) {
        printInfo("  Your API key may be invalid or expired. Check your authentication.");
      } else if (msg.toLowerCase().includes("rate limit") || msg.toLowerCase().includes("429")) {
        printInfo("  You've hit a rate limit. Wait a moment and try again.");
      }
    }
  }
}

// ── Entry ─────────────────────────────────────────────────────────────

main().catch((err) => {
  printError(`Fatal: ${err instanceof Error ? err.message : String(err)}`);
  process.exit(1);
});
