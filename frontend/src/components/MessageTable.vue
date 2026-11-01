<script setup lang="ts">
// Per-recipient view of a campaign's messages (GET /campaigns/{id}/messages):
// server-side pages of 50, filterable by status (multi-select, plus the
// "Gitmeyenler" quick filter) and channel. Reloads when the dashboard's
// status counts change, so the table follows a running send.
import { computed, ref, watch } from 'vue'
import { api } from '../api'
import { errorText } from '../lib/errors'
import { CHANNEL_LABEL, dateTime, num } from '../lib/format'
import { fullName, MESSAGE_STATUS_LABEL, MESSAGE_STATUSES, messagesQuery, NOT_DELIVERED, sameStatuses, toggleStatus, type MessageFilter } from '../lib/lists'
import { humanReason } from '../lib/log'
import type { Channel, MessagePage, MessageStatus, StatusReport } from '../types'
import Icon from './Icon.vue'
import Pager from './Pager.vue'

const props = defineProps<{ campaignId: string; report: StatusReport | null }>()

const filter = ref<MessageFilter>({ statuses: [], channel: '', page: 1 })
const data = ref<MessagePage | null>(null)
const loading = ref(false)
const error = ref('')
let seq = 0

async function load() {
  const my = ++seq
  loading.value = true
  try {
    const r = await api.messages(props.campaignId, messagesQuery(filter.value))
    if (my !== seq) return
    // Live counts can shrink the filtered total under the current page.
    const last = Math.max(1, Math.ceil(r.total / r.per_page))
    if (r.total > 0 && filter.value.page > last) {
      filter.value.page = last // the deep watch reloads
      return
    }
    data.value = r
    error.value = ''
  } catch (e) {
    if (my === seq) error.value = errorText(e)
  } finally {
    if (my === seq) loading.value = false
  }
}

watch(filter, load, { deep: true, immediate: true })
// Reload when the counts move (every poll while sending; never after completion).
const countsKey = computed(() => JSON.stringify(Object.entries(props.report?.channels ?? {}).map(([ch, cs]) => [ch, cs.by_status])))
watch(countsKey, (k, old) => {
  if (old !== undefined && k !== old) load()
})

const set = (patch: Partial<MessageFilter>) => (filter.value = { ...filter.value, page: 1, ...patch })
const notDeliveredActive = computed(() => sameStatuses(filter.value.statuses, NOT_DELIVERED))
const channels = computed(() => Object.keys(props.report?.channels ?? {}).sort().reverse() as Channel[])

/** Message count per status for the selected channel (or all), from the status report. */
function count(s: MessageStatus): number {
  let n = 0
  for (const [ch, cs] of Object.entries(props.report?.channels ?? {})) {
    if (!filter.value.channel || filter.value.channel === ch) n += cs.by_status[s] ?? 0
  }
  return n
}
const notDeliveredCount = computed(() => NOT_DELIVERED.reduce((a, s) => a + count(s), 0))
</script>

