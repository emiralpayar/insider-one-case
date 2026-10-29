<script setup lang="ts">
// İçerik adımı: streamed generation (SSE), repair notices, editable fields
// with live meters, rendered previews, per-channel regeneration and save.
// Subcomponents: SmsMeter, LengthMeter, MessagePreview.
import { computed, onUnmounted, ref, watch } from 'vue'
import { api, ApiError, streamContent } from '../api'
import { errorText, errorsByPath } from '../lib/errors'
import { applyPartial, cloneContent, sameContent } from '../lib/partial'
import { WORST_CASE_FIRST_NAME, worstCaseCity } from '../lib/rule'
import { placeholderIssues, worstCaseLength } from '../lib/sms'
import { appState } from '../store'
import type { Campaign, Channel, Content, PreviewResult, VError } from '../types'
import LengthMeter from './LengthMeter.vue'
import MessagePreview from './MessagePreview.vue'
import SmsMeter from './SmsMeter.vue'

const props = defineProps<{ campaign: Campaign; preview: PreviewResult | null }>()
const emit = defineEmits<{ saved: [c: Content]; dirty: [d: boolean]; back: []; next: []; locked: [] }>()

const draft = ref<Content>(cloneContent(props.campaign.content))
const streaming = ref<null | 'all' | Channel>(null)
const repairs = ref<string[]>([])
const streamError = ref<unknown>(null)
const issues = ref<VError[]>([])
const unsavedFromClaude = ref(false)
const saving = ref(false)
const saveError = ref<unknown>(null)
const savedAt = ref<Date | null>(null)
const manual = ref(false)
let controller: AbortController | null = null

const channels = computed(() => props.campaign.channels)
const has = (ch: Channel) => channels.value.includes(ch)
const hasSaved = computed(() => !!props.campaign.content)
const llmOff = computed(() => appState.config?.llm_configured === false)
const dirty = computed(() => !sameContent(draft.value, props.campaign.content ?? null) && !!(draft.value.sms || draft.value.email.subject || draft.value.email.body))
watch(dirty, (d) => emit('dirty', d), { immediate: true })

const longest = computed(() => ({ first_name: WORST_CASE_FIRST_NAME, city: worstCaseCity(props.campaign.rule) }))
const subjectLen = computed(() => worstCaseLength(draft.value.email.subject, longest.value))
const sample = computed(() => props.preview?.sample.find((u) => u.first_name) ?? props.preview?.sample[0] ?? null)

const serverErrors = computed(() => {
  const byPath: Record<string, string[]> = {}
  for (const e of issues.value) (byPath[e.path] ??= []).push(e.message)
  return byPath
})
function fieldErrors(path: string, text: string): string[] {
  return [...new Set([...(serverErrors.value[path] ?? []), ...placeholderIssues(text)])]
}

// ---- generation ----
async function generate(which: 'all' | Channel) {
  if (streaming.value) return
  if (dirty.value && !window.confirm('Kaydedilmemiş düzenlemeleriniz Claude\'un yeni çıktısıyla değişecek. Devam edilsin mi?')) return
  const gen: Channel[] = which === 'all' ? [...channels.value] : [which]
  const base = cloneContent(draft.value)
  streaming.value = which
  repairs.value = []
  streamError.value = null
  issues.value = []
  saveError.value = null
  controller = new AbortController()
  try {
    const done = await streamContent(
      props.campaign.id,
      which,
      {
        partial: (p) => (draft.value = applyPartial(base, p, gen)),
        repair: (errs) => (repairs.value = errs.map((e) => e.message)),
      },
      controller.signal,
    )
    draft.value = cloneContent(done.content)
    issues.value = done.issues
    unsavedFromClaude.value = !done.saved
    if (done.saved) {
      emit('saved', cloneContent(done.content))
      savedAt.value = new Date()
    }
  } catch (e) {
    if (controller?.signal.aborted) return
    if (e instanceof ApiError && e.code === 'campaign_locked') emit('locked')
    streamError.value = e
    if (e instanceof ApiError) issues.value = e.fieldErrors
  } finally {
    streaming.value = null
    controller = null
  }
}
onUnmounted(() => controller?.abort())

