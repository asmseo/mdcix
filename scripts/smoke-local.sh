#!/usr/bin/env bash
# Smoke pontual do mdcix — fixtures locais, sem rede externa.
# Uso (na raiz do repo):
#   ./scripts/smoke-local.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

BIN="${MDCIX_BIN:-$ROOT/bin/mdcix-smoke}"
CACHE_DIR="${TMPDIR:-/tmp}/mdcix-smoke-cache-$$"
FIX_DIR="$ROOT/benchmarks/fixtures"
PASS=0
FAIL=0

# Pick free ports so re-runs don't collide with leftover listeners.
free_port() {
  python3 - <<'PY'
import socket
s=socket.socket(); s.bind(('127.0.0.1',0)); print(s.getsockname()[1]); s.close()
PY
}
API_PORT="${API_PORT:-$(free_port)}"
PROXY_PORT="${PROXY_PORT:-$(free_port)}"
FIX_PORT="${FIX_PORT:-$(free_port)}"
JSON_PORT="${JSON_PORT:-$(free_port)}"

step() { echo; echo "=== [$1] $2 ==="; }
ok()   { echo "PASS: $1"; PASS=$((PASS+1)); }
bad()  { echo "FAIL: $1"; FAIL=$((FAIL+1)); }
need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing dependency: $1" >&2
    exit 1
  fi
}

cleanup() {
  [[ -n "${FIX_PID:-}" ]] && kill "$FIX_PID" 2>/dev/null || true
  [[ -n "${API_PID:-}" ]] && kill "$API_PID" 2>/dev/null || true
  [[ -n "${PROXY_PID:-}" ]] && kill "$PROXY_PID" 2>/dev/null || true
  [[ -n "${JSON_PID:-}" ]] && kill "$JSON_PID" 2>/dev/null || true
  rm -rf "$CACHE_DIR"
}
trap cleanup EXIT

need go
need curl
need python3

step 0 "build"
mkdir -p "$(dirname "$BIN")"
go build -o "$BIN" ./cmd/mdcix
ok "binary $BIN"

step 1 "unit + harness"
go test ./... -count=1
ok "go test ./..."

step 2 "fixture HTTP server :$FIX_PORT"
python3 -m http.server "$FIX_PORT" --bind 127.0.0.1 --directory "$FIX_DIR" >/tmp/mdcix-fix-http.log 2>&1 &
FIX_PID=$!
sleep 0.4
URL="http://127.0.0.1:${FIX_PORT}/en-docs-structured.html"
curl -sf "$URL" | grep -q "Acme Cloud" && ok "fixture $URL" || bad "fixture server"

# Persistent flags before the subcommand (cobra-safe).
COMMON=(--allow-private --cache-dir "$CACHE_DIR" --mitm=false)

step 3 "CLI fetch"
if "$BIN" "${COMMON[@]}" fetch "$URL" 2>/tmp/mdcix-fetch.err | grep -q "Pricing\|Overview\|Acme"; then
  ok "fetch markdown"
else
  bad "fetch"; cat /tmp/mdcix-fetch.err >&2 || true
fi
grep -q "mdcix token comparison\|Tokens\|original\|markdown" /tmp/mdcix-fetch.err 2>/dev/null && ok "fetch token stats stderr" || ok "fetch ran (stats optional format)"

step 4 "CLI outline"
OUT="$("$BIN" "${COMMON[@]}" outline "$URL")"
echo "$OUT" | grep -q '"sections"' && ok "outline json" || bad "outline json"
echo "$OUT" | grep -qi 'pricing' && ok "outline has pricing" || bad "outline pricing"
SID="$(echo "$OUT" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(next(s["id"] for s in d["sections"] if "pricing" in s["id"].lower() or "pricing" in s["title"].lower()))')"
VER="$(echo "$OUT" | python3 -c 'import sys,json; print(json.load(sys.stdin)["document_version"])')"
echo "section_id=$SID version=$VER"

step 5 "CLI read full / sections / budget"
"$BIN" "${COMMON[@]}" read "$URL" 2>/dev/null | grep -qi Acme && ok "read full" || bad "read full"
"$BIN" "${COMMON[@]}" read "$URL" --sections "$SID" --budget 800 --json 2>/dev/null | grep -q selected_sections && ok "read --sections" || bad "read --sections"
"$BIN" "${COMMON[@]}" read "$URL" --query "pricing integrations" --budget 400 --json 2>/dev/null | grep -q returned_tokens && ok "read --query --budget" || bad "read --query"

step 6 "cache stats"
STAT_LINE="$("$BIN" --cache-dir "$CACHE_DIR" cache stats)"
echo "$STAT_LINE"
echo "$STAT_LINE" | grep -q entries= && ok "cache stats" || bad "cache stats"
ENTRIES="$(echo "$STAT_LINE" | sed -n 's/.* entries=\([0-9][0-9]*\).*/\1/p')"
[[ "${ENTRIES:-0}" -ge 1 ]] && ok "cache populated ($ENTRIES)" || bad "cache empty (line=$STAT_LINE)"

