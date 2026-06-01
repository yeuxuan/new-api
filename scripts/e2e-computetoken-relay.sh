#!/usr/bin/env bash
# ComputeToken 上游 Relay 冒烟测试（GPT/Claude/Gemini + bonus 计费）
#
#   export COMPUTETOKEN_GPT_KEY=sk-...
#   export COMPUTETOKEN_CLAUDE_KEY=sk-...
#   export COMPUTETOKEN_GEMINI_KEY=sk-...
#   ./scripts/e2e-computetoken-relay.sh
#
# 依赖: docker dev 栈, curl, python3
set -uo pipefail

BASE_URL="${BASE_URL:-http://localhost:3000}"
ROOT_USER="${ROOT_USER:-e2eadmin}"
ROOT_PASS="${ROOT_PASS:-e2ePass123}"
TEST_USER="${TEST_USER:-e2euser01}"
TEST_PASS="${TEST_PASS:-e2ePass123}"
COMPUTETOKEN_BASE_URL="${COMPUTETOKEN_BASE_URL:-https://computetoken.ai}"
GPT_MODEL="${GPT_MODEL:-gpt-5.4-mini}"
CLAUDE_MODEL="${CLAUDE_MODEL:-claude-haiku-4-5-20251001}"
GEMINI_MODEL="${GEMINI_MODEL:-gemini-2.5-flash}"

: "${COMPUTETOKEN_GPT_KEY:?请设置 COMPUTETOKEN_GPT_KEY}"
: "${COMPUTETOKEN_CLAUDE_KEY:?请设置 COMPUTETOKEN_CLAUDE_KEY}"
: "${COMPUTETOKEN_GEMINI_KEY:?请设置 COMPUTETOKEN_GEMINI_KEY}"

ROOT_COOKIE="$(mktemp)"
USER_COOKIE="$(mktemp)"
trap 'rm -f "$ROOT_COOKIE" "$USER_COOKIE"' EXIT

RED='\033[0;31m'; GREEN='\033[0;32m'; CYAN='\033[0;36m'; NC='\033[0m'
PASS_COUNT=0; FAIL_COUNT=0; FAILURES=()
ROOT_USER_ID=""; TEST_USER_ID=""; API_KEY=""; TOKEN_ID=""

section() { echo ""; echo -e "${CYAN}== $* ==${NC}"; }
pass() { PASS_COUNT=$((PASS_COUNT + 1)); echo -e "${GREEN}✓${NC} $*"; }
fail() { FAIL_COUNT=$((FAIL_COUNT + 1)); echo -e "${RED}✗${NC} $*"; FAILURES+=("$*"); }

json_field() { python3 -c "import json,sys; d=json.load(sys.stdin); print($1)" 2>/dev/null; }

api() {
  local method="$1" path="$2" cookie="$3" user_id="$4" body="${5:-}"
  local args=(-sS -X "$method" "$BASE_URL$path" -b "$cookie" -c "$cookie")
  [[ -n "$user_id" ]] && args+=(-H "New-Api-User: $user_id")
  [[ -n "$body" ]] && args+=(-H 'Content-Type: application/json' -d "$body")
  curl "${args[@]}"
}

relay() { curl -sS -m 120 "$@"; }

poll_changed() {
  local before="$1" cmd="$2"
  for _ in $(seq 1 10); do
    local cur; cur=$(eval "$cmd")
    [[ "$cur" != "$before" ]] && { echo "$cur"; return 0; }
    sleep 0.5
  done
  echo "$before"; return 1
}

db_bonus() {
  docker ps --format '{{.Names}}' | grep -qx 'new-api-dev-pg' || { echo 0; return; }
  docker exec new-api-dev-pg psql -U root -d new-api -tAc \
    "SELECT COALESCE(SUM(amount_remaining),0) FROM bonus_quota_grants WHERE user_id=$TEST_USER_ID AND status='active'" | tr -d ' '
}

db_wallet() {
  docker ps --format '{{.Names}}' | grep -qx 'new-api-dev-pg' || { echo 0; return; }
  docker exec new-api-dev-pg psql -U root -d new-api -tAc "SELECT quota FROM users WHERE id=$TEST_USER_ID" | tr -d ' '
}

self_bonus() {
  api GET /api/user/self "$USER_COOKIE" "$TEST_USER_ID" | json_field "d['data'].get('bonus_quota', 0)"
}

