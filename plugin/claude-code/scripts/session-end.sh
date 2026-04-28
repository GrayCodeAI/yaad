#!/usr/bin/env bash
# Yaad session-end hook for Claude Code
YAAD_PORT=${YAAD_PORT:-3456}
BASE="http://127.0.0.1:${YAAD_PORT}"

# Read summary from stdin (Claude Code passes session summary)
SUMMARY=$(cat)

if [ -n "$SUMMARY" ]; then
  curl -sf -X POST "${BASE}/yaad/remember" \
    -H "Content-Type: application/json" \
    -d "{\"type\":\"session\",\"content\":$(echo "$SUMMARY" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))'),\"scope\":\"project\",\"agent\":\"claude-code\"}" \
    > /dev/null 2>&1
fi
