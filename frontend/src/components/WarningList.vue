<script setup lang="ts">
// Audience warnings as full-width alerts (empty / large / unmapped / mixed language).
import { warningLevel, warningText, WARNING_LABEL } from '../lib/format'
import type { PreviewResult } from '../types'

defineProps<{ preview: PreviewResult; unmapped: string[] }>()
const ICON = { danger: '!', warn: '!', info: 'i' }
</script>

<template>
  <div v-if="preview.warnings.length" class="stack-sm">
    <div v-for="w in preview.warnings" :key="w.code" class="alert" :class="'alert-' + warningLevel(w.code)" role="alert">
      <span class="alert-icon">{{ ICON[warningLevel(w.code)] }}</span>
      <div class="alert-body">
        <div class="alert-title">{{ WARNING_LABEL[w.code] ?? w.code }}</div>
        <div>{{ warningText(w, preview.english_speakers, preview.total, preview.all_users) }}</div>
        <ul v-if="w.code === 'unmapped' && unmapped.length">
          <li v-for="u in unmapped" :key="u"><strong>{{ u }}</strong></li>
        </ul>
        <div v-if="w.code === 'unmapped'" class="small" style="margin-top: 4px">
          Bu istekler kurala yansıtılmadı. Gerekirse koşulu elle ekleyin ya da Claude'a düzeltme talimatı verin.
        </div>
      </div>
    </div>
  </div>
</template>
