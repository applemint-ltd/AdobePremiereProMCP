#!/bin/bash
# Installs the PremierPro Slack bot as a per-user launchd LaunchAgent so it
# stays running (and restarts on crash/reboot) on this Mac without anyone
# needing to keep a terminal open. Must be a LaunchAgent (not a
# LaunchDaemon) because Premiere Pro needs a logged-in GUI session.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LABEL="com.premierpro.slackbot"
PLIST_PATH="$HOME/Library/LaunchAgents/$LABEL.plist"

mkdir -p "$SCRIPT_DIR/logs"

cat > "$PLIST_PATH" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>$LABEL</string>
    <key>ProgramArguments</key>
    <array>
        <string>/bin/bash</string>
        <string>$SCRIPT_DIR/run-slack-bot.sh</string>
    </array>
    <key>WorkingDirectory</key>
    <string>$PROJECT_ROOT</string>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>$SCRIPT_DIR/logs/slack-bot.log</string>
    <key>StandardErrorPath</key>
    <string>$SCRIPT_DIR/logs/slack-bot.log</string>
</dict>
</plist>
PLIST

launchctl unload "$PLIST_PATH" 2>/dev/null || true
launchctl load "$PLIST_PATH"

echo "Installed and started LaunchAgent: $LABEL"
echo "  Plist:  $PLIST_PATH"
echo "  Logs:   $SCRIPT_DIR/logs/slack-bot.log"
echo ""
echo "Useful commands:"
echo "  tail -f $SCRIPT_DIR/logs/slack-bot.log   # watch logs"
echo "  launchctl unload $PLIST_PATH             # stop and disable"
echo "  launchctl kickstart -k gui/\$(id -u)/$LABEL  # force restart"
echo ""
echo "Security note: do NOT set MCP_EXPOSE_ALL_TOOLS or MCP_ENABLE_ESCAPE_HATCHES"
echo "in this service's environment -- the Slack bot allowlists every registered"
echo "premiere tool, so widening the tool surface widens what Slack users can do."
