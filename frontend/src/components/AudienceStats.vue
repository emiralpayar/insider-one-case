<script setup lang="ts">
// Audience counts (KPI tiles), warning badges and a few example users.
import { num, pct, warningLevel, WARNING_LABEL } from '../lib/format'
import type { PreviewResult } from '../types'
import Icon from './Icon.vue'

defineProps<{ preview: PreviewResult; stale?: boolean; loading?: boolean }>()
const share = (part: number, whole: number) => (whole > 0 ? Math.min(100, (part / whole) * 100) : 0)
</script>

<template>
  <div class="stack-sm" :class="{ stale }">
    <div v-if="loading || preview.warnings.length" class="row" style="gap: 6px">
      <span v-if="loading" class="row muted small" style="gap: 6px"><span class="spinner" style="width: 12px; height: 12px" /> güncelleniyor</span>
      <span
        v-for="w in preview.warnings"
        :key="w.code"
        class="badge"
        :class="'badge-' + warningLevel(w.code)"
      >{{ WARNING_LABEL[w.code] ?? w.code }}</span>
    </div>
    <div class="stat total-tile" :class="{ empty: preview.total === 0 }">
      <div class="stat-head">
        <span class="stat-icon" :class="preview.total === 0 ? 'red' : ''"><Icon name="target" :size="15" /></span>
        <span class="stat-label">Toplam eşleşen</span>
      </div>
      <div class="stat-value" :style="preview.total === 0 ? 'color: var(--danger)' : ''">
        {{ num(preview.total) }}<span class="unit">kişi</span>
      </div>
      <div class="stat-sub">{{ pct(preview.total, preview.all_users) }} / {{ num(preview.all_users) }} kullanıcı</div>
      <div class="stat-bar"><div :style="{ width: share(preview.total, preview.all_users) + '%' }" /></div>
    </div>
    <div class="grid-2 pair" style="gap: 8px">
      <div class="stat">
        <div class="stat-head">
          <span class="stat-icon violet"><Icon name="sms" :size="14" /></span>
          <span class="stat-label">SMS izinli</span>
        </div>
        <div class="stat-value small-value">{{ num(preview.sms_eligible) }}</div>
        <div class="stat-sub">{{ pct(preview.sms_eligible, preview.total) }} kitlenin</div>
      </div>
      <div class="stat">
        <div class="stat-head">
          <span class="stat-icon teal"><Icon name="mail" :size="14" /></span>
          <span class="stat-label">E-posta izinli</span>
        </div>
        <div class="stat-value small-value">{{ num(preview.email_eligible) }}</div>
        <div class="stat-sub">{{ pct(preview.email_eligible, preview.total) }} kitlenin</div>
      </div>
    </div>
    <div v-if="preview.sample.length" class="samples">
      <div class="section-label" style="margin-bottom: 6px">Örnek kişiler</div>
      <div class="sample-list">
        <span v-for="u in preview.sample" :key="u.id" class="sample">
          <span class="sample-avatar">{{ (u.first_name || '?').slice(0, 1) }}</span>
          <span>{{ u.first_name || '(isimsiz)' }}</span>
          <span class="muted">· {{ u.city }}</span>
        </span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.total-tile { background: linear-gradient(180deg, #fff, var(--surface-2)); }
.total-tile.empty { border-color: var(--danger-border); background: var(--danger-soft); }
.total-tile.empty .stat-bar > div { background: var(--danger); }
.small-value { font-size: 20px; }
.pair { grid-template-columns: 1fr 1fr; }
.sample-list { display: flex; flex-wrap: wrap; gap: 6px; }
.sample { display: inline-flex; align-items: center; gap: 6px; height: 26px; padding: 0 9px 0 3px; border-radius: 999px; background: var(--surface-3); font-size: 12.5px; }
.sample-avatar { width: 20px; height: 20px; border-radius: 50%; display: grid; place-items: center; background: var(--surface); border: 1px solid var(--border); font-size: 10.5px; font-weight: 700; color: var(--text-2); }
</style>
