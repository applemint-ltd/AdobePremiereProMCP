#!/bin/bash
# Runner used by the com.premierpro.slackbot LaunchAgent (see
# install-slack-bot-service.sh). Makes sure the backend services are up,
# then execs the Slack bot so launchd can supervise/restart it directly.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$PROJECT_ROOT"

# .env itself is loaded by cli/src/load-env.ts (via the `dotenv` npm package)
# once the Node process starts -- not here. dotenv correctly parses values
# with unquoted spaces (e.g. PREMIERE_PRO_PATH); bash `source .env` does not.

# launchd starts with a minimal PATH (no nvm/node/claude), so add the known
# install locations directly. Sourcing ~/.zshrc/.zprofile here would run
# zsh-only syntax under this bash script (and fail); borrowing PATH from a
# `zsh -l` subshell isn't reliable either -- nvm.sh only prepends the node
# bin dir onto an *existing* PATH, so in a truly clean environment (like
# launchd's) it can come back without it. Building PATH directly sidesteps
# both problems.
NVM_NODE_BIN="$(ls -d "$HOME"/.nvm/versions/node/*/bin 2>/dev/null | sort -V | tail -1)"
export PATH="$HOME/.local/bin:${NVM_NODE_BIN:+$NVM_NODE_BIN:}/opt/homebrew/bin:/usr/local/bin:$PATH"

if ! lsof -i :50054 &>/dev/null; then
    echo "Backend services not running -- starting them..."
    "$SCRIPT_DIR/start-all.sh" > "$SCRIPT_DIR/logs/start-all.log" 2>&1 &
    sleep 5
fi

exec npx --prefix cli tsx cli/src/slack-bot.ts
