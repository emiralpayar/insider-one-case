<script setup lang="ts">
// Horizontal numbered stepper (Brief → Kitle → İçerik → Onay → Gönderim).
import Icon from './Icon.vue'

export interface StepDef {
  key: string
  title: string
  sub: string
}

defineProps<{ steps: StepDef[]; current: string; currentIndex: number; reachable: (key: string) => boolean }>()
const emit = defineEmits<{ go: [key: string] }>()
</script>

<template>
  <nav class="stepper" aria-label="Adımlar">
    <template v-for="(s, i) in steps" :key="s.key">
      <span v-if="i > 0" class="step-line" :class="{ done: i <= currentIndex }" aria-hidden="true" />
      <button
        type="button"
        class="step"
        :class="{ active: s.key === current, reachable: reachable(s.key) && s.key !== current, done: i < currentIndex }"
        :disabled="!reachable(s.key)"
        :aria-current="s.key === current ? 'step' : undefined"
        @click="emit('go', s.key)"
      >
        <span class="step-num"><Icon v-if="i < currentIndex" name="check" :size="15" :stroke="3" /><template v-else>{{ i + 1 }}</template></span>
        <span class="step-text">
          <span class="step-title">{{ s.title }}</span>
          <span class="step-sub">{{ s.sub }}</span>
        </span>
      </button>
    </template>
  </nav>
</template>
