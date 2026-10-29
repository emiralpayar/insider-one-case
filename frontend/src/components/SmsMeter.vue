<script setup lang="ts">
// Live SMS counter: worst-case length /160, GSM-7 vs UCS-2, segment count,
// and which characters forced Unicode. Same math as the backend (lib/sms.ts).
import { computed } from 'vue'
import { num } from '../lib/format'
import { render, smsStats, worstCaseLength } from '../lib/sms'
import LengthMeter from './LengthMeter.vue'

const props = defineProps<{ text: string; longest: Record<string, string>; recipients: number }>()

const length = computed(() => worstCaseLength(props.text, props.longest))
const stats = computed(() => smsStats(render(props.text, props.longest)))
const shown = (c: string) => (c === '\n' ? '↵' : c === ' ' ? '␠' : c)
</script>

<template>
  <div class="stack-sm">
    <LengthMeter :length="length" :limit="160" />
    <div class="row small" style="gap: 8px">
      <span class="badge" :class="stats.encoding === 'UCS-2' ? 'badge-warn' : 'badge-success'">{{ stats.encoding }}</span>
      <span><strong>{{ stats.segments }}</strong> SMS parçası</span>
      <span v-if="recipients > 0 && stats.segments > 0" class="muted">
        · {{ num(recipients) }} alıcı × {{ stats.segments }} = {{ num(recipients * stats.segments) }} faturalı SMS
      </span>
    </div>
    <div v-if="stats.encoding === 'UCS-2'" class="alert alert-warn">
      <span class="alert-icon">!</span>
      <div class="alert-body">
        <div>
          Türkçe karakter ({{ stats.nonGsmChars.slice(0, 3).join(', ') }}{{ stats.nonGsmChars.length > 3 ? '…' : '' }})
          nedeniyle Unicode — <strong>{{ stats.segments }} SMS</strong> olarak faturalanır.
        </div>
        <div class="small">
          GSM-7 dışı karakterler:
          <span v-for="c in stats.nonGsmChars" :key="c" class="nonGsm">{{ shown(c) }}</span>
          · Unicode'da tek parça 70, çok parçada parça başına 67 karakter.
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.nonGsm {
  display: inline-block;
  min-width: 20px;
  text-align: center;
  margin: 0 2px;
  padding: 0 4px;
  border-radius: 4px;
  background: #fff;
  border: 1px solid var(--warn-border);
  font-family: var(--mono);
}
</style>
