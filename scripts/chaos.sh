#!/usr/bin/env bash
# make chaos: SIGKILL a worker in the middle of a real campaign and check that
# nothing is lost or delivered twice. Runs against the stack started with
# `docker compose up` (api on :8080, mock provider on :8081, 2 workers).
#
#   1. create a campaign for all users on both channels (no LLM: the rule and
#      the content are set through the API) and approve it
#   2. once sending is under way, SIGKILL the worker that leads the SMS channel
#      (no graceful shutdown: its in-flight sends are cut mid-request)
#   3. record the rows that worker held in 'sending' at the moment of the kill
#   4. wait until every message of the campaign is terminal
#   5. check: nothing stuck in queued/sending, the surviving worker took over
#      after the kill, every message the killed worker held got a final state,
#      and nothing was delivered twice. With idempotency on, the provider's
#      delivery counters cannot exceed 1 whatever the queue does, so the queue
#      is checked with the mock's detectors instead: no key in flight twice,
#      and a resend after a success answer only for messages the dead worker
#      had already sent.
# The killed worker is started again at the end.
set -euo pipefail

API=${API:-http://localhost:8080/api}
PROVIDER=${PROVIDER:-http://localhost:8081}
KILL_AFTER_SENT=${KILL_AFTER_SENT:-400}
TIMEOUT=${TIMEOUT:-300}

say() { printf '\n\033[1m%s\033[0m\n' "$*"; }
fail() { printf '\n\033[31mFAIL: %s\033[0m\n' "$*"; exit 1; }
psql_() { docker compose exec -T postgres psql -U composer -d composer -At -F ' ' -c "$1"; }
json() { curl -sf -H 'Content-Type: application/json' "$@"; }
field() { sed -nE "s/.*\"$1\":([0-9]+).*/\1/p"; } # first integer value of a JSON key

curl -sf "${API%/api}/readyz" >/dev/null || fail "stack is not running; start it with: docker compose up --build -d --wait"
workers=$(docker compose ps -q worker)
[ "$(echo "$workers" | wc -l | tr -d ' ')" -ge 2 ] || fail "need 2 running workers (docker compose up -d)"

# Leader of a channel = the worker whose last leadership log line for it, since
# the container's current start, is "acquired". (A SIGKILLed worker never logs
# "released", and a restarted container keeps its old logs.)
leader_of() { # channel [workers...]
  local ch=$1; shift
  for w in ${@:-$workers}; do
    if docker logs --since "$(docker inspect -f '{{.State.StartedAt}}' "$w")" "$w" 2>&1 |
      grep -E "dispatch $ch: (acquired|released)" | tail -1 | grep -q acquired; then
      echo "$w"; return
    fi
  done
}
name_of() { docker inspect -f '{{.Name}}' "$1" | sed 's#^/##'; }
idem=$(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' $(echo "$workers" | head -1) | sed -n 's/^PROVIDER_IDEMPOTENCY=//p')
idem=${idem:-true}

before=$(curl -sf "$PROVIDER/v1/stats")
delivered_before=$(echo "$before" | field delivered)
concurrent_before=$(echo "$before" | field concurrent_same_key)
resent_before=$(echo "$before" | field resent_after_ack)
# An older mock-provider image lacks these counters; empty would read as 0.
[ -n "$concurrent_before" ] && [ -n "$resent_before" ] ||
  fail "mock provider has no concurrent_same_key/resent_after_ack counters; rebuild it (make up)"

say "1) Kampanya oluşturuluyor (herkese, SMS + e-posta)"
id=$(json -X POST "$API/campaigns" -d '{"brief":"Chaos test: worker gönderim ortasında öldürülüyor."}' | sed -nE 's/.*"id":"([0-9a-f-]{36})".*/\1/p')
[ -n "$id" ] || fail "could not create campaign"
json -X PUT "$API/campaigns/$id/segment" -d '{"rule":{"audience":"all","conditions":[],"unmapped":[]}}' >/dev/null
json -X PUT "$API/campaigns/$id/channels" -d '{"channels":["sms","email"]}' >/dev/null
# The text carries the run id: the provider's per-recipient counter is keyed by
# (channel, to, subject, body), so identical text across runs would add up.
run=${id%%-*}
json -X PUT "$API/campaigns/$id/content" -d '{"content":{"sms":"Merhaba {{first_name}}, chaos testi '"$run"'.","email":{"subject":"Chaos testi '"$run"'","preheader":"","body":"Merhaba {{first_name}}, bu bir chaos testi mesajıdır ('"$run"')."}}}' >/dev/null
# confirm_duplicate: earlier chaos runs reached the same people with the same brief.
enq=$(json -X POST "$API/campaigns/$id/approve" -d '{"confirm_large_audience":true,"confirm_duplicate":true}')
echo "kampanya $id onaylandı: $enq"
echo "provider idempotency: $idem"

say "2) Gönderim başlaması bekleniyor ($KILL_AFTER_SENT mesaj gönderilene kadar)"
for _ in $(seq 1 120); do
  sent=$(psql_ "select count(*) from messages where campaign_id='$id' and status='sent'")
  [ "$sent" -ge "$KILL_AFTER_SENT" ] && break
  sleep 0.5
done
victim=$(leader_of sms)
[ -n "$victim" ] || fail "could not find the SMS leader in worker logs"
channels="'sms'"
[ "$(leader_of email)" = "$victim" ] && channels="'sms','email'"
vname=$(name_of "$victim")
t0=$(psql_ "select now()")
t0_docker=$(date -u +%Y-%m-%dT%H:%M:%SZ)
docker kill -s KILL "$victim" >/dev/null
echo "gönderilen: $sent — SMS lideri $vname SIGKILL ile öldürüldü (kanallar: $channels)"

# Rows the dead worker held: 'sending' and claimed before the kill. Rows the
# surviving worker claims after taking over have a later updated_at.
inflight=$(psql_ "select id from messages where campaign_id='$id' and channel in ($channels) and status='sending' and updated_at < '$t0'")
n_inflight=$(echo "$inflight" | grep -c . || true)
echo "öldürülen worker'ın elinde yarım kalan mesaj: $n_inflight"
[ "$n_inflight" -gt 0 ] || fail "the killed worker held no in-flight messages; increase load or lower KILL_AFTER_SENT"
ids=$(echo "$inflight" | sed "s/.*/'&'/" | paste -sd, -)
# The dangerous ones: the provider already has them, but the dead worker never
# recorded it. A resend must not deliver them again.
reached=$(for m in $inflight; do curl -sf "$PROVIDER/v1/messages/$m" >/dev/null && echo "$m"; done || true)
n_reached=$(echo "$reached" | grep -c . || true)
echo "  bunlardan $n_reached tanesi provider'a ulaşmış ama sonucu kaydedilememiş"

say "3) Kalan worker'ın devralması ve kira süresinin (30 sn) dolması bekleniyor"
start=$(date +%s)
while :; do
  open=$(psql_ "select count(*) from messages where campaign_id='$id' and status in ('queued','sending')")
  [ "$open" -eq 0 ] && break
  [ $(( $(date +%s) - start )) -gt "$TIMEOUT" ] && fail "$open messages still queued/sending after ${TIMEOUT}s"
  if [ -t 1 ]; then printf '\r  açık mesaj: %-6s' "$open"; elif [ $(( ($(date +%s) - start) % 20 )) -lt 2 ]; then echo "  açık mesaj: $open"; fi
  sleep 2
done
echo
docker start "$victim" >/dev/null
echo "$vname tekrar başlatıldı"

say "4) Sonuç"
echo "kampanyanın durumları:"
psql_ "select '  '||status, count(*) from messages where campaign_id='$id' group by status order by status"
echo "yarım kalan $n_inflight mesajın son durumu:"
psql_ "select '  '||status, count(*) from messages where id in ($ids) group by status order by status"
# The survivor must have taken SMS leadership after the kill.
survivor=""
for w in $(echo "$workers" | grep -v "$victim"); do
  docker logs --since "$t0_docker" "$w" 2>&1 | grep -q "dispatch sms: acquired" && survivor=$w
done
after=$(curl -sf "$PROVIDER/v1/stats")
max=$(echo "$after" | field max_deliveries_per_recipient_message)
concurrent=$(( $(echo "$after" | field concurrent_same_key) - concurrent_before ))
resent=$(( $(echo "$after" | field resent_after_ack) - resent_before ))
delivered=$(( $(echo "$after" | field delivered) - delivered_before ))
sent_rows=$(psql_ "select count(*) from messages where campaign_id='$id' and status='sent'")
echo "provider: bu testte teslim edilen $delivered, veritabanında sent $sent_rows, alıcı başına en fazla teslim $max"
echo "provider: $after"

say "5) Kontroller"
ok=1
check() { if eval "$2"; then echo "  ✓ $1"; else echo "  ✗ $1"; ok=0; fi; }
check "takılı kalan mesaj yok (queued/sending = 0)" '[ "$open" -eq 0 ]'
check "SMS liderliğini kalan worker kill'den sonra devraldı" '[ -n "$survivor" ]'
check "aynı mesaj iki worker'dan aynı anda gönderilmedi (concurrent_same_key = 0)" '[ "$concurrent" -eq 0 ]'
if [ "$idem" = "true" ]; then
  # Only the dead worker's already-sent messages may be sent again (same key,
  # the provider deduplicates); anything more means the queue double-sent.
  check "başarılı cevaptan sonra tekrar gönderim yalnızca yarım kalanlarda ($resent ≤ $n_reached)" '[ "$resent" -le "$n_reached" ]'
else
  check "hiçbir mesaj iki kez teslim edilmedi (max = 1)" '[ "$max" -le 1 ]'
fi
if [ "$idem" = "true" ]; then
  check "provider teslim sayısı = veritabanındaki sent sayısı" '[ "$delivered" -eq "$sent_rows" ]'
else
  # Without idempotency an uncertain send is never retried: it may or may not
  # have been delivered, so delivered lies between sent and sent+unknown.
  unknown_rows=$(psql_ "select count(*) from messages where campaign_id='$id' and status='unknown'")
  check "sent ≤ provider teslim ≤ sent + unknown" '[ "$delivered" -ge "$sent_rows" ] && [ "$delivered" -le $((sent_rows + unknown_rows)) ]'
fi
if [ "$idem" = "true" ]; then
  stuck=$(psql_ "select count(*) from messages where id in ($ids) and status not in ('sent','failed','unknown','skipped_no_consent')")
  check "yarım kalanların hepsi sonuçlandı" '[ "$stuck" -eq 0 ]'
else
  nonunk=$(psql_ "select count(*) from messages where id in ($ids) and status <> 'unknown'")
  check "idempotency yok: yarım kalanlar tekrar gönderilmedi, unknown oldu" '[ "$nonunk" -eq 0 ]'
fi
if [ "$idem" != "true" ]; then
  redelivered=$(for m in $reached; do curl -sf "$PROVIDER/v1/messages/$m"; done | grep '"deliveries"' | grep -vc '"deliveries":1,' || true)
  check "ölmeden önce provider'a ulaşan $n_reached mesaj tekrar teslim edilmedi" '[ "$redelivered" -eq 0 ]'
fi
[ "$ok" -eq 1 ] || fail "chaos test failed"
printf '\n\033[32mCHAOS TEST PASSED\033[0m\n'
