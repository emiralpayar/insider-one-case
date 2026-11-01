<script setup lang="ts">
// Live sending status: polls GET /status every 2 s (paused while the tab is
// hidden, stopped once the campaign is completed).
import { computed, onMounted, onUnmounted, ref, shallowRef } from 'vue'
import { api } from '../api'
import { errorText } from '../lib/errors'
import { CAMPAIGN_STATUS, CHANNEL_LABEL, num, pct, time } from '../lib/format'
import { MESSAGE_STATUS_LABEL, MESSAGE_STATUSES } from '../lib/lists'
import { freshRows, humanReason, outcomeText, prependRow, revealDelay, type LogRow } from '../lib/log'
import type { CampaignStatus, ChannelStatus, MessageError, StatusReport } from '../types'
import Icon from './Icon.vue'

const props = defineProps<{ campaignId: string }>()
const emit = defineEmits<{ status: [s: CampaignStatus]; report: [r: StatusReport] }>()

const POLL_MS = 2000
const report = ref<StatusReport | null>(null)
const error = ref('')
const updatedAt = ref<Date | null>(null)
let timer: ReturnType<typeof setTimeout> | undefined
let stopped = false

// ---- Gönderim günlüğü: client-side log, new rows revealed one by one ----
const log = shallowRef<LogRow[]>([])
let pending: LogRow[] = [] // oldest first; revealed from the front
let revealTimer: ReturnType<typeof setTimeout> | undefined
let primed = false

function revealNext() {
  const row = pending.shift()
  if (!row) return
  log.value = prependRow(log.value, row)
  if (pending.length) revealTimer = setTimeout(revealNext, revealGap)
}
let revealGap = 0
function mergeLog(incoming: MessageError[]) {
  // Rows of the previous poll still waiting are shown at once.
  clearTimeout(revealTimer)
  while (pending.length) revealNext()
  clearTimeout(revealTimer)
  const fresh = freshRows(log.value, incoming)
  if (!fresh.length) return
  if (!primed) {
    // First snapshot: no animation, just the list.
    log.value = fresh
    return
  }
  pending = [...fresh].reverse()
  revealGap = revealDelay(pending.length, POLL_MS)
  revealNext()
}

const STATUSES = MESSAGE_STATUSES

async function poll() {
  clearTimeout(timer)
  if (stopped) return
  try {
    const r = await api.status(props.campaignId)
    report.value = r
    mergeLog(r.recent_errors ?? [])
    primed = true
    error.value = ''
    updatedAt.value = new Date()
    emit('status', r.campaign_status)
    emit('report', r)
    if (r.campaign_status === 'completed') return // final: stop polling
  } catch (e) {
    error.value = errorText(e)
  }
  schedule()
}
function schedule() {
  clearTimeout(timer)
  if (!stopped && !document.hidden) timer = setTimeout(poll, POLL_MS)
}
function onVisibility() {
  if (document.hidden) clearTimeout(timer)
  else if (report.value?.campaign_status !== 'completed') poll()
}

onMounted(() => {
  document.addEventListener('visibilitychange', onVisibility)
  poll()
})
onUnmounted(() => {
  stopped = true
  clearTimeout(timer)
  clearTimeout(revealTimer)
  document.removeEventListener('visibilitychange', onVisibility)
})

const channels = computed(() => Object.entries(report.value?.channels ?? {}).sort(([a], [b]) => a.localeCompare(b)).reverse())
const n = (cs: ChannelStatus, k: string) => cs.by_status[k] ?? 0
const done = (cs: ChannelStatus) => n(cs, 'sent') + n(cs, 'failed') + n(cs, 'unknown') + n(cs, 'skipped_no_consent')
const totals = computed(() => {
  let total = 0
  let finished = 0
  let sent = 0
  for (const [, cs] of channels.value) {
    total += cs.total
    finished += done(cs)
    sent += n(cs, 'sent')
  }
  return { total, finished, sent }
})
const rate = (r: number) => r.toLocaleString('tr-TR', { maximumFractionDigits: 1 })
const totalRate = computed(() => channels.value.reduce((a, [, cs]) => a + cs.rate_per_second, 0))
const problems = computed(() => {
  let failed = 0
  let unknown = 0
  for (const [, cs] of channels.value) {
    failed += n(cs, 'failed')
    unknown += n(cs, 'unknown')
  }
  return { failed, unknown }
})
const STATUS_LABEL = MESSAGE_STATUS_LABEL
</script>

