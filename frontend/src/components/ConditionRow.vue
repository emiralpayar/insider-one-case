<script setup lang="ts">
// One editable segment condition: field / operator / value, all chosen from
// the whitelist (lib/rule.ts mirrors segment.whitelist).
import { computed } from 'vue'
import { CITIES, FIELDS, LANGUAGES, OP_LABELS, defaultCondition, describeCondition } from '../lib/rule'
import type { Condition } from '../types'
import Icon from './Icon.vue'

const props = defineProps<{ condition: Condition; errors: string[]; disabled?: boolean }>()
const emit = defineEmits<{ update: [c: Condition]; remove: [] }>()

const spec = computed(() => FIELDS[props.condition.field])
const cities = computed(() => (Array.isArray(props.condition.value) ? (props.condition.value as string[]) : []))
const remaining = computed(() => CITIES.filter((c) => !cities.value.includes(c)))

function setField(field: string) {
  emit('update', defaultCondition(field))
}
function setOp(op: string) {
  emit('update', { ...props.condition, op })
}
function setValue(value: unknown) {
  emit('update', { ...props.condition, value })
}
function addCity(e: Event) {
  const sel = e.target as HTMLSelectElement
  if (sel.value) setValue([...cities.value, sel.value])
  sel.value = ''
}
function removeCity(c: string) {
  setValue(cities.value.filter((x) => x !== c))
}
function setInt(e: Event) {
  const raw = (e.target as HTMLInputElement).value
  setValue(raw === '' ? null : Number(raw))
}
</script>

<template>
  <div class="cond" :class="{ 'has-error': errors.length }">
    <div class="cond-controls">
      <select
        class="select cond-field"
        :value="condition.field"
        :disabled="disabled"
        aria-label="Alan"
        @change="setField(($event.target as HTMLSelectElement).value)"
      >
        <option v-if="!spec" :value="condition.field">{{ condition.field }} (geçersiz)</option>
        <option v-for="(s, key) in FIELDS" :key="key" :value="key">{{ s.label }}</option>
      </select>

      <!-- A choice of one is not a choice: show it as a label. -->
      <span v-if="spec && spec.ops.length === 1" class="static-value cond-op" aria-label="Operatör" :title="'Bu alan için tek operatör'">
        {{ OP_LABELS[spec.ops[0]] ?? spec.ops[0] }}
      </span>
      <select
        v-else-if="spec"
        class="select cond-op"
        :value="condition.op"
        :disabled="disabled"
        aria-label="Operatör"
        @change="setOp(($event.target as HTMLSelectElement).value)"
      >
        <option v-for="op in spec.ops" :key="op" :value="op">{{ OP_LABELS[op] ?? op }}</option>
      </select>

      <div v-if="spec" class="cond-value">
        <template v-if="spec.kind === 'cities'">
          <div class="city-box">
            <span v-for="c in cities" :key="c" class="chip">
              {{ c }}
              <button type="button" :disabled="disabled" :aria-label="c + ' kaldır'" @click="removeCity(c)">×</button>
            </span>
            <button
              v-if="remaining.length === 1"
              type="button"
              class="btn btn-sm btn-ghost city-add-one"
              :disabled="disabled"
              @click="setValue([...cities, remaining[0]])"
            >
              <Icon name="plus" :size="13" /> {{ remaining[0] }}
            </button>
            <select v-else-if="remaining.length" class="select city-add" :disabled="disabled" aria-label="Şehir ekle" @change="addCity">
              <option value="">+ Şehir ekle</option>
              <option v-for="c in remaining" :key="c" :value="c">{{ c }}</option>
            </select>
          </div>
        </template>
        <select
          v-else-if="spec.kind === 'language'"
          class="select"
          :value="condition.value"
          :disabled="disabled"
          aria-label="Dil"
          @change="setValue(($event.target as HTMLSelectElement).value)"
        >
          <option v-for="(label, code) in LANGUAGES" :key="code" :value="code">{{ label }}</option>
        </select>
        <div v-else class="int-input">
          <input
            class="input"
            type="number"
            :min="spec.min"
            :max="spec.max"
            step="1"
            :value="condition.value ?? ''"
            :disabled="disabled"
            aria-label="Değer"
            @input="setInt"
          />
          <span class="muted">{{ spec.unit }}</span>
        </div>
      </div>

      <button type="button" class="btn btn-ghost btn-icon" :disabled="disabled" title="Koşulu kaldır" aria-label="Koşulu kaldır" @click="emit('remove')">
        <Icon name="trash" :size="15" />
      </button>
    </div>
    <div class="cond-foot">
      <span v-if="errors.length" class="field-error"><Icon name="alert" :size="13" style="margin-top: 2px" /> {{ errors.join(' · ') }}</span>
      <span v-else class="muted small cond-desc"><Icon name="check" :size="12" :stroke="2.5" /> {{ describeCondition(condition) }}</span>
    </div>
  </div>
</template>

<style scoped>
.cond {
  padding: 12px 14px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface-2);
  transition: border-color 0.12s, background 0.12s;
}
.cond:hover { border-color: var(--border-strong); }
.cond.has-error { border-color: var(--danger-border); background: var(--danger-soft); }
.cond-controls { display: flex; gap: 8px; align-items: flex-start; flex-wrap: wrap; }
.cond-field { width: 180px; }
.cond-op { width: 190px; }
span.cond-op { justify-content: flex-start; }
.cond-value { flex: 1; min-width: 200px; }
.city-box { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; min-height: 36px; }
.city-add { width: 160px; height: 30px; padding: 4px 28px 4px 10px; font-size: 13px; }
.city-add-one { border: 1px dashed var(--border-strong); }
.int-input { display: flex; align-items: center; gap: 8px; }
.int-input .input { width: 140px; }
.cond-foot { margin-top: 8px; padding-left: 2px; }
.cond-desc { display: inline-flex; align-items: center; gap: 6px; }
.cond-desc .icon { color: var(--success); }
@media (max-width: 640px) {
  .cond-field, .cond-op { width: 100%; }
}
</style>
