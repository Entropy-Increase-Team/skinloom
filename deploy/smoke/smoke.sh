#!/usr/bin/env bash
# Smoke test of a fresh SkinLoom deployment, as a new site owner would use it:
#   bash deploy/smoke/smoke.sh <base URL> <command that prints the server log>
# e.g.
#   bash deploy/smoke/smoke.sh http://127.0.0.1:17546 "docker compose -f deploy/compose/minimal/compose.yaml logs skinloom"
#
# Waits until the site is ready, creates the administrator with the one-time setup
# link from the log, signs in, creates a player and fetches it through the
# CustomSkinLoader API, the way a game client does. Needs curl and jq.
set -euo pipefail

BASE="${1:?base URL}"
LOGS="${2:?command that prints the server log}"
JAR="$(mktemp)"
PAGE="$(mktemp)"
trap 'rm -f "$JAR" "$PAGE"' EXIT

step() { printf '\n== %s\n' "$*"; }
fail() { echo "FAIL: $*" >&2; eval "$LOGS" | tail -n 80 >&2 || true; exit 1; }
api() { # method path [json]
  curl -sS --fail-with-body -b "$JAR" -c "$JAR" -X "$1" -H 'Content-Type: application/json' -H 'X-SkinLoom-Request: 1' \
    ${3:+--data "$3"} "$BASE$2"
}

step "waiting for $BASE/readyz"
for _ in $(seq 1 120); do
  curl -sf "$BASE/readyz" >/dev/null 2>&1 && break
  sleep 2
done
curl -sf "$BASE/readyz" >/dev/null || fail "not ready after 4 minutes"

step "the home page renders (backend and frontend)"
# Into a file: grep -q stops reading at the first match, and curl then fails its next write.
curl -sf "$BASE/" -o "$PAGE" && grep -q '<html' "$PAGE" || fail "no HTML at /"
[ "$(curl -sf "$BASE/api/v1/site/bootstrap" | jq -r .needsSetup)" = "true" ] || fail "a fresh site should need setup"

step "setup with the one-time link from the log"
TOKEN="$(eval "$LOGS" 2>&1 | grep -o 'setup?token=[A-Za-z0-9_-]*' | tail -n 1 | cut -d= -f2 || true)"
[ -n "$TOKEN" ] || fail "no setup link in the log"
api POST /api/v1/setup "{\"token\":\"$TOKEN\",\"email\":\"smoke@example.com\",\"nickname\":\"Smoke\",\"password\":\"smoke-pass-123\"}" >/dev/null ||
  fail "setup refused"

step "sign in, make players free, create one"
api POST /api/v1/auth/login '{"email":"smoke@example.com","password":"smoke-pass-123"}' >/dev/null || fail "login"
api PATCH /api/v1/admin/settings '{"score.per_player":0}' >/dev/null || fail "settings"
api POST /api/v1/me/players '{"name":"SmokeSteve"}' >/dev/null || fail "create player"

step "the game API answers for it"
[ "$(curl -sf "$BASE/SmokeSteve.json" | jq -r .username)" = "SmokeSteve" ] || fail "CustomSkinLoader profile"
[ "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/nobody-here.json")" = "404" ] || fail "unknown player"

step "system status has no errors"
STATUS="$(api GET /api/v1/admin/status)" || fail "status"
echo "$STATUS" | jq -r '"database: \(.database.dialect) \(.database.version), migration \(.database.migration)"'
[ "$(echo "$STATUS" | jq '[.checks[] | select(.level == "error")] | length')" = "0" ] || fail "status reports errors: $STATUS"

echo
echo "smoke test passed: $BASE"
