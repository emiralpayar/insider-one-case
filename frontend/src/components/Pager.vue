<script setup lang="ts">
// Previous/next pager for the server-paginated lists.
import { computed } from 'vue'
import { pageCount, rangeText } from '../lib/lists'
import Icon from './Icon.vue'

const props = defineProps<{ page: number; perPage: number; total: number; loading?: boolean }>()
const emit = defineEmits<{ 'update:page': [p: number] }>()
const pages = computed(() => pageCount(props.total, props.perPage))
</script>

<template>
  <div class="pager">
    <span class="muted small tabular">{{ rangeText(page, perPage, total) }}</span>
    <div class="row" style="gap: 6px">
      <span v-if="loading" class="spinner" style="width: 13px; height: 13px; color: var(--text-3)" />
      <button type="button" class="btn btn-sm" :disabled="page <= 1 || loading" aria-label="Önceki sayfa" @click="emit('update:page', page - 1)">
        <Icon name="arrow-left" :size="13" /> Önceki
      </button>
      <span class="small tabular page-no">{{ page }} / {{ pages }}</span>
      <button type="button" class="btn btn-sm" :disabled="page >= pages || loading" aria-label="Sonraki sayfa" @click="emit('update:page', page + 1)">
        Sonraki <Icon name="arrow-right" :size="13" />
      </button>
    </div>
  </div>
</template>

<style scoped>
.pager { display: flex; align-items: center; justify-content: space-between; gap: var(--space-3); flex-wrap: wrap; padding: 10px var(--space-5); border-top: 1px solid var(--border); background: var(--surface-2); }
.page-no { min-width: 56px; text-align: center; color: var(--text-2); }
</style>
