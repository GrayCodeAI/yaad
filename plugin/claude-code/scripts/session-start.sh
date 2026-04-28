#!/usr/bin/env bash
# Yaad session-start hook for Claude Code
# Ensures server is running, creates session, injects context

YAAD_PORT=${YAAD_PORT:-3456}
BASE="http://127.0.0.1:${YAAD_PORT}"
PROJECT="${CLAUDE_PROJECT_DIR:-$(pwd)}"

# Start server if not running
if ! curl -sf "${BASE}/yaad/health" > /dev/null 2>&1; then
  yaad serve --addr ":${YAAD_PORT}" &>/dev/null &
  sleep 0.5
fi

# Import any pending sync chunks
if [ -f "${PROJECT}/.yaad/manifest.json" ]; then
  yaad sync --import 2>/dev/null
fi

# Get context and print to stdout (Claude Code injects this into session)
curl -sf "${BASE}/yaad/context?project=$(python3 -c 'import urllib.parse,sys; print(urllib.parse.quote(sys.argv[1]))' "${PROJECT}")" \
  | python3 -c "
import json,sys
data = json.load(sys.stdin)
nodes = data.get('nodes', [])
if not nodes: sys.exit(0)
print('## Project Memory (Yaad)\n')
by_type = {}
for n in nodes:
    by_type.setdefault(n['type'], []).append(n['content'])
for t, h in [('convention','### Conventions'),('task','### Active Tasks'),('decision','### Recent Decisions'),('bug','### Known Bug Patterns')]:
    items = by_type.get(t, [])
    if items:
        print(h)
        for i in items: print(f'- {i}')
        print()
" 2>/dev/null
