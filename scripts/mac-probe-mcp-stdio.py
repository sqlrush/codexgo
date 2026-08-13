#!/usr/bin/env python3
"""Speak MCP stdio directly to a server binary: initialize + tools/list.

Ground truth for "did this server really start and what does it expose",
independent of any model's account of it.

Usage: mac-probe-mcp-stdio.py /path/to/server [protocol-version]
"""

import json
import subprocess
import sys

binary = sys.argv[1]
version = sys.argv[2] if len(sys.argv) > 2 else "2025-06-18"

requests = [
    {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "initialize",
        "params": {
            "protocolVersion": version,
            "capabilities": {},
            "clientInfo": {"name": "stdio-probe", "version": "0"},
        },
    },
    {"jsonrpc": "2.0", "method": "notifications/initialized"},
    {"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {}},
]
stdin = "".join(json.dumps(r) + "\n" for r in requests)

proc = subprocess.run(
    [binary], input=stdin, capture_output=True, text=True, timeout=30
)

tools = []
for line in proc.stdout.splitlines():
    try:
        msg = json.loads(line)
    except json.JSONDecodeError:
        continue
    result = msg.get("result", {})
    if msg.get("id") == 1:
        print(f"server protocolVersion: {result.get('protocolVersion')!r}")
        print(f"serverInfo:             {result.get('serverInfo')}")
    if msg.get("id") == 2:
        tools = [t["name"] for t in result.get("tools", [])]

print(f"tools/list count:       {len(tools)}")
print("tools:                  " + ", ".join(sorted(tools)))
if proc.stderr.strip():
    print("--- stderr ---\n" + proc.stderr.strip()[:500])
