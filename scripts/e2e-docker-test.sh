#!/usr/bin/env bash
#
# Full-flow smoke test for the Dockerized stack: builds and starts mongo +
# app via docker compose, then exercises ingest -> decrypt -> route ->
# throttle -> forward -> retry -> deactivate/reactivate backlog
# preservation -> crash recovery against the real running containers, and
# tears everything down afterward. Safe to re-run — uses an isolated
# compose project name so it never touches a real deployment on the same
# host.
#
# Requires on the host: docker, docker compose (v2 plugin), curl, python3,
# go (only to build the tiny host-side encryption helper under
# scripts/e2e-docker-test/ — the app itself never needs Go to run in
# Docker; this is dev/CI tooling, not something an end user running the
# container needs).
#
#   ./scripts/e2e-docker-test.sh

set -uo pipefail
cd "$(dirname "$0")/.."

PROJECT="queue-manager-e2e"
export APP_HOST_PORT="18099"
export JWT_SECRET="e2e-test-secret-do-not-use-in-prod"
BASE_URL="http://127.0.0.1:${APP_HOST_PORT}"
KEY="abcdefghij0123456789ABCDEFGHIJ01"
IV="0123456789abcdef"
NETWORK="${PROJECT}_default"
COOKIES="$(mktemp)"
FAILED=0

log()  { printf '\033[1;34m[e2e]\033[0m %s\n' "$*"; }
pass() { printf '\033[1;32m[ok]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[FAIL]\033[0m %s\n' "$*"; FAILED=1; }
die()  { printf '\033[1;31m[FATAL]\033[0m %s\n' "$*"; exit 1; }