// ---- save ----
async function save(): Promise<boolean> {
  saving.value = true
  saveError.value = null
  try {
    const r = await api.putContent(props.campaign.id, draft.value)
    issues.value = []
    unsavedFromClaude.value = false
    emit('saved', cloneContent(r.content))
    savedAt.value = new Date()
    return true
  } catch (e) {
    if (e instanceof ApiError && e.code === 'campaign_locked') emit('locked')
    saveError.value = e
    issues.value = e instanceof ApiError ? e.fieldErrors : []
    return false
  } finally {
    saving.value = false
  }
}
async function next() {
  if ((dirty.value || unsavedFromClaude.value) && !(await save())) return
  emit('next')
}
const generalSaveError = computed(() => saveError.value && Object.keys(errorsByPath(saveError.value)).length === 0)
</script>

<template>
  <section class="stack">
    <!-- first generation -->
    <div v-if="!hasSaved && !streaming && !manual && !draft.sms && !draft.email.subject" class="card empty-state">
      <h2>Mesajları Claude yazsın</h2>
      <p class="muted">
        {{ channels.map((c) => (c === 'sms' ? 'SMS' : 'e-posta')).join(' ve ') }} içeriği brief'e ve kitleye göre üretilir,
        canlı olarak burada belirir ve kanal limitlerine göre doğrulanır.
      </p>
      <p style="margin-top: 16px">
        <button class="btn btn-primary btn-lg" type="button" :disabled="llmOff" @click="generate('all')">✦ Claude ile içerik üret</button>
      </p>
      <p style="margin-top: 8px">
        <button class="btn btn-ghost btn-sm" type="button" @click="manual = true">ya da içeriği elle yazın</button>
      </p>
      <p v-if="llmOff" class="field-error" style="margin-top: 8px">Claude API anahtarı tanımlı değil — içerik üretilemez.</p>
    </div>

    <template v-else>
      <!-- stream status -->
      <div v-if="streaming" class="alert alert-info" role="status">
        <span class="spinner" />
        <div class="alert-body">
          <strong>Claude yazıyor…</strong>
          <span class="muted">({{ streaming === 'all' ? 'tüm kanallar' : streaming === 'sms' ? 'yalnızca SMS' : 'yalnızca e-posta' }})</span>
          <div v-if="repairs.length" class="repair">Claude düzeltiyor: {{ repairs.join(' · ') }}</div>
        </div>
      </div>
      <div v-else-if="streamError" class="alert alert-danger" role="alert">
        <span class="alert-icon">!</span>
        <div class="alert-body">
          <div class="alert-title">İçerik üretilemedi</div>
          <div>{{ errorText(streamError) }}</div>
          <div v-if="issues.length" class="small">Son çıktıdaki sorunlar alanların altında işaretlendi; elle düzeltip kaydedebilirsiniz.</div>
        </div>
      </div>
      <div v-else-if="unsavedFromClaude && issues.length" class="alert alert-warn" role="alert">
        <span class="alert-icon">!</span>
        <div class="alert-body">
          <div class="alert-title">Claude'un çıktısı doğrulamadan geçmedi — kaydedilmedi</div>
          <div>İşaretli alanları düzeltip kaydedin.</div>
        </div>
      </div>
      <div v-else-if="repairs.length" class="alert alert-info">
        <span class="alert-icon">i</span>
        <div class="alert-body">Claude ilk çıktısını düzeltti: {{ repairs.join(' · ') }}</div>
      </div>

      <div class="grid-2 editors">
        <!-- SMS -->
        <div class="card stack" :class="{ off: !has('sms') }">
          <div class="card-title" style="margin: 0">
            <h2>SMS</h2>
            <span v-if="!has('sms')" class="badge">Kanal kapalı</span>
            <button v-else class="btn btn-sm" type="button" :disabled="!!streaming || !hasSaved || llmOff" title="Yalnızca SMS'i yeniden üret" @click="generate('sms')">
              <span v-if="streaming === 'sms'" class="spinner" /> ↻ Yeniden üret
            </button>
          </div>
          <div class="field" :class="{ 'has-error': fieldErrors('sms', draft.sms).length }">
            <textarea v-model="draft.sms" class="textarea" rows="5" :readonly="!!streaming" aria-label="SMS metni" />
            <div v-for="m in fieldErrors('sms', draft.sms)" :key="m" class="field-error">{{ m }}</div>
          </div>
          <SmsMeter :text="draft.sms" :longest="longest" :recipients="has('sms') ? preview?.sms_eligible ?? 0 : 0" />
        </div>

        <!-- Email -->
        <div class="card stack" :class="{ off: !has('email') }">
          <div class="card-title" style="margin: 0">
            <h2>E-posta</h2>
            <span v-if="!has('email')" class="badge">Kanal kapalı</span>
            <button v-else class="btn btn-sm" type="button" :disabled="!!streaming || !hasSaved || llmOff" title="Yalnızca e-postayı yeniden üret" @click="generate('email')">
              <span v-if="streaming === 'email'" class="spinner" /> ↻ Yeniden üret
            </button>
          </div>
          <div class="field" :class="{ 'has-error': fieldErrors('email.subject', draft.email.subject).length }">
            <label class="field-label" for="subject">Konu</label>
            <input id="subject" v-model="draft.email.subject" class="input" :readonly="!!streaming" />
            <div v-for="m in fieldErrors('email.subject', draft.email.subject)" :key="m" class="field-error">{{ m }}</div>
            <LengthMeter :length="subjectLen" :limit="80" />
          </div>
          <div class="field" :class="{ 'has-error': fieldErrors('email.preheader', draft.email.preheader).length }">
            <label class="field-label" for="preheader">Ön başlık <span class="muted small">(isteğe bağlı)</span></label>
            <input id="preheader" v-model="draft.email.preheader" class="input" :readonly="!!streaming" />
            <div v-for="m in fieldErrors('email.preheader', draft.email.preheader)" :key="m" class="field-error">{{ m }}</div>
          </div>
          <div class="field" :class="{ 'has-error': fieldErrors('email.body', draft.email.body).length }">
            <label class="field-label" for="body">Gövde</label>
            <textarea id="body" v-model="draft.email.body" class="textarea" rows="7" :readonly="!!streaming" />
            <div v-for="m in fieldErrors('email.body', draft.email.body)" :key="m" class="field-error">{{ m }}</div>
          </div>
        </div>
      </div>

      <div class="card">
        <div class="card-title">
          <h2>Önizleme</h2>
          <span class="muted small">Gerçek bir alıcıyla ve ismi boş bir alıcıyla</span>
        </div>
        <MessagePreview :content="draft" :sample="sample" :channels="channels" :streaming="!!streaming" />
      </div>

      <div v-if="generalSaveError" class="alert alert-danger">
        <span class="alert-icon">!</span>
        <div class="alert-body">{{ errorText(saveError) }}</div>
      </div>

      <div class="footer-bar">
        <button class="btn" type="button" :disabled="!!streaming" @click="emit('back')">← Kitle</button>
        <div class="row">
          <span v-if="dirty || unsavedFromClaude" class="badge badge-warn">Kaydedilmemiş değişiklik</span>
          <span v-else-if="savedAt" class="muted small">Kaydedildi · {{ savedAt.toLocaleTimeString('tr-TR') }}</span>
          <button class="btn" type="button" :disabled="!!streaming || saving || !(dirty || unsavedFromClaude)" @click="save">Kaydet</button>
          <button class="btn btn-primary btn-lg" type="button" :disabled="!!streaming || saving || (!hasSaved && !dirty && !unsavedFromClaude)" @click="next">
            <span v-if="saving" class="spinner" /> Onaya geç →
          </button>
        </div>
      </div>
    </template>
  </section>
</template>

<style scoped>
.off { opacity: 0.6; }
.repair { margin-top: 4px; font-weight: 600; color: #8a5300; }
.editors { align-items: start; }
</style>
