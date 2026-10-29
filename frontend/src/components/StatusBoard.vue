<script setup lang="ts">
// Live sending status: polls GET /status every 2 s (paused while the tab is
// hidden, stopped once the campaign is completed).
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api } from '../api'
import { errorText } from '../lib/errors'
import { CAMPAIGN_STATUS, CHANNEL_LABEL, num, pct, time } from '../lib/format'
import type { CampaignStatus, ChannelStatus, StatusReport } from '../types'

const props = defineProps<{ campaignId: string }>()
const emit = defineEmits<{ status: [s: CampaignStatus] }>()

const POLL_MS = 2000
const report = ref<StatusReport | null>(null)
const error = ref('')
const updatedAt = ref<Date | null>(null)
let timer: ReturnType<typeof setTimeout> | undefined
let stopped = false

const STATUSES = [
  { key: 'sent', label: 'Gönderildi', tip: 'Provider kabul etti' },
  { key: 'sending', label: 'Gönderiliyor', tip: 'Şu an bir worker tarafından işleniyor' },
  { key: 'queued', label: 'Kuyrukta', tip: 'Sırasını bekliyor (yeniden deneme dahil)' },
  { key: 'failed', label: 'Başarısız', tip: 'Kalıcı hata ya da deneme hakkı bitti' },
  { key: 'unknown', label: 'Belirsiz', tip: 'Gönderilmiş olabilir; çift gönderim riskine karşı tekrar denenmedi' },
  { key: 'skipped_no_consent', label: 'İzin yok, atlandı', tip: 'Gönderimden hemen önce kullanıcının izni kalkmış' },
] as const

async function poll() {
  clearTimeout(timer)
  if (stopped) return
  try {
    const r = await api.status(props.campaignId)
    report.value = r
    error.value = ''
    updatedAt.value = new Date()
    emit('status', r.campaign_status)
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
</script>

<template>
  <div class="stack">
    <div v-if="error" class="alert alert-danger" role="alert">
      <span class="alert-icon">!</span>
      <div class="alert-body">Durum alınamadı: {{ error }} — tekrar deneniyor.</div>
    </div>

    <div v-if="!report" class="row muted"><span class="spinner" /> Yükleniyor…</div>

    <template v-else>
      <div class="card row-between">
        <div class="row">
          <span class="badge" :class="'status-' + report.campaign_status" style="height: 28px; font-size: 13px">
            <span v-if="report.campaign_status === 'sending'" class="spinner" style="width: 12px; height: 12px" />
            {{ CAMPAIGN_STATUS[report.campaign_status] ?? report.campaign_status }}
          </span>
          <span><strong>{{ num(totals.finished) }}</strong> / {{ num(totals.total) }} mesaj sonuçlandı ({{ pct(totals.finished, totals.total) }})</span>
        </div>
        <span class="muted small">
          <template v-if="report.campaign_status === 'completed'">Tamamlandı — canlı güncelleme durdu</template>
          <template v-else>2 sn'de bir güncellenir<template v-if="updatedAt"> · son: {{ updatedAt.toLocaleTimeString('tr-TR') }}</template></template>
        </span>
      </div>

      <div class="grid-2">
        <div v-for="[ch, cs] in channels" :key="ch" class="card stack-sm">
          <div class="row-between">
            <h2>{{ CHANNEL_LABEL[ch] ?? ch }}</h2>
            <span class="muted small">
              <strong class="rate">{{ rate(cs.rate_per_second) }}</strong> mesaj/sn · son 10 sn'de {{ num(cs.sent_last_10s) }}
            </span>
          </div>
          <div class="row-between small">
            <span>{{ num(done(cs)) }} / {{ num(cs.total) }} sonuçlandı</span>
            <strong>{{ pct(done(cs), cs.total) }}</strong>
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

      <div class="card">
        <div class="card-title"><h2>Son hatalar</h2><span class="muted small">en yeni 10</span></div>
        <p v-if="report.recent_errors.length === 0" class="muted">Hata yok.</p>
        <div v-else class="table-wrap" style="box-shadow: none">
          <table class="table">
            <thead>
              <tr><th>Zaman</th><th>Kanal</th><th>Kullanıcı</th><th>Durum</th><th>Deneme</th><th>Hata</th></tr>
            </thead>
            <tbody>
              <tr v-for="e in report.recent_errors" :key="e.message_id">
                <td class="nowrap">{{ time(e.at) }}</td>
                <td>{{ CHANNEL_LABEL[e.channel] ?? e.channel }}</td>
                <td class="mono">#{{ e.user_id }}</td>
                <td><span class="row" style="gap: 6px"><span class="dot" :class="'bg-' + e.status" />{{ STATUSES.find((s) => s.key === e.status)?.label ?? e.status }}</span></td>
                <td>{{ e.attempts }}</td>
                <td class="mono err">{{ e.error }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.breakdown { width: 100%; border-collapse: collapse; margin-top: 6px; font-variant-numeric: tabular-nums; }
.breakdown td { padding: 5px 4px; border-bottom: 1px solid var(--border); }
.breakdown tr:last-child td { border-bottom: 0; }
.breakdown td:first-child { width: 18px; }
.breakdown tr.zero { color: var(--text-3); }
.numcell { text-align: right; width: 80px; }
.tip { display: inline-grid; place-items: center; width: 16px; height: 16px; border-radius: 50%; background: var(--st-unknown); color: #fff; font-size: 11px; font-weight: 700; cursor: help; margin-left: 4px; }
.rate { color: var(--text); font-size: 15px; }
.err { word-break: break-word; max-width: 360px; }
</style>
