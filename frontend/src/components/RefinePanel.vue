<script setup lang="ts">
// "Doğal dille düzelt": instruction box, then old ↔ new rule side by side with
// the audience delta; the marketer accepts or rejects the proposal.
import { computed, ref } from 'vue'
import { errorText } from '../lib/errors'
import { num } from '../lib/format'
import { describeRule } from '../lib/rule'
import type { Rule, RuleResponse } from '../types'

const props = defineProps<{
  current: Rule
  proposal: RuleResponse | null
  busy: boolean
  error: unknown
  disabled?: boolean
}>()
const emit = defineEmits<{ refine: [instruction: string]; accept: []; reject: [] }>()

const instruction = ref('')

const oldLines = computed(() => describeRule(props.current))
const newLines = computed(() => (props.proposal ? describeRule(props.proposal.rule) : []))
const unchanged = computed(
  () => props.proposal && JSON.stringify(props.proposal.rule.conditions) === JSON.stringify(props.current.conditions),
)

function delta(a: number, b: number) {
  const d = b - a
  return d === 0 ? '±0' : (d > 0 ? '+' : '−') + num(Math.abs(d))
}
const rows = computed(() => {
  const p = props.proposal
  if (!p?.previous_preview) return []
  const o = p.previous_preview
  const n = p.preview
  return [
    { label: 'Toplam', old: o.total, new: n.total },
    { label: 'SMS izinli', old: o.sms_eligible, new: n.sms_eligible },
    { label: 'E-posta izinli', old: o.email_eligible, new: n.email_eligible },
  ]
})

function submit() {
  const t = instruction.value.trim()
  if (t && !props.busy) emit('refine', t)
}
function accept() {
  instruction.value = ''
  emit('accept')
}
</script>

<template>
  <div class="stack-sm">
    <div class="section-label">Doğal dille düzelt</div>
    <form class="row" @submit.prevent="submit">
      <input
        v-model="instruction"
        class="input"
        style="flex: 1; min-width: 220px"
        placeholder="Örn. Ankara'yı da ekle"
        :disabled="busy || disabled || !!proposal"
      />
      <button class="btn" type="submit" :disabled="busy || disabled || !!proposal || !instruction.trim()">
        <span v-if="busy" class="spinner" /> Claude'a sor
      </button>
    </form>
    <div v-if="error" class="alert alert-danger">
      <span class="alert-icon">!</span>
      <div class="alert-body">{{ errorText(error) }}</div>
    </div>

    <div v-if="proposal" class="proposal">
      <div class="row-between" style="margin-bottom: 12px">
        <strong>Claude'un önerisi</strong>
        <span v-if="unchanged" class="badge badge-warn">Koşullar değişmedi</span>
      </div>
      <div class="grid-2">
        <div class="side old">
          <div class="section-label">Mevcut kural</div>
          <ul>
            <li v-for="l in oldLines" :key="l" :class="{ removed: !newLines.includes(l) }">{{ l }}</li>
          </ul>
        </div>
        <div class="side new">
          <div class="section-label">Önerilen kural</div>
          <ul>
            <li v-for="l in newLines" :key="l" :class="{ added: !oldLines.includes(l) }">{{ l }}</li>
          </ul>
        </div>
      </div>
      <p class="small" style="margin-top: 10px">
        <span class="muted">Claude'un açıklaması:</span> {{ proposal.rule.explanation }}
      </p>
      <table class="delta">
        <tbody>
          <tr v-for="r in rows" :key="r.label">
            <td class="muted">{{ r.label }}</td>
            <td>{{ num(r.old) }}</td>
            <td class="muted">→</td>
            <td><strong>{{ num(r.new) }}</strong></td>
            <td :class="r.new > r.old ? 'up' : r.new < r.old ? 'down' : 'muted'">{{ delta(r.old, r.new) }}</td>
          </tr>
        </tbody>
      </table>
      <div class="row" style="justify-content: flex-end; margin-top: 12px">
        <button type="button" class="btn" :disabled="busy" @click="emit('reject')">Reddet</button>
        <button type="button" class="btn btn-primary" :disabled="busy" @click="accept">Kabul et</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.proposal { border: 1px solid #d6d4f7; border-radius: var(--radius); padding: 16px; background: #fbfbff; }
.side { border: 1px solid var(--border); border-radius: var(--radius-sm); padding: 12px; background: var(--surface); }
.side ul { margin: 0; padding-left: 18px; }
.side li { padding: 2px 0; }
.removed { color: var(--danger); text-decoration: line-through; }
.added { color: var(--success); font-weight: 600; }
.delta { margin-top: 10px; border-collapse: collapse; font-variant-numeric: tabular-nums; }
.delta td { padding: 3px 12px 3px 0; }
.up { color: var(--success); font-weight: 600; }
.down { color: var(--danger); font-weight: 600; }
</style>