<template>
  <div class="card card-flush">
    <div class="card-head">
      <span class="card-icon slate"><Icon name="users" :size="17" /></span>
      <h2>Alıcılar<span class="sub">Her alıcıya giden mesaj ve son durumu</span></h2>
      <button type="button" class="btn btn-ghost btn-sm" :disabled="loading" title="Listeyi yenile" @click="load">
        <Icon name="refresh" :size="13" /> Yenile
      </button>
    </div>

    <div class="filters">
      <div class="row" style="gap: 8px">
        <div class="segmented" role="group" aria-label="Hızlı filtre">
          <button type="button" :aria-selected="filter.statuses.length === 0" @click="set({ statuses: [] })">Tümü</button>
          <button type="button" :aria-selected="notDeliveredActive" title="Başarısız, belirsiz ve izin yok" @click="set({ statuses: [...NOT_DELIVERED] })">
            <Icon name="x-circle" :size="13" /> Gitmeyenler <span class="count">{{ num(notDeliveredCount) }}</span>
          </button>
        </div>
        <div class="segmented" role="group" aria-label="Kanal">
          <button type="button" :aria-selected="filter.channel === ''" @click="set({ channel: '' })">Tüm kanallar</button>
          <button v-for="ch in channels" :key="ch" type="button" :aria-selected="filter.channel === ch" @click="set({ channel: ch })">
            <Icon :name="ch === 'sms' ? 'sms' : 'mail'" :size="13" /> {{ CHANNEL_LABEL[ch] }}
          </button>
        </div>
      </div>
      <div class="status-chips" role="group" aria-label="Durum">
        <button
          v-for="s in MESSAGE_STATUSES"
          :key="s.key"
          type="button"
          class="fchip"
          :class="{ on: filter.statuses.includes(s.key) }"
          :aria-pressed="filter.statuses.includes(s.key)"
          :title="s.tip"
          @click="set({ statuses: toggleStatus(filter.statuses, s.key) })"
        >
          <span class="dot" :class="'bg-' + s.key" /> {{ s.label }} <span class="count">{{ num(count(s.key)) }}</span>
        </button>
      </div>
    </div>

    <div v-if="error" class="alert alert-danger" style="margin: 12px var(--space-5)">
      <span class="alert-icon">!</span>
      <div class="alert-body">Alıcılar alınamadı: {{ error }}</div>
    </div>

    <div class="table-scroll" :class="{ stale: loading && !!data }">
      <table class="table compact">
        <thead>
          <tr>
            <th>Alıcı</th>
            <th>Kanal</th>
            <th>Durum</th>
            <th class="right">Deneme</th>
            <th>Son hata</th>
            <th>Gönderim</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="!data">
            <td colspan="6"><span class="skeleton" /><span class="skeleton" style="width: 70%" /></td>
          </tr>
          <tr v-else-if="data.messages.length === 0">
            <td colspan="6" class="muted empty">
              {{ filter.statuses.length || filter.channel ? 'Bu filtreye uyan mesaj yok.' : 'Henüz mesaj yok.' }}
            </td>
          </tr>
          <tr v-for="m in data?.messages ?? []" :key="m.id">
            <td class="who">
              <div :class="{ muted: !m.first_name && !m.last_name }">{{ fullName(m.first_name, m.last_name) }}</div>
              <div class="muted tiny mono addr" :title="m.recipient">{{ m.recipient }}</div>
            </td>
            <td><span class="badge" :class="'ch-' + m.channel">{{ CHANNEL_LABEL[m.channel] ?? m.channel }}</span></td>
            <td class="nowrap">
              <span class="st" :class="'out-' + m.status"><span class="dot" :class="'bg-' + m.status" /> {{ MESSAGE_STATUS_LABEL[m.status] ?? m.status }}</span>
            </td>
            <td class="right tabular">{{ m.attempts }}</td>
            <td class="err">
              <span v-if="m.last_error" :title="m.last_error">{{ humanReason(m.last_error) }}</span>
              <span v-else class="muted">—</span>
            </td>
            <td class="nowrap small">{{ m.sent_at ? dateTime(m.sent_at) : '—' }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <Pager v-if="data" :page="filter.page" :per-page="data.per_page" :total="data.total" :loading="loading" @update:page="filter = { ...filter, page: $event }" />
  </div>
</template>

<style scoped>
.filters { display: flex; flex-direction: column; gap: 10px; padding: 12px var(--space-5); border-bottom: 1px solid var(--border); }
.status-chips { display: flex; flex-wrap: wrap; gap: 6px; }
.fchip {
  font: inherit; font-size: 12.5px; font-weight: 550;
  display: inline-flex; align-items: center; gap: 6px;
  height: 28px; padding: 0 10px; border-radius: 999px; cursor: pointer;
  background: var(--surface); color: var(--text-2); border: 1px solid var(--border);
  transition: background 0.12s, border-color 0.12s;
}
.fchip:hover { border-color: var(--text-4); }
.fchip.on { background: var(--accent-soft); border-color: var(--accent-border); color: var(--accent-text); }
.fchip:focus-visible { outline: none; box-shadow: var(--ring); }
.count { font-size: 11px; font-weight: 600; color: var(--text-4); font-variant-numeric: tabular-nums; }
.table-scroll { overflow-x: auto; }
.compact td { padding: 9px 16px; font-size: 13.5px; }
.right { text-align: right !important; }
.who { min-width: 180px; }
.addr { max-width: 240px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.err { max-width: 280px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.empty { text-align: center; padding: 32px 16px !important; }
.st { display: inline-flex; align-items: center; gap: 7px; font-weight: 600; }
.out-sent { color: var(--success); }
.out-failed { color: var(--danger); }
.out-unknown { color: var(--st-unknown); }
.out-queued, .out-sending { color: var(--info); }
.out-skipped_no_consent { color: var(--warn); }
.ch-sms { background: var(--var-name-soft); color: var(--var-name); border-color: var(--var-name-border); }
.ch-email { background: var(--var-city-soft); color: var(--var-city); border-color: var(--var-city-border); }
</style>
