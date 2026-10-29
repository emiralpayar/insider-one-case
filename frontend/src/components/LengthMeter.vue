<script setup lang="ts">
// Worst-case length bar: "142 / 160 karakter".
import { computed } from 'vue'

const props = defineProps<{ length: number; limit: number; label?: string }>()
const ratio = computed(() => Math.min(1, props.length / props.limit))
const level = computed(() => (props.length > props.limit ? 'over' : props.length > props.limit * 0.9 ? 'warn' : ''))
</script>

<template>
  <div class="meter" :title="'Değişkenler en uzun isim ve şehirle doldurulduğunda ölçülen uzunluk'">
    <span class="count" :class="level">{{ length }} / {{ limit }}</span>
    <span>{{ label ?? 'karakter (en kötü durum)' }}</span>
    <div class="meter-bar"><div class="meter-fill" :class="level" :style="{ width: ratio * 100 + '%' }" /></div>
  </div>
</template>