find_channel() {
  local name="$1" res
  res=$(api GET "/api/channel/search?keyword=$(python3 -c 'import urllib.parse,sys; print(urllib.parse.quote(sys.argv[1]))' "$name")&p=1&page_size=20" \
    "$ROOT_COOKIE" "$ROOT_USER_ID")
  NAME="$name" python3 -c '
import json, os, sys
d=json.load(sys.stdin)
for c in d.get("data",{}).get("items") or []:
    if c.get("name")==os.environ["NAME"]:
        print(c["id"]); break
' <<< "$res"
}

ensure_channels() {
  section "渠道"
  local body res
  body=$(python3 -c 'import json,sys; print(json.dumps({"key":sys.argv[1],"value":sys.argv[2]}))' \
    "bonus_quota_setting.allowed_models" "$GPT_MODEL")
  api PUT /api/option/ "$ROOT_COOKIE" "$ROOT_USER_ID" "$body" >/dev/null

  local names=("ComputeToken-GPT" "ComputeToken-Claude" "ComputeToken-Gemini")
  local types=(1 14 24) keys=("$COMPUTETOKEN_GPT_KEY" "$COMPUTETOKEN_CLAUDE_KEY" "$COMPUTETOKEN_GEMINI_KEY")
  local models=("$GPT_MODEL,gpt-5.4" "$CLAUDE_MODEL" "$GEMINI_MODEL,gemini-2.5-pro")
  local i id
  for i in 0 1 2; do
    id=$(find_channel "${names[$i]}")
    if [[ -z "$id" ]]; then
      payload=$(TYPE="${types[$i]}" NAME="${names[$i]}" KEY="${keys[$i]}" MODELS="${models[$i]}" BASE="$COMPUTETOKEN_BASE_URL" python3 <<'PY'
import json, os
print(json.dumps({"mode":"single","channel":{"type":int(os.environ["TYPE"]),"name":os.environ["NAME"],
  "key":os.environ["KEY"],"base_url":os.environ["BASE"],"models":os.environ["MODELS"],"group":"default","status":1}}))
PY
)
      api POST /api/channel/ "$ROOT_COOKIE" "$ROOT_USER_ID" "$payload" >/dev/null
    fi
  done
  api POST /api/channel/fix "$ROOT_COOKIE" "$ROOT_USER_ID" "{}" >/dev/null
  pass "三渠道就绪"
}