# jget '<python subscript expr>' — pipe JSON in, print one field out.
jget() { python3 -c "import json,sys
d=json.load(sys.stdin)
print(d$1)"; }

cleanup() {
  log "cleaning up"
  docker rm -f e2e-dest-a e2e-dest-b >/dev/null 2>&1
  docker compose -p "$PROJECT" down -v >/dev/null 2>&1
  rm -f "$COOKIES" /tmp/e2e_tmpl_broken.json /tmp/e2e_tmpl_healed.json
}
trap cleanup EXIT

for bin in docker curl python3 go; do
  command -v "$bin" >/dev/null 2>&1 || die "$bin is required on PATH to run this script"
done
docker compose version >/dev/null 2>&1 || die "docker compose (v2 plugin) is required"

wait_for() {
  local desc="$1" url="$2" tries=60 i
  for ((i = 0; i < tries; i++)); do
    if curl -sf -o /dev/null "$url"; then
      return 0
    fi
    sleep 1
  done
  die "timed out waiting for $desc ($url)"
}

# Polls one request until it leaves pending/processing; prints the final
# status (or "timeout").
wait_status() {
  local req_id="$1" tries=20 i status
  for ((i = 0; i < tries; i++)); do
    status=$(curl -s -b "$COOKIES" "$BASE_URL/api/requests/$req_id" | jget "['request_process_status']" 2>/dev/null)
    if [ "$status" = "success" ] || [ "$status" = "failed" ]; then
      echo "$status"
      return 0
    fi
    sleep 0.5
  done
  echo "timeout"
}

log "building and starting mongo + app (project=$PROJECT, port=$APP_HOST_PORT)"
if ! docker compose -p "$PROJECT" up -d --build; then
  die "docker compose up failed — is port $APP_HOST_PORT already in use?"
fi

wait_for "app healthz" "$BASE_URL/healthz"
pass "app is up on $BASE_URL"

docker network inspect "$NETWORK" >/dev/null 2>&1 || die "expected compose network $NETWORK not found"
MONGO_CID=$(docker compose -p "$PROJECT" ps -q mongo)
[ -n "$MONGO_CID" ] || die "could not find the mongo container"

log "starting two throwaway destination containers on $NETWORK"
docker run -d --rm --name e2e-dest-a --network "$NETWORK" \
  -v "$(pwd)/scripts/e2e-docker-test/echo_dest.py:/echo.py:ro" \
  python:3-alpine python3 /echo.py >/dev/null
docker run -d --rm --name e2e-dest-b --network "$NETWORK" \
  -v "$(pwd)/scripts/e2e-docker-test/echo_dest.py:/echo.py:ro" \
  python:3-alpine python3 /echo.py >/dev/null
sleep 1

log "seeding the first console admin"
if ! docker compose -p "$PROJECT" run --rm app /app/seedadmin -email=e2e@test.local -password=e2etestpass123 -name="E2E Test" >/dev/null; then
  die "seedadmin failed"
fi

log "logging in"
LOGIN_STATUS=$(curl -s -c "$COOKIES" -o /dev/null -w '%{http_code}' -X POST "$BASE_URL/api/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"admin_email":"e2e@test.local","admin_password":"e2etestpass123"}')
if [ "$LOGIN_STATUS" = "200" ]; then pass "login"; else die "login returned $LOGIN_STATUS"; fi

log "creating product"
PRODUCT=$(curl -s -b "$COOKIES" -X POST "$BASE_URL/api/products" -H 'Content-Type: application/json' \
  -d "{\"product_name\":\"E2E Product\",\"product_description\":\"\",\"product_authentication_key\":\"$KEY\",\"product_authentication_iv\":\"$IV\"}")
PRODUCT_ID=$(echo "$PRODUCT" | jget "['id']" 2>/dev/null)
if [ -n "$PRODUCT_ID" ]; then pass "product created ($PRODUCT_ID)"; else die "product creation failed: $PRODUCT"; fi

log "creating a two-destination routed transmission channel"
TEMPLATE=$(curl -s -b "$COOKIES" -X POST "$BASE_URL/api/templates" -H 'Content-Type: application/json' -d "{
  \"product_id\":\"$PRODUCT_ID\",
  \"template_name\":\"E2E Channel\",
  \"template_description\":\"\",
  \"template_entry_point\":\"e2e-channel\",
  \"template_routing_field\":\"route\",
  \"template_request_ttl_days\":7,
  \"destinations\":[
    {\"destination_name\":\"Dest A\",\"destination_routing_value\":\"a\",\"destination_end_point\":\"http://e2e-dest-a:8000/hook\",\"destination_throttling_second\":1},
    {\"destination_name\":\"Dest B\",\"destination_routing_value\":\"b\",\"destination_end_point\":\"http://e2e-dest-b:8000/hook\",\"destination_throttling_second\":0}
  ]}")
TEMPLATE_ID=$(echo "$TEMPLATE" | jget "['id']" 2>/dev/null)
DEST_A_ID=$(echo "$TEMPLATE" | jget "['destinations'][0]['id']" 2>/dev/null)
DEST_B_ID=$(echo "$TEMPLATE" | jget "['destinations'][1]['id']" 2>/dev/null)
if [ -n "$TEMPLATE_ID" ]; then pass "template created ($TEMPLATE_ID)"; else die "template creation failed: $TEMPLATE"; fi
if echo "$TEMPLATE" | grep -q 'template_exchange\|destination_queue'; then
  fail "response still contains RabbitMQ-era fields"
else
  pass "no leftover exchange/queue fields in API response"
fi

log "activating"
curl -s -b "$COOKIES" -X PATCH "$BASE_URL/api/templates/$TEMPLATE_ID/activate" >/dev/null

encrypt() { go run ./scripts/e2e-docker-test/encrypt.go "$KEY" "$IV" "$1"; }

log "ingesting a payload routed to Dest A"
ENC_A=$(encrypt '{"route":"a","order_id":"A-1"}')
RESP_A=$(curl -s -X POST "$BASE_URL/ingest/e2e-channel" --data-binary "$ENC_A")
REQ_A=$(echo "$RESP_A" | jget "['request_id']" 2>/dev/null)
STATUS_A=$(wait_status "$REQ_A")
if [ "$STATUS_A" = "success" ]; then pass "routed-to-A request delivered"; else fail "routed-to-A request ended in $STATUS_A"; fi

REQ_A_DOC=$(curl -s -b "$COOKIES" "$BASE_URL/api/requests/$REQ_A")
BODY_A=$(echo "$REQ_A_DOC" | jget "['request_body']" 2>/dev/null)
if [ "$BODY_A" = '{"route":"a","order_id":"A-1"}' ]; then pass "decrypted body matches plaintext exactly"; else fail "decrypted body mismatch: $BODY_A"; fi
DEST_ID_A=$(echo "$REQ_A_DOC" | jget "['destination_id']" 2>/dev/null)
if [ "$DEST_ID_A" = "$DEST_A_ID" ]; then pass "routed to the correct destination (A)"; else fail "routing mismatch: got $DEST_ID_A want $DEST_A_ID"; fi

log "ingesting a payload routed to Dest B"
ENC_B=$(encrypt '{"route":"b","order_id":"B-1"}')
RESP_B=$(curl -s -X POST "$BASE_URL/ingest/e2e-channel" --data-binary "$ENC_B")
REQ_B=$(echo "$RESP_B" | jget "['request_id']" 2>/dev/null)
STATUS_B=$(wait_status "$REQ_B")
if [ "$STATUS_B" = "success" ]; then pass "routed-to-B request delivered"; else fail "routed-to-B request ended in $STATUS_B"; fi

log "bursting 3 more requests at throttled Dest A (configured: 1s)"
for n in 2 3 4; do
  ENC=$(encrypt "{\"route\":\"a\",\"order_id\":\"A-$n\"}")
  curl -s -X POST "$BASE_URL/ingest/e2e-channel" --data-binary "$ENC" >/dev/null
done
sleep 4
GAPS=$(docker logs e2e-dest-a 2>&1 | grep -oE 'ts=[0-9.]+' | cut -d= -f2 | tail -n 3 | python3 -c "
import sys
ts = [float(x) for x in sys.stdin]
print(' '.join(f'{b-a:.2f}' for a, b in zip(ts, ts[1:])))
")
log "observed gaps between the last 3 Dest A deliveries: $GAPS"
if python3 -c "
gaps = [float(x) for x in '$GAPS'.split()]
raise SystemExit(0 if len(gaps) >= 2 and all(0.8 <= g <= 2.0 for g in gaps) else 1)
"; then
  pass "throttle pacing held ~1s between deliveries"
else
  fail "throttle pacing out of expected range: $GAPS"
fi

log "forcing a delivery failure against Dest B, then retrying after fixing it"
curl -s -b "$COOKIES" "$BASE_URL/api/templates/$TEMPLATE_ID" | python3 -c "
import json, sys
t = json.load(sys.stdin)
t['template_entry_point'] = 'e2e-channel'
t['destinations'][1]['destination_end_point'] = 'http://127.0.0.1:1/dead'
json.dump(t, sys.stdout)
" > /tmp/e2e_tmpl_broken.json
curl -s -b "$COOKIES" -X PUT "$BASE_URL/api/templates/$TEMPLATE_ID" -H 'Content-Type: application/json' -d @/tmp/e2e_tmpl_broken.json >/dev/null

ENC_FAIL=$(encrypt '{"route":"b","order_id":"FAIL-1"}')
RESP_FAIL=$(curl -s -X POST "$BASE_URL/ingest/e2e-channel" --data-binary "$ENC_FAIL")
REQ_FAIL=$(echo "$RESP_FAIL" | jget "['request_id']" 2>/dev/null)
STATUS_FAIL=$(wait_status "$REQ_FAIL")
if [ "$STATUS_FAIL" = "failed" ]; then pass "forced failure recorded as failed"; else fail "expected failed, got $STATUS_FAIL"; fi

curl -s -b "$COOKIES" "$BASE_URL/api/templates/$TEMPLATE_ID" | python3 -c "
import json, sys
t = json.load(sys.stdin)
t['template_entry_point'] = 'e2e-channel'
t['destinations'][1]['destination_end_point'] = 'http://e2e-dest-b:8000/hook'
json.dump(t, sys.stdout)
" > /tmp/e2e_tmpl_healed.json
curl -s -b "$COOKIES" -X PUT "$BASE_URL/api/templates/$TEMPLATE_ID" -H 'Content-Type: application/json' -d @/tmp/e2e_tmpl_healed.json >/dev/null

curl -s -b "$COOKIES" -X POST "$BASE_URL/api/requests/$REQ_FAIL/retry" >/dev/null
STATUS_RETRY=$(wait_status "$REQ_FAIL")
if [ "$STATUS_RETRY" = "success" ]; then pass "retry succeeded after fixing the endpoint"; else fail "retry ended in $STATUS_RETRY"; fi

log "deactivate: confirming backlog is preserved, not dropped"
curl -s -b "$COOKIES" -X PATCH "$BASE_URL/api/templates/$TEMPLATE_ID/deactivate" >/dev/null
sleep 0.5
docker exec "$MONGO_CID" mongosh queue_manager --quiet --eval "
db.requests.insertOne({
  product_id: ObjectId('$PRODUCT_ID'), template_id: ObjectId('$TEMPLATE_ID'), destination_id: ObjectId('$DEST_A_ID'),
  request_id: 'e2e-backlog-test', template_entry_point: '/ingest/e2e-channel', destination_end_point: 'http://e2e-dest-a:8000/hook',
  request_header: {}, request_body: '{\"route\":\"a\",\"order_id\":\"BACKLOG-1\"}', request_response: '', request_status_code: 0,
  request_process_status: 'pending', retry_count: 0, request_at: new Date(), request_expiry_ttl: new Date(Date.now()+7*24*3600*1000)
})" >/dev/null
sleep 1.5
if docker logs e2e-dest-a 2>&1 | grep -q BACKLOG-1; then
  fail "deactivated channel still delivered — backlog should have stayed put"
else
  pass "backlog untouched while deactivated"
fi

curl -s -b "$COOKIES" -X PATCH "$BASE_URL/api/templates/$TEMPLATE_ID/activate" >/dev/null
sleep 1.5
if docker logs e2e-dest-a 2>&1 | grep -q BACKLOG-1; then
  pass "backlog drained immediately on reactivate"
else
  fail "backlog was not delivered after reactivating"
fi

log "crash recovery: a request stuck in 'processing' should resume after a restart"
docker exec "$MONGO_CID" mongosh queue_manager --quiet --eval "
db.requests.insertOne({
  product_id: ObjectId('$PRODUCT_ID'), template_id: ObjectId('$TEMPLATE_ID'), destination_id: ObjectId('$DEST_B_ID'),
  request_id: 'e2e-crash-test', template_entry_point: '/ingest/e2e-channel', destination_end_point: 'http://e2e-dest-b:8000/hook',
  request_header: {}, request_body: '{\"route\":\"b\",\"order_id\":\"CRASH-1\"}', request_response: '', request_status_code: 0,
  request_process_status: 'processing', retry_count: 0, request_at: new Date(), request_expiry_ttl: new Date(Date.now()+7*24*3600*1000)
})" >/dev/null
docker compose -p "$PROJECT" kill app >/dev/null 2>&1
docker compose -p "$PROJECT" up -d app >/dev/null
wait_for "app healthz after restart" "$BASE_URL/healthz"
sleep 1.5
CRASH_STATUS=$(curl -s -b "$COOKIES" "$BASE_URL/api/requests" | python3 -c "
import json, sys
rows = json.load(sys.stdin)
matches = [r for r in rows if r['request_id'] == 'e2e-crash-test']
print(matches[0]['request_process_status'] if matches else 'missing')
")
if [ "$CRASH_STATUS" = "success" ]; then
  pass "stuck-processing request recovered and delivered after restart"
else
  fail "crash recovery failed, status=$CRASH_STATUS"
fi

echo
if [ "$FAILED" -eq 0 ]; then
  log "ALL CHECKS PASSED"
  exit 0
else
  log "ONE OR MORE CHECKS FAILED — see [FAIL] lines above"
  exit 1
fi
