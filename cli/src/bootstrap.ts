/**
 * Shared startup sequence for any PremierPro entry point (interactive CLI,
 * Slack bot, etc.): resolve auth, spawn/connect the MCP server, list its
 * tools, and make sure Premiere Pro itself is running.
 */

import { MCPClient } from "./mcp-client.js";
import { resolveAuth, printAuthHelp, printOAuthHelp } from "./auth.js";
import type { AuthResult } from "./auth.js";
import { printError, printInfo, printSuccess, color } from "./ui.js";

export interface BootstrappedSession {
  mcpClient: MCPClient;
  auth: AuthResult;
  toolCount: number;
}

/**
 * Runs the full startup sequence and exits the process on unrecoverable
 * failure (no auth, MCP server won't start, tools can't be listed) — mirrors
 * the behavior the interactive CLI has always had.
 */
export async function bootstrapSession(): Promise<BootstrappedSession> {
  // 1. Resolve authentication (Anthropic or OpenAI)
  const authResult = await resolveAuth();

  if (!authResult) {
    printError("No authentication found.");
    printAuthHelp(color);
    console.log(
      `  ${color.dim}For setup instructions, visit:${color.reset}`,
    );
    console.log(
      `  ${color.cyan}https://github.com/ayushozha/AdobePremiereProMCP#setup${color.reset}`,
    );
    console.log();
    process.exit(1);
  }

  // Handle OAuth detection (logged in via claude.ai but no API key)
  if ("kind" in authResult && authResult.kind === "oauth-no-key") {
    printError("Claude OAuth session detected, but no API key available.");
    printOAuthHelp(color, authResult.email);
    process.exit(1);
  }

  const auth = authResult as AuthResult;

  const providerName = auth.provider === "anthropic" ? "Anthropic (Claude)" : "OpenAI";
  const authSource = getAuthSource(auth);
  printSuccess(`  Authenticated via ${authSource}`);
  printInfo(`  Provider: ${providerName}  |  Model: ${auth.model}`);

  // 2. Spawn and connect to the MCP server
  const mcpClient = new MCPClient();

  printInfo("  Connecting to PremierPro MCP server...");

  try {
    await mcpClient.connect();
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    printError(`Failed to start MCP server: ${msg}`);
    console.log();
    console.log(`  ${color.yellow}To fix this:${color.reset}`);
    console.log();
    console.log("  1. Make sure the server binary exists:");
    console.log(
      `     ${color.cyan}go-orchestrator/bin/premierpro-mcp${color.reset}`,
    );
    console.log();
    console.log("  2. Build it with:");
    console.log(
      `     ${color.cyan}cd go-orchestrator && go build -o bin/premierpro-mcp ./cmd/server${color.reset}`,
    );
    console.log();
    console.log("  3. Or install Go if you haven't:");
    console.log(
      `     ${color.cyan}brew install go${color.reset}  (macOS)`,
    );
    console.log();
    process.exit(1);
  }

  // 3. Fetch available tools
  let toolCount: number;
  try {
    const tools = await mcpClient.listTools();
    toolCount = tools.length;
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    printError(`Failed to list MCP tools: ${msg}`);
    printInfo("  The MCP server started but could not enumerate tools.");
    printInfo("  Check the server logs for more details.");
    await mcpClient.disconnect();
    process.exit(1);
  }

  printSuccess(`  Connected. ${toolCount.toLocaleString()} tools available.`);

  // 4. Check Premiere Pro status
  try {
    const statusResult = await mcpClient.callTool("premiere_is_running", {});
    const isRunning =
      !statusResult.isError &&
      statusResult.content.toLowerCase().includes("true");

    if (!isRunning) {
      printInfo("  Premiere Pro is not running. Launching...");
      const launchResult = await mcpClient.callTool("premiere_open", {});
      if (launchResult.isError) {
        printError(`Failed to launch Premiere Pro: ${launchResult.content}`);
        printInfo("  You can launch it manually and the CLI will still work.");
      } else {
        printSuccess("  Premiere Pro launched.");
      }
    } else {
      printSuccess("  Premiere Pro is running.");
    }
  } catch (err) {
    // Non-fatal: tools may not include premiere_is_running if the server
    // is a different version. Continue regardless.
    const msg = err instanceof Error ? err.message : String(err);
    printInfo(`  (Could not check Premiere Pro status: ${msg})`);
    printInfo("  Continuing anyway -- Premiere Pro commands will work once it is running.");
  }

  return { mcpClient, auth, toolCount };
}

/** Infer how the user authenticated for display purposes. */
export function getAuthSource(auth: AuthResult): string {
  if (process.env.ANTHROPIC_API_KEY) return "ANTHROPIC_API_KEY env var";
  if (process.env.OPENAI_API_KEY) return "OPENAI_API_KEY env var";
  if (auth.provider === "anthropic") return "Claude CLI (claude login)";
  if (auth.provider === "openai") return "Codex CLI or config file";
  return "config file";
}