<template>
  <div class="stack">
    <div v-if="error" class="alert alert-danger" role="alert">
      <span class="alert-icon">!</span>
      <div class="alert-body">Durum alınamadı: {{ error }} — tekrar deneniyor.</div>
    </div>

    <div v-if="!report" class="stack">
      <div class="stats stats-4">
        <div v-for="i in 4" :key="i" class="stat"><span class="skeleton" style="width: 50%" /><span class="skeleton" style="width: 70%; height: 24px" /></div>
      </div>
      <div class="row muted"><span class="spinner" /> Yükleniyor…</div>
    </div>

    <template v-else>
      <div class="card overview">
        <div class="row-between">
          <div class="row" style="gap: 14px">
            <span class="badge badge-dot" :class="'status-' + report.campaign_status" style="height: 28px; font-size: 13px; padding: 0 12px">
              {{ CAMPAIGN_STATUS[report.campaign_status] ?? report.campaign_status }}
            </span>
            <span class="ov-count"><strong>{{ num(totals.finished) }}</strong> / {{ num(totals.total) }} mesaj sonuçlandı</span>
          </div>
          <span class="muted small row" style="gap: 6px">
            <template v-if="report.campaign_status === 'completed'"><Icon name="check-circle" :size="14" /> Tamamlandı — canlı güncelleme durdu</template>
            <template v-else><span class="live-dot" /> 2 sn'de bir güncellenir<template v-if="updatedAt"> · son: {{ updatedAt.toLocaleTimeString('tr-TR') }}</template></template>
          </span>
        </div>
        <div class="ov-bar">
          <div class="ov-fill" :class="{ done: report.campaign_status === 'completed' }" :style="{ width: (totals.total ? (totals.finished / totals.total) * 100 : 0) + '%' }" />
        </div>
        <div class="ov-pct tabular">{{ pct(totals.finished, totals.total) }}</div>
      </div>

      <div class="stats stats-4">
        <div class="stat">
          <div class="stat-head"><span class="stat-icon"><Icon name="layers" :size="14" /></span><span class="stat-label">Toplam mesaj</span></div>
          <div class="stat-value">{{ num(totals.total) }}</div>
          <div class="stat-sub">{{ channels.map(([ch, cs]) => `${CHANNEL_LABEL[ch] ?? ch} ${num(cs.total)}`).join(' · ') }}</div>
        </div>
        <div class="stat">
          <div class="stat-head"><span class="stat-icon green"><Icon name="check-circle" :size="14" /></span><span class="stat-label">Gönderildi</span></div>
          <div class="stat-value">{{ num(totals.sent) }}</div>
          <div class="stat-sub">{{ pct(totals.sent, totals.total) }} başarı</div>
        </div>
        <div class="stat">
          <div class="stat-head"><span class="stat-icon red"><Icon name="x-circle" :size="14" /></span><span class="stat-label">Başarısız / belirsiz</span></div>
          <div class="stat-value">{{ num(problems.failed) }}<span class="unit">/ {{ num(problems.unknown) }}</span></div>
          <div class="stat-sub">kalıcı hata / tekrar denenmeyen</div>
        </div>
        <div class="stat">
          <div class="stat-head"><span class="stat-icon blue"><Icon name="gauge" :size="14" /></span><span class="stat-label">Anlık hız</span></div>
          <div class="stat-value">{{ rate(totalRate) }}<span class="unit">mesaj/sn</span></div>
          <div class="stat-sub">son 10 sn ortalaması, tüm kanallar</div>
        </div>
      </div>

      <div class="grid-2">
        <div v-for="[ch, cs] in channels" :key="ch" class="card card-flush">
          <div class="card-head">
            <span class="card-icon" :class="ch === 'sms' ? 'violet' : 'teal'"><Icon :name="ch === 'sms' ? 'sms' : 'mail'" :size="17" /></span>
            <h2>{{ CHANNEL_LABEL[ch] ?? ch }}<span class="sub">{{ num(done(cs)) }} / {{ num(cs.total) }} sonuçlandı</span></h2>
            <div class="rate-box">
              <strong class="rate tabular">{{ rate(cs.rate_per_second) }}</strong>
              <span class="muted tiny">mesaj/sn · son 10 sn'de {{ num(cs.sent_last_10s) }}</span>
            </div>
          </div>
          <div class="card-body stack-sm">
            <div class="row-between small">
              <span class="muted">İlerleme</span>
              <strong class="tabular">{{ pct(done(cs), cs.total) }}</strong>
            </div>
            <div class="progress" role="img" :aria-label="`${CHANNEL_LABEL[ch]} ilerleme`">
              <div
                v-for="s in STATUSES"
                :key="s.key"
                :class="'bg-' + s.key"
                :style="{ width: cs.total ? (n(cs, s.key) / cs.total) * 100 + '%' : '0' }"
                :title="`${s.label}: ${num(n(cs, s.key))}`"
              />
            </div>
            <table class="breakdown">
              <tbody>
                <tr v-for="s in STATUSES" :key="s.key" :title="s.tip" :class="{ zero: n(cs, s.key) === 0 }">
                  <td><span class="dot" :class="'bg-' + s.key" /></td>
                  <td>
                    {{ s.label }}
                    <span v-if="s.key === 'unknown'" class="tip" :title="s.tip" tabindex="0" aria-label="Açıklama">?</span>
                  </td>
                  <td class="numcell">{{ num(n(cs, s.key)) }}</td>
                  <td class="numcell muted">{{ pct(n(cs, s.key), cs.total) }}</td>
                </tr>
              </tbody>
            </table>
            <p v-if="n(cs, 'unknown') > 0" class="small" style="color: var(--st-unknown)">
              {{ num(n(cs, 'unknown')) }} mesaj belirsiz: gönderilmiş olabilir; çift gönderim riskine karşı tekrar denenmedi.
            </p>
          </div>
        </div>
      </div>

      <div class="card card-flush log-card">
        <div class="card-head">
          <span class="card-icon amber"><Icon name="activity" :size="17" /></span>
          <h2>Gönderim günlüğü<span class="sub">Yeniden denemeler ve hatalar · {{ report.campaign_status === 'completed' ? 'tamamlandı' : 'canlı' }}</span></h2>
          <span v-if="report.campaign_status !== 'completed'" class="badge badge-info"><span class="live-dot" /> Canlı</span>
          <span class="muted small tabular">{{ num(log.length) }} kayıt</span>
        </div>
        <div class="log-cols" aria-hidden="true">
          <span>Zaman</span><span>Kanal</span><span>Kullanıcı</span><span>Neden</span><span>Sonuç</span>
        </div>
        <div class="log-list" role="log" aria-live="polite" aria-label="Gönderim günlüğü">
          <div v-if="log.length === 0" class="log-empty">
            <span class="card-icon green"><Icon name="check-circle" :size="18" /></span>
            <div>
              <strong>Henüz hata ya da yeniden deneme yok</strong>
              <div class="muted small">Sağlayıcı hata döndürdüğünde kayıtlar burada akar.</div>
            </div>
          </div>
          <TransitionGroup name="log" tag="div">
            <div v-for="e in log" :key="e.key" class="log-row">
              <span class="mono muted">{{ time(e.at) }}</span>
              <span><span class="badge" :class="e.channel === 'sms' ? 'ch-sms' : 'ch-email'">{{ CHANNEL_LABEL[e.channel] ?? e.channel }}</span></span>
              <span class="mono">#{{ e.user_id }}</span>
              <span class="reason" :title="e.error">{{ humanReason(e.error) }}</span>
              <span class="outcome" :class="'out-' + e.status" :title="(STATUS_LABEL[e.status] ?? e.status) + ' · ' + e.attempts + ' deneme'">
                <span class="dot" :class="'bg-' + e.status" />{{ outcomeText(e.status, e.attempts) }}
              </span>
            </div>
          </TransitionGroup>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.overview { position: relative; padding-bottom: 18px; }
