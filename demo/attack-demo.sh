#!/usr/bin/env bash
set -euo pipefail

# ── FaultWall × OpenClaw Demo — 5 Attack Scenarios ──────────────────────────

PROXY_HOST="${PROXY_HOST:-localhost}"
PROXY_PORT="${PROXY_PORT:-5433}"
PG_USER="${PG_USER:-ghost}"
PG_PASS="${PG_PASS:-ghostpass}"
PG_DB="${PG_DB:-faultwall_demo}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
BOLD='\033[1m'
RESET='\033[0m'

PASS=0
FAIL=0

banner() {
    echo ""
    echo -e "${CYAN}${BOLD}╔══════════════════════════════════════════════════════════╗${RESET}"
    echo -e "${CYAN}${BOLD}║   🛡️  FaultWall × OpenClaw — Agent Security Demo         ║${RESET}"
    echo -e "${CYAN}${BOLD}║   Demonstrating the confused-deputy firewall in action   ║${RESET}"
    echo -e "${CYAN}${BOLD}╚══════════════════════════════════════════════════════════╝${RESET}"
    echo ""
    echo -e "  Proxy:    ${MAGENTA}${PROXY_HOST}:${PROXY_PORT}${RESET}"
    echo -e "  Database: ${MAGENTA}${PG_DB}${RESET}"
    echo ""
}

wait_for_proxy() {
    echo -e "${YELLOW}⏳ Waiting for FaultWall proxy at ${PROXY_HOST}:${PROXY_PORT}...${RESET}"
    for i in $(seq 1 30); do
        if PGPASSWORD="$PG_PASS" \
            PGAPPNAME="agent:openclaw-coder:mission:read-feedback:token:oc-coder-secret-abc" \
            psql -h "$PROXY_HOST" -p "$PROXY_PORT" -U "$PG_USER" -d "$PG_DB" \
            -o /dev/null -c "SELECT 1" 2>/dev/null; then
            echo -e "${GREEN}✅ Proxy is ready!${RESET}"
            echo ""
            return 0
        fi
        sleep 1
    done
    echo -e "${RED}❌ Proxy not ready after 30s. Is docker-compose up?${RESET}"
    echo -e "${YELLOW}   Hint: docker-compose up -d && sleep 10 && ./demo/attack-demo.sh${RESET}"
    exit 1
}

run_query() {
    local app_name="$1"
    local query="$2"
    # CRITICAL: application_name must be set via PGAPPNAME env var (or connection
    # string), NOT psql --set. The --set flag sets a psql client variable which
    # is never sent to the server, so FaultWall sees application_name=psql and
    # identity parsing silently fails. Documented in audit 2026-05-03.
    # -o "-c client_min_messages=warning" suppresses the cosmetic truncation
    # NOTICE Postgres emits for application_name > 63 chars (harmless — FaultWall
    # reads the raw startup packet before PG truncates).
    PGPASSWORD="$PG_PASS" \
        PGAPPNAME="$app_name" \
        psql -h "$PROXY_HOST" -p "$PROXY_PORT" -U "$PG_USER" -d "$PG_DB" \
        -o "-c client_min_messages=warning" \
        -c "$query" 2>&1 || true
}

test_case() {
    local num="$1"
    local icon="$2"
    local label="$3"
    local app_name="$4"
    local query="$5"
    local expect_blocked="$6"

    echo -e "${BOLD}── Test ${num}: ${icon} ${label} ──${RESET}"
    echo -e "   Agent:    ${CYAN}${app_name}${RESET}"
    echo -e "   Query:    ${CYAN}${query}${RESET}"

    result=$(run_query "$app_name" "$query")

    if echo "$result" | grep -qi "BLOCKED by FaultWall\|FATAL.*BLOCKED\|server closed the connection\|connection.*refused\|permission denied"; then
        actual="BLOCKED"
    else
        actual="ALLOWED"
    fi

    if [ "$expect_blocked" = "BLOCKED" ]; then
        expected_icon="🚫"
        expected_label="BLOCKED"
    else
        expected_icon="✅"
        expected_label="ALLOWED"
    fi

    echo -e "   Expected: ${expected_icon} ${expected_label}"

    if [ "$actual" = "$expect_blocked" ]; then
        if [ "$actual" = "BLOCKED" ]; then
            echo -e "   Actual:   ${RED}🚫 BLOCKED${RESET}  ${GREEN}✓ PASS${RESET}"
        else
            echo -e "   Actual:   ${GREEN}✅ ALLOWED${RESET}  ${GREEN}✓ PASS${RESET}"
        fi
        PASS=$((PASS + 1))
    else
        if [ "$actual" = "BLOCKED" ]; then
            echo -e "   Actual:   ${RED}🚫 BLOCKED${RESET}  ${RED}✗ FAIL (expected ALLOWED)${RESET}"
        else
            echo -e "   Actual:   ${GREEN}✅ ALLOWED${RESET}  ${RED}✗ FAIL (expected BLOCKED)${RESET}"
        fi
        FAIL=$((FAIL + 1))
    fi
    echo ""
}

summary() {
    echo -e "${BOLD}══════════════════════════════════════════════════════════${RESET}"
    if [ "$FAIL" -eq 0 ]; then
        echo -e "${BOLD}  ${GREEN}🎉 All ${PASS} tests passed — FaultWall is protecting your DB!${RESET}"
    else
        echo -e "${BOLD}  Summary: ${GREEN}${PASS} passed${RESET}, ${RED}${FAIL} failed${RESET} out of 5 tests${RESET}"
    fi
    echo -e "${BOLD}══════════════════════════════════════════════════════════${RESET}"
    echo ""
    if [ "$FAIL" -gt 0 ]; then
        exit 1
    fi
}

# ── Main ──────────────────────────────────────────────────────────────────────

banner
wait_for_proxy

# Test 1: ✅ ALLOWED — openclaw-coder reads feedback (legitimate)
test_case 1 "✅" "ALLOWED — openclaw-coder reads feedback" \
    "agent:openclaw-coder:mission:read-feedback:token:oc-coder-secret-abc" \
    "SELECT id, agent_id, rating, category FROM feedback LIMIT 5" \
    "ALLOWED"

# Test 2: 🚫 BLOCKED — openclaw-coder attempts DROP TABLE (destructive DDL)
test_case 2 "🚫" "BLOCKED — openclaw-coder tries DROP TABLE" \
    "agent:openclaw-coder:mission:read-feedback:token:oc-coder-secret-abc" \
    "DROP TABLE feedback" \
    "BLOCKED"

# Test 3: 🚫 BLOCKED — rogue-agent has no policy entry (default: deny)
test_case 3 "🚫" "BLOCKED — rogue-agent not in policy (default: deny)" \
    "agent:rogue-agent:mission:steal-data" \
    "SELECT * FROM users" \
    "BLOCKED"

# Test 4: 🚫 BLOCKED — openclaw-coder tries to access payments (blocked table)
test_case 4 "🚫" "BLOCKED — openclaw-coder accesses payments table" \
    "agent:openclaw-coder:mission:read-feedback:token:oc-coder-secret-abc" \
    "SELECT * FROM payments LIMIT 10" \
    "BLOCKED"

# Test 5: ✅ ALLOWED — openclaw-admin reads orders (permitted by policy)
test_case 5 "✅" "ALLOWED — openclaw-admin reads orders" \
    "agent:openclaw-admin:mission:manage-orders:token:oc-admin-secret-ghi" \
    "SELECT order_ref, product_sku, status, total_cents FROM orders LIMIT 5" \
    "ALLOWED"

summary