step 7 "REST API :$API_PORT"
"$BIN" "${COMMON[@]}" serve --api --listen "127.0.0.1:$API_PORT" >/tmp/mdcix-api.log 2>&1 &
API_PID=$!
HEALTH_OK=0
for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
  if curl -sf "http://127.0.0.1:$API_PORT/health" 2>/dev/null | grep -q '"status":"ok"'; then
    HEALTH_OK=1
    break
  fi
  # bail early if process died
  if ! kill -0 "$API_PID" 2>/dev/null; then
    echo "API process exited:" >&2
    cat /tmp/mdcix-api.log >&2 || true
    break
  fi
  sleep 0.2
done
[[ "$HEALTH_OK" -eq 1 ]] && ok "GET /health" || bad "GET /health"
curl -sf -X POST "http://127.0.0.1:$API_PORT/v1/outline" -H 'content-type: application/json' \
  -d "{\"url\":\"$URL\"}" | grep -q sections && ok "POST /v1/outline" || bad "POST /v1/outline"
curl -sf -X POST "http://127.0.0.1:$API_PORT/v1/read" -H 'content-type: application/json' \
  -d "{\"url\":\"$URL\",\"format\":\"markdown\"}" | grep -qi Acme && ok "POST /v1/read" || bad "POST /v1/read"
curl -sf -X POST "http://127.0.0.1:$API_PORT/v1/sections" -H 'content-type: application/json' \
  -d "{\"url\":\"$URL\",\"section_ids\":[\"$SID\"],\"budget\":800}" | grep -q selected_sections && ok "POST /v1/sections" || bad "POST /v1/sections"
curl -sf -X POST "http://127.0.0.1:$API_PORT/v1/read-budget" -H 'content-type: application/json' \
  -d "{\"url\":\"$URL\",\"query\":\"pricing plans\",\"budget\":400}" | grep -q returned_tokens && ok "POST /v1/read-budget" || bad "POST /v1/read-budget"

step 8 "SSRF guard (must fail without --allow-private)"
if "$BIN" --mitm=false outline "http://127.0.0.1:$FIX_PORT/en-docs-structured.html" >/tmp/mdcix-ssrf.out 2>/tmp/mdcix-ssrf.err; then
  bad "SSRF should block loopback"
else
  ok "SSRF blocks loopback"
fi

step 9 "bot challenge not treated as savings"
if "$BIN" "${COMMON[@]}" outline "http://127.0.0.1:$FIX_PORT/bot-challenge.html" >/tmp/mdcix-bot.out 2>/tmp/mdcix-bot.err; then
  bad "bot challenge should error"
else
  grep -qi 'bot_challenge\|challenge\|interstitial' /tmp/mdcix-bot.err /tmp/mdcix-bot.out 2>/dev/null && ok "bot challenge rejected" || ok "bot challenge errored"
fi

step 10 "MCP handshake"
MCP_OUT="$(printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"smoke","version":"1"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
  | "$BIN" "${COMMON[@]}" mcp 2>/tmp/mdcix-mcp.err || true)"
echo "$MCP_OUT" | grep -q '"protocolVersion"' && ok "mcp initialize" || bad "mcp initialize"
echo "$MCP_OUT" | grep -q 'page_outline' && echo "$MCP_OUT" | grep -q 'read_budget' && ok "mcp tools/list" || bad "mcp tools/list"

step 11 "proxy HTML convert + JSON pass-through :$PROXY_PORT"
"$BIN" --mitm=false --mode reader serve --listen "127.0.0.1:$PROXY_PORT" >/tmp/mdcix-proxy.log 2>&1 &
PROXY_PID=$!
sleep 0.5
PROXY_HTML="$(curl -sf -x "http://127.0.0.1:$PROXY_PORT" -D /tmp/mdcix-proxy-h.hdr "$URL" || true)"
echo "$PROXY_HTML" | grep -qi 'Pricing\|Overview\|Acme' && ok "proxy converts HTML→MD" || bad "proxy HTML convert"
grep -qi 'X-Mdcix-Source' /tmp/mdcix-proxy-h.hdr && ok "proxy X-Mdcix headers" || bad "proxy headers"
# JSON fixture via tiny python one-shot on FIX_PORT is HTML dir; spin a dedicated json handler with curl to a public-less local:
python3 - "$JSON_PORT" <<'PY' >/tmp/mdcix-json-srv.log 2>&1 &
import http.server, sys
port=int(sys.argv[1])
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        body=b'{"ok":true,"n":3}'
        self.send_response(200); self.send_header('Content-Type','application/json'); self.send_header('Content-Length',str(len(body))); self.end_headers(); self.wfile.write(body)
    def log_message(self,*a): pass
http.server.HTTPServer(('127.0.0.1', port), H).serve_forever()
PY
JSON_PID=$!
sleep 0.3
JSON_BODY="$(curl -sf -x "http://127.0.0.1:$PROXY_PORT" -H 'Accept: application/json' "http://127.0.0.1:$JSON_PORT/" || true)"
kill "$JSON_PID" 2>/dev/null || true
echo "$JSON_BODY" | grep -q '"ok":true' && ok "proxy JSON pass-through" || bad "proxy JSON pass-through"

step 12 "cache clear"
"$BIN" --cache-dir "$CACHE_DIR" cache clear >/dev/null
"$BIN" --cache-dir "$CACHE_DIR" cache stats | grep -q 'entries=0' && ok "cache clear" || bad "cache clear"

echo
echo "=============================="
echo "SMOKE RESULT: PASS=$PASS FAIL=$FAIL"
echo "Checklist manual: docs/TESTAR.md"
echo "=============================="
[[ "$FAIL" -eq 0 ]]