.ov-count { font-size: 15px; }
.ov-count strong { font-size: 18px; }
.ov-bar { margin-top: 14px; height: 10px; border-radius: 5px; background: var(--surface-3); overflow: hidden; margin-right: 56px; }
.ov-fill { height: 100%; background: var(--brand-grad); border-radius: 5px; transition: width 0.6s ease; }
.ov-fill.done { background: var(--st-sent); }
.ov-pct { position: absolute; right: var(--space-5); bottom: 12px; font-weight: 700; font-size: 14px; }
.live-dot { width: 7px; height: 7px; border-radius: 50%; background: var(--info); display: inline-block; animation: pulse 1.6s ease-in-out infinite; }
@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.3; } }
.rate-box { display: flex; flex-direction: column; align-items: flex-end; line-height: 1.2; }
.rate { color: var(--text); font-size: 20px; }
.breakdown { width: 100%; border-collapse: collapse; margin-top: 6px; font-variant-numeric: tabular-nums; }
.breakdown td { padding: 6px 4px; border-bottom: 1px solid var(--border); }
.breakdown tr:last-child td { border-bottom: 0; }
.breakdown td:first-child { width: 18px; }
.breakdown tr.zero { color: var(--text-4); }
.numcell { text-align: right; width: 80px; }
.tip { display: inline-grid; place-items: center; width: 16px; height: 16px; border-radius: 50%; background: var(--st-unknown); color: #fff; font-size: 11px; font-weight: 700; cursor: help; margin-left: 4px; }

/* log: fixed height, the list scrolls inside */
.log-cols, .log-row { display: grid; grid-template-columns: 88px 84px 80px minmax(0, 1fr) 230px; gap: 12px; align-items: center; padding: 0 var(--space-5); }
.log-cols { height: 34px; font-size: 11px; font-weight: 650; letter-spacing: 0.05em; text-transform: uppercase; color: var(--text-3); background: var(--surface-2); border-bottom: 1px solid var(--border); }
.log-list { position: relative; height: 380px; overflow-y: auto; overscroll-behavior: contain; }
.log-row { height: 42px; border-bottom: 1px solid var(--border); background: var(--surface); font-size: 13px; }
.log-row:hover { background: #fafbff; }
.reason { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; cursor: help; }
.outcome { display: inline-flex; align-items: center; gap: 7px; font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.out-sent { color: var(--success); }
.out-failed { color: var(--danger); }
.out-unknown { color: var(--st-unknown); }
.out-queued, .out-sending { color: var(--info); }
.out-skipped_no_consent { color: var(--warn); }
.ch-sms { background: var(--var-name-soft); color: var(--var-name); border-color: var(--var-name-border); }
.ch-email { background: var(--var-city-soft); color: var(--var-city); border-color: var(--var-city-border); }
.log-empty { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; gap: 12px; text-align: left; }

.log-enter-active { transition: opacity 0.35s ease, transform 0.35s ease, background-color 1.6s ease; }
.log-enter-from { opacity: 0; transform: translateY(-10px); background-color: var(--accent-soft); }
.log-enter-to { background-color: var(--surface); }
.log-move { transition: transform 0.35s ease; }
/* replaced rows disappear at once; only new rows animate */
.log-leave-active { display: none; }
</style>
