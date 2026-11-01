<script setup lang="ts">
// "Kitleyi gör": every user matching the rule, 50 per page from
// POST /segments/members, with an optional name search. Side drawer; Esc or
// the backdrop closes it.
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { api } from '../api'
import { errorText } from '../lib/errors'
import { num } from '../lib/format'
import { fullName, money } from '../lib/lists'
import type { MemberPage, Rule } from '../types'
import Icon from './Icon.vue'
import Pager from './Pager.vue'

const props = defineProps<{ rule: Rule }>()
const emit = defineEmits<{ close: [] }>()

const page = ref(1)
const search = ref('')
const data = ref<MemberPage | null>(null)
const loading = ref(false)
const error = ref('')
const closeBtn = ref<HTMLButtonElement | null>(null)
let seq = 0
let debounce: ReturnType<typeof setTimeout> | undefined

async function load() {
  const my = ++seq
  loading.value = true
  error.value = ''
  try {
    const r = await api.members(props.rule, page.value, search.value)
    if (my === seq) data.value = r
  } catch (e) {
    if (my === seq) error.value = errorText(e)
  } finally {
    if (my === seq) loading.value = false
  }
}

watch(page, load)
watch(search, () => {
  clearTimeout(debounce)
  debounce = setTimeout(() => {
    if (page.value === 1) load()
    else page.value = 1
  }, 300)
})

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}
let prevOverflow = ''
onMounted(() => {
  document.addEventListener('keydown', onKey)
  prevOverflow = document.body.style.overflow
  document.body.style.overflow = 'hidden'
  closeBtn.value?.focus()
  load()
})
onUnmounted(() => {
  clearTimeout(debounce)
  document.removeEventListener('keydown', onKey)
  document.body.style.overflow = prevOverflow
})

const consentTitle = (v: boolean | null) => (v === null ? 'Bilinmiyor' : v ? 'İzin var' : 'İzin yok')
</script>

<template>
  <Teleport to="body">
    <div class="drawer-backdrop" @click.self="emit('close')">
      <aside class="drawer" role="dialog" aria-modal="true" aria-labelledby="audience-drawer-title">
        <div class="card-head">
          <span class="card-icon teal"><Icon name="users" :size="17" /></span>
          <h2 id="audience-drawer-title">
            Kitle
            <span class="sub">
              <template v-if="data">{{ num(data.total) }} kişi{{ search.trim() ? ' (aramayla eşleşen)' : '' }}</template>
              <template v-else>Yükleniyor…</template>
            </span>
          </h2>
          <button ref="closeBtn" type="button" class="btn btn-ghost btn-icon" aria-label="Kapat" @click="emit('close')"><Icon name="x" :size="16" /></button>
        </div>

        <div class="drawer-tools">
          <label class="search">
            <Icon name="search" :size="14" />
            <input v-model="search" class="input" type="search" placeholder="İsimle ara…" aria-label="İsimle ara" maxlength="100" />
          </label>
        </div>

        <div v-if="error" class="alert alert-danger" style="margin: 12px var(--space-5)">
          <span class="alert-icon">!</span>
          <div class="alert-body">{{ error }} <button type="button" class="btn btn-sm" @click="load">Tekrar dene</button></div>
        </div>

        <div class="drawer-body" :class="{ stale: loading && !!data }">
          <table class="table compact">
            <thead>
              <tr>
                <th>Ad</th>
                <th>Şehir</th>
                <th>Dil</th>
                <th class="center">SMS izni</th>
                <th class="center">E-posta izni</th>
                <th class="right">Sipariş</th>
                <th class="right">Toplam harcama</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="!data && loading">
                <td colspan="7"><span class="skeleton" /><span class="skeleton" style="width: 70%" /><span class="skeleton" style="width: 50%" /></td>
              </tr>
              <tr v-else-if="data && data.members.length === 0">
                <td colspan="7" class="muted empty">{{ search.trim() ? 'Aramayla eşleşen kişi yok.' : 'Bu kurala uyan kullanıcı yok.' }}</td>
              </tr>
              <tr v-for="m in data?.members ?? []" :key="m.id">
                <td>
                  <span :class="{ muted: !m.first_name && !m.last_name }">{{ fullName(m.first_name, m.last_name) }}</span>
                  <span class="muted tiny mono"> #{{ m.id }}</span>
                </td>
                <td>{{ m.city }}</td>
                <td><span class="badge">{{ m.language.toUpperCase() }}</span></td>
                <td class="center" :title="consentTitle(m.sms_consent)">
                  <Icon v-if="m.sms_consent" name="check" :size="15" class="yes" />
                  <Icon v-else-if="m.sms_consent === false" name="x" :size="15" class="no" />
                  <span v-else class="muted">—</span>
                </td>
                <td class="center" :title="consentTitle(m.email_consent)">
                  <Icon v-if="m.email_consent" name="check" :size="15" class="yes" />
                  <Icon v-else-if="m.email_consent === false" name="x" :size="15" class="no" />
                  <span v-else class="muted">—</span>
                </td>
                <td class="right tabular">{{ num(m.order_count) }}</td>
                <td class="right tabular">{{ money(m.total_spent) }}</td>
              </tr>
            </tbody>
          </table>
        </div>

        <Pager v-if="data" v-model:page="page" :per-page="data.per_page" :total="data.total" :loading="loading" />
      </aside>
    </div>
  </Teleport>
</template>

<style scoped>
.drawer-backdrop { position: fixed; inset: 0; z-index: 50; background: rgba(15, 23, 42, 0.4); display: flex; justify-content: flex-end; animation: fade 0.15s ease; }
.drawer { width: min(920px, 100vw); height: 100%; background: var(--surface); box-shadow: var(--shadow-lg); display: flex; flex-direction: column; animation: slide 0.2s ease; }
.drawer-tools { padding: 12px var(--space-5); border-bottom: 1px solid var(--border); }
.search { display: flex; align-items: center; gap: 8px; color: var(--text-3); max-width: 340px; }
.search .input { flex: 1; }
.drawer-body { flex: 1; overflow: auto; overscroll-behavior: contain; }
.compact th { position: sticky; top: 0; z-index: 1; }
.compact td { padding: 9px 16px; font-size: 13.5px; }
.center { text-align: center !important; }
.right { text-align: right !important; }
.yes { color: var(--success); }
.no { color: var(--text-4); }
.empty { text-align: center; padding: 32px 16px !important; }
@keyframes fade { from { opacity: 0; } }
@keyframes slide { from { transform: translateX(24px); opacity: 0.6; } }
</style>
