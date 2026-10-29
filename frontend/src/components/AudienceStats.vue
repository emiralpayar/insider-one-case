<script setup lang="ts">
// Audience counts, warning badges and a few example users.
import { num, pct, warningLevel, WARNING_LABEL } from '../lib/format'
import type { PreviewResult } from '../types'

defineProps<{ preview: PreviewResult; stale?: boolean; loading?: boolean }>()
</script>

<template>
  <div class="stack-sm" :class="{ stale }">
    <div class="row-between">
      <div class="section-label" style="margin: 0">Kitle büyüklüğü</div>
      <div class="row" style="gap: 6px">
        <span v-if="loading" class="row muted small" style="gap: 6px"><span class="spinner" /> güncelleniyor</span>
        <span
          v-for="w in preview.warnings"
          :key="w.code"
          class="badge"
          :class="'badge-' + warningLevel(w.code)"
        >{{ WARNING_LABEL[w.code] ?? w.code }}</span>
      </div>
    </div>
    <div class="stats">
      <div class="stat">
        <div class="stat-label">Toplam eşleşen</div>
        <div class="stat-value" :style="preview.total === 0 ? 'color: var(--danger)' : ''">{{ num(preview.total) }}</div>
        <div class="stat-sub">{{ pct(preview.total, preview.all_users) }} / {{ num(preview.all_users) }} kullanıcı</div>
      </div>
      <div class="stat">
        <div class="stat-label">SMS izinli</div>
        <div class="stat-value">{{ num(preview.sms_eligible) }}</div>
        <div class="stat-sub">SMS bu kişilere gider</div>
      </div>
      <div class="stat">
        <div class="stat-label">E-posta izinli</div>
        <div class="stat-value">{{ num(preview.email_eligible) }}</div>
        <div class="stat-sub">E-posta bu kişilere gider</div>
      </div>
    </div>
    <div v-if="preview.sample.length" class="small muted">
      Örnek kişiler:
      <span v-for="(u, i) in preview.sample" :key="u.id">
        {{ u.first_name || '(isimsiz)' }} · {{ u.city }}<template v-if="i < preview.sample.length - 1">, </template>
      </span>
    </div>
  </div>
</template>