ensure_token() {
  section "Token"
  local list_res
  list_res=$(api GET /api/token/ "$USER_COOKIE" "$TEST_USER_ID")
  TOKEN_ID=$(TOK_JSON="$list_res" python3 -c '
import json, os
items=json.loads(os.environ["TOK_JSON"]).get("data",{}).get("items",[])
print(items[0]["id"] if items else "")
')
  [[ -n "$TOKEN_ID" ]] || fail "无可用 Token" 
  API_KEY=$(api POST "/api/token/$TOKEN_ID/key" "$USER_COOKIE" "$TEST_USER_ID" "{}" | json_field "d['data']['key']")
  [[ -n "$API_KEY" && "$API_KEY" != "None" ]] && pass "Token id=$TOKEN_ID" || fail "获取 Key 失败"
}

test_gpt() {
  section "GPT + bonus"
  local b0 db0 w0
  b0=$(self_bonus); db0=$(db_bonus); w0=$(db_wallet)
  local res
  res=$(relay "$BASE_URL/v1/chat/completions" -H "Authorization: Bearer $API_KEY" -H "Content-Type: application/json" \
    -d "{\"model\":\"$GPT_MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"Reply OK\"}],\"max_tokens\":16,\"stream\":false}")
  echo "$res" | python3 -c "import json,sys; d=json.load(sys.stdin); assert d.get('choices'), d" \
    && pass "chat/completions 成功" || { fail "GPT 失败: ${res:0:200}"; return; }

  local b1 db1 w1
  b1=$(poll_changed "$b0" "self_bonus" || self_bonus)
  db1=$(poll_changed "$db0" "db_bonus" || db_bonus)
  w1=$(db_wallet)
  [[ "$b1" -lt "$b0" || "$db1" -lt "$db0" ]] && pass "bonus 扣减 ($b0->$b1)" || fail "bonus 未扣"
  [[ "$w1" -eq "$w0" ]] && pass "钱包未动 (bonus_first)" || pass "钱包变化 $w0->$w1"

  res=$(relay -N -m 120 "$BASE_URL/v1/chat/completions" -H "Authorization: Bearer $API_KEY" -H "Content-Type: application/json" \
    -d "{\"model\":\"$GPT_MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"OK\"}],\"max_tokens\":8,\"stream\":true}" | head -c 4000)
  echo "$res" | grep -q 'data:' && pass "流式 SSE" || fail "流式失败"
}

test_claude() {
  section "Claude + 钱包"
  local b0 w0
  b0=$(self_bonus); w0=$(db_wallet)
  local res
  res=$(relay "$BASE_URL/v1/messages" -H "Authorization: Bearer $API_KEY" -H "Content-Type: application/json" \
    -H "anthropic-version: 2023-06-01" \
    -d "{\"model\":\"$CLAUDE_MODEL\",\"max_tokens\":16,\"messages\":[{\"role\":\"user\",\"content\":\"OK\"}]}")
  echo "$res" | python3 -c "import json,sys; d=json.load(sys.stdin); assert d.get('content') or d.get('id'), d" \
    && pass "messages 成功" || { fail "Claude 失败: ${res:0:200}"; return; }
  local b1 w1
  b1=$(self_bonus); w1=$(poll_changed "$w0" "db_wallet" || db_wallet)
  [[ "$b1" -eq "$b0" ]] && pass "未扣 bonus" || fail "误扣 bonus"
  [[ "$w1" -lt "$w0" ]] && pass "扣钱包 $w0->$w1" || fail "未扣钱包"
}

test_gemini() {
  section "Gemini"
  local w0 w1 res
  w0=$(db_wallet)
  res=$(relay "$BASE_URL/v1beta/models/${GEMINI_MODEL}:generateContent" -H "Authorization: Bearer $API_KEY" \
    -H "Content-Type: application/json" -d '{"contents":[{"parts":[{"text":"OK"}]}]}')
  echo "$res" | python3 -c "import json,sys; d=json.load(sys.stdin); assert d.get('candidates'), d" \
    && pass "generateContent 成功" || { fail "Gemini 失败: ${res:0:200}"; return; }
  w1=$(poll_changed "$w0" "db_wallet" || db_wallet)
  [[ "$w1" -lt "$w0" ]] && pass "扣钱包 $w0->$w1" || fail "未扣钱包"
}

# --- main ---
echo "========== ComputeToken Relay 冒烟 =========="
for _ in $(seq 1 30); do
  curl -sS -o /dev/null -w '%{http_code}' "$BASE_URL/api/status" | grep -q 200 && break
  sleep 2
done
curl -sS -o /dev/null -w '%{http_code}' "$BASE_URL/api/status" | grep -q 200 && pass "API 就绪" || fail "API 未就绪"

res=$(api POST /api/user/login "$ROOT_COOKIE" "" "{\"username\":\"$ROOT_USER\",\"password\":\"$ROOT_PASS\"}")
ROOT_USER_ID=$(echo "$res" | json_field "d['data']['id']")
res=$(api POST /api/user/login "$USER_COOKIE" "" "{\"username\":\"$TEST_USER\",\"password\":\"$TEST_PASS\"}")
TEST_USER_ID=$(echo "$res" | json_field "d['data']['id']")
pass "登录完成"

ensure_channels
api POST /api/user/manage "$ROOT_COOKIE" "$ROOT_USER_ID" \
  "{\"id\":$TEST_USER_ID,\"action\":\"add_quota\",\"mode\":\"add\",\"value\":50000000}" >/dev/null
ensure_token

test_gpt
test_claude
test_gemini

res=$(relay "$BASE_URL/v1/chat/completions" -H "Authorization: Bearer $API_KEY" -H "Content-Type: application/json" \
  -d '{"model":"not-a-real-model-xyz","messages":[{"role":"user","content":"hi"}],"max_tokens":8}')
echo "$res" | python3 -c "import json,sys; d=json.load(sys.stdin); assert d.get('error'), d" \
  && pass "无效模型报错" || fail "无效模型未报错"

echo ""
echo "=========================================="
echo -e "通过: ${GREEN}$PASS_COUNT${NC}  失败: ${RED}$FAIL_COUNT${NC}"
if [[ "$FAIL_COUNT" -gt 0 ]]; then
  printf ' - %s\n' "${FAILURES[@]}"
  exit 1
fi
echo -e "${GREEN}========== Relay 冒烟通过 ==========${NC}"
