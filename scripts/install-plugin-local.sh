#!/usr/bin/env bash
# install-plugin-local.sh — build a local plugin and install it into the codexgo
# plugin store cache so the runtime auto-discovers its MCP servers.
#
# The GaussDB MCP server now lives in its OWN repo (github.com/sqlrush/
# opendbx-mcp-for-codex); this script builds from there by default. Override with
# an explicit dir argument or the OPENDBX_MCP_DIR env var.
#
# Usage:
#   scripts/install-plugin-local.sh                       # default: ~/opendbx-mcp-for-codex
#   scripts/install-plugin-local.sh /path/to/plugin-dir   # explicit
#
# After running, enable the plugin in $CODEXGO_HOME/config.toml:
#   [plugins."<name>@local"]
#   enabled = true
set -euo pipefail

PLUGIN_DIR="${1:-${OPENDBX_MCP_DIR:-$HOME/opendbx-mcp-for-codex}}"
PLUGIN_DIR="$(cd "$PLUGIN_DIR" && pwd)"

# Resolve plugin name from .codex-plugin/plugin.json (fallback: dir basename).
MANIFEST="$PLUGIN_DIR/.codex-plugin/plugin.json"
[ -f "$MANIFEST" ] || MANIFEST="$PLUGIN_DIR/.codexgo-plugin/plugin.json"
if command -v python3 >/dev/null 2>&1 && [ -f "$MANIFEST" ]; then
  NAME="$(python3 -c "import json,sys;print(json.load(open(sys.argv[1])).get('name',''))" "$MANIFEST")"
fi
NAME="${NAME:-$(basename "$PLUGIN_DIR")}"

CODEXGO_HOME="${CODEXGO_HOME:-$HOME/.codexgo}"
DEST="$CODEXGO_HOME/plugins/cache/local/$NAME/local"

echo "==> building plugin in $PLUGIN_DIR"
make -C "$PLUGIN_DIR" build

echo "==> installing '$NAME' into $DEST"
rm -rf "$DEST"
mkdir -p "$DEST"
# Copy the bundle: manifest dir, .mcp.json, built bin/, and any other assets.
# (Excludes VCS noise and Go sources that the runtime does not need.)
cp -R "$PLUGIN_DIR"/. "$DEST"/
rm -rf "$DEST/.git"

echo
echo "Installed. Enable it in $CODEXGO_HOME/config.toml:"
echo
echo "  [plugins.\"$NAME@local\"]"
echo "  enabled = true"
echo
echo "Then start codexgo; its MCP servers are auto-discovered (no [mcp_servers] entry needed)."
