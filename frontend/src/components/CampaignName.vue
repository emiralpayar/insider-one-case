<script setup lang="ts">
// Campaign name as the page title, editable in place (PUT /campaigns/{id}/name).
// Enter saves, Escape cancels.
import { nextTick, ref } from 'vue'
import { api } from '../api'
import { errorText } from '../lib/errors'
import { NAME_MAX, nameError, normalizeName } from '../lib/name'
import type { Campaign } from '../types'
import Icon from './Icon.vue'

const props = defineProps<{ campaign: Campaign }>()
const emit = defineEmits<{ renamed: [c: Campaign] }>()

const editing = ref(false)
const value = ref('')
const error = ref('')
const saving = ref(false)
const input = ref<HTMLInputElement | null>(null)

async function start() {
  value.value = props.campaign.name
  error.value = ''
  editing.value = true
  await nextTick()
  input.value?.focus()
  input.value?.select()
}

function cancel() {
  editing.value = false
  error.value = ''
}

async function save() {
  if (saving.value) return
  const name = normalizeName(value.value)
  if (name === props.campaign.name) return cancel()
  const local = nameError(name)
  if (local) {
    error.value = local
    return
  }
  saving.value = true
  error.value = ''
  try {
    emit('renamed', await api.renameCampaign(props.campaign.id, name))
    editing.value = false
  } catch (e) {
    error.value = errorText(e)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div v-if="!editing" class="name-view">
    <h1 class="name-text">{{ campaign.name || 'İsimsiz kampanya' }}</h1>
    <button type="button" class="btn btn-ghost btn-icon btn-sm" title="Adı düzenle" aria-label="Kampanya adını düzenle" @click="start">
      <Icon name="edit" :size="15" />
    </button>
  </div>
  <form v-else class="name-edit" @submit.prevent="save">
    <div class="field" :class="{ 'has-error': !!error }">
      <div class="row" style="gap: 6px; flex-wrap: nowrap">
        <input
          ref="input"
          v-model="value"
          class="input name-input"
          :maxlength="NAME_MAX + 20"
          aria-label="Kampanya adı"
          :disabled="saving"
          @keydown.esc.prevent="cancel"
        />
        <button type="submit" class="btn btn-primary btn-sm" :disabled="saving">
          <span v-if="saving" class="spinner" style="width: 13px; height: 13px" /><Icon v-else name="check" :size="14" /> Kaydet
        </button>
        <button type="button" class="btn btn-ghost btn-sm" :disabled="saving" @click="cancel">Vazgeç</button>
      </div>
      <div v-if="error" class="field-error" role="alert">{{ error }}</div>
      <div v-else class="field-hint">Enter ile kaydedin, Esc ile vazgeçin · en fazla {{ NAME_MAX }} karakter</div>
    </div>
  </form>
</template>

<style scoped>
.name-view { display: flex; align-items: center; gap: 6px; min-width: 0; }
.name-text { min-width: 0; overflow-wrap: anywhere; }
.name-view .btn-icon { color: var(--text-3); flex: none; }
.name-edit { max-width: 640px; }
.name-input { font-size: 18px; font-weight: 650; height: 40px; min-width: 0; flex: 1; }
</style>
