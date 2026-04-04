#!/usr/bin/env bash
# FaultWall status — quick health check for proxy, monitor, and recent activity.

set -uo pipefail

FAULTWALL_DIR="${HOME}/.faultwall"
PROXY_PORT=5433
MONITOR_PORT=8080
MONITOR_URL="http://localhost:${MONITOR_PORT}"

# ── colours ───────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'

ok()   { echo -e "  ${GREEN}✓${NC} $*"; }
fail() { echo -e "  ${RED}✗${NC} $*"; }
info() { echo -e "  ${CYAN}→${NC} $*"; }
hdr()  { echo -e "\n${BOLD}$*${NC}"; }

port_in_use() { nc -z 127.0.0.1 "$1" 2>/dev/null; }

# ── proxy status ──────────────────────────────────────────────────────────────
hdr "── FaultWall Status ──────────────────────────────────"

hdr "Proxy"
if port_in_use "${PROXY_PORT}"; then
  ok "Listening on port ${PROXY_PORT}"

  # Try to find PID
  if [[ -f "${FAULTWALL_DIR}/proxy.pid" ]]; then
    PID=$(cat "${FAULTWALL_DIR}/proxy.pid")
    if kill -0 "${PID}" 2>/dev/null; then
      info "PID: ${PID}"
    else
      fail "PID file exists (${PID}) but process is gone — port may be held by another process"
    fi
  else
    FOUND_PID=$(lsof -ti :"${PROXY_PORT}" 2>/dev/null | head -1 || true)
    [[ -n "${FOUND_PID}" ]] && info "PID: ${FOUND_PID} (from lsof)"
  fi
else
  fail "Not running on port ${PROXY_PORT}"
  echo -e "  ${YELLOW}Run: bash skill/scripts/setup.sh${NC}"
fi

# ── monitor status ────────────────────────────────────────────────────────────
hdr "Monitor / Dashboard"
if port_in_use "${MONITOR_PORT}"; then
  ok "Dashboard available at ${MONITOR_URL}"
else
  fail "Monitor not running on port ${MONITOR_PORT}"
fi

# ── API queries ───────────────────────────────────────────────────────────────
if ! port_in_use "${MONITOR_PORT}"; then
  echo -e "\n${YELLOW}Monitor is offline — skipping API checks.${NC}"
  exit 0
fi

curl_json() {
  local endpoint=$1
  curl -sf --max-time 5 "${MONITOR_URL}${endpoint}" 2>/dev/null || echo "null"
}

# Active agents
hdr "Active Agents  (GET /api/firewall/agents)"
AGENTS_JSON=$(curl_json "/api/firewall/agents")
if [[ "${AGENTS_JSON}" == "null" || -z "${AGENTS_JSON}" ]]; then
  fail "Could not reach /api/firewall/agents"
else
  echo "${AGENTS_JSON}" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    agents = data if isinstance(data, list) else data.get('agents', data.get('data', []))
    if not agents:
        print('  (no active agents)')
    else:
        for a in agents:
            aid   = a.get('agent_id') or a.get('id', '?')
            miss  = a.get('mission', '-')
            conns = a.get('connections', a.get('active_connections', '?'))
            print(f'  • {aid}  mission={miss}  connections={conns}')
except Exception as e:
    print(f'  (could not parse response: {e})')
    print('  Raw:', sys.stdin.read()[:300])
" 2>/dev/null || echo "${AGENTS_JSON}" | head -40
fi

# Violations
hdr "Recent Violations  (GET /api/violations)"
VIOLATIONS_JSON=$(curl_json "/api/violations")
if [[ "${VIOLATIONS_JSON}" == "null" || -z "${VIOLATIONS_JSON}" ]]; then
  fail "Could not reach /api/violations"
else
  echo "${VIOLATIONS_JSON}" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    viols = data if isinstance(data, list) else data.get('violations', data.get('data', []))
    total = len(viols)
    print(f'  Total violations logged: {total}')
    recent = viols[-5:] if total > 5 else viols
    if recent:
        print('  Last 5:')
        for v in reversed(recent):
            ts    = v.get('timestamp', v.get('time', '?'))
            agent = v.get('agent_id', v.get('agent', '?'))
            query = v.get('query', v.get('sql', '?'))[:80]
            reason= v.get('reason', v.get('message', '-'))
            print(f'    [{ts}] {agent}: {query!r}')
            print(f'           reason: {reason}')
    else:
        print('  (no violations recorded)')
except Exception as e:
    print(f'  (could not parse response: {e})')
" 2>/dev/null || echo "${VIOLATIONS_JSON}" | head -40
fi

# Policies summary
hdr "Policies  (GET /api/policies)"
POLICIES_JSON=$(curl_json "/api/policies")
if [[ "${POLICIES_JSON}" == "null" || -z "${POLICIES_JSON}" ]]; then
  fail "Could not reach /api/policies"
else
  echo "${POLICIES_JSON}" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    default = data.get('default_policy', '?')
    agents  = data.get('agents', [])
    print(f'  default_policy: {default}')
    print(f'  configured agents: {len(agents)}')
    for a in agents:
        aid = a.get('id', '?')
        desc = a.get('description', '')
        print(f'    • {aid}' + (f' — {desc}' if desc else ''))
except Exception as e:
    print(f'  (could not parse response: {e})')
" 2>/dev/null || echo "${POLICIES_JSON}" | head -40
fi

# ── footer ────────────────────────────────────────────────────────────────────
hdr "──────────────────────────────────────────────────────"
echo -e "  Dashboard : ${MONITOR_URL}"
echo -e "  Reload    : curl -X POST ${MONITOR_URL}/api/policies/reload"
echo -e "  Proxy logs: ${FAULTWALL_DIR}/logs/proxy.log"
echo ""
