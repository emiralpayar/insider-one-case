<script setup lang="ts">
// Kitle adımı: Claude's rule (explanation, conditions, unmapped), live audience
// preview with 400 ms debounce, natural-language refine and save.
// Subcomponents: ConditionRow, AudienceStats, WarningList, RefinePanel.
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { api, ApiError } from '../api'
import { errorText, errorsByPath } from '../lib/errors'
import { defaultCondition, validateLocal } from '../lib/rule'
import { appState } from '../store'
import type { Campaign, Condition, PreviewResult, Rule, RuleResponse } from '../types'
import AudienceStats from './AudienceStats.vue'
import ConditionRow from './ConditionRow.vue'
import RefinePanel from './RefinePanel.vue'
import WarningList from './WarningList.vue'

const props = defineProps<{ campaign: Campaign; preview: PreviewResult | null }>()
const emit = defineEmits<{ saved: [r: RuleResponse]; dirty: [d: boolean]; next: []; locked: [] }>()

const clone = (r: Rule): Rule => JSON.parse(JSON.stringify(r))

const draft = ref<Rule | null>(props.campaign.rule ? clone(props.campaign.rule) : null)
const livePreview = ref<PreviewResult | null>(props.preview)
const previewLoading = ref(false)
const serverErrors = ref<Record<string, string[]>>({})
const generating = ref(false)
const genError = ref<unknown>(null)
const saving = ref(false)
const saveError = ref<unknown>(null)
// The rule differs from what Claude produced (form edits, or saved that way:
// a hand-edited rule is saved with an empty explanation).
const handEdited = ref(!!props.campaign.rule && !props.campaign.rule.explanation)

const refineBusy = ref(false)
const refineError = ref<unknown>(null)
const proposal = ref<RuleResponse | null>(null)

const dirty = computed(() => !!draft.value && JSON.stringify(draft.value) !== JSON.stringify(props.campaign.rule))
watch(dirty, (d) => emit('dirty', d), { immediate: true })
// The parent may load the saved rule's preview after this step mounted.
watch(
  () => props.preview,
  (p) => {
    if (!dirty.value) livePreview.value = p
  },
)

const localErrors = computed(() => (draft.value ? errorsByPathList(validateLocal(draft.value)) : {}))
const allErrors = computed(() => ({ ...serverErrors.value, ...localErrors.value }))
const hasErrors = computed(() => Object.keys(allErrors.value).length > 0)
const ruleLevelErrors = computed(() =>
  Object.entries(allErrors.value).filter(([p]) => !p.startsWith('conditions[')).flatMap(([, m]) => m),
)
const condErrors = (i: number) =>
  Object.entries(allErrors.value).filter(([p]) => p === `conditions[${i}]` || p.startsWith(`conditions[${i}].`)).flatMap(([, m]) => m)

function errorsByPathList(list: { path: string; message: string }[]) {
  const out: Record<string, string[]> = {}
  for (const e of list) (out[e.path] ??= []).push(e.message)
  return out
}

// ---- generation ----
async function generate() {
  generating.value = true
  genError.value = null
  try {
    applySaved(await api.generateSegment(props.campaign.id), true)
  } catch (e) {
    if (e instanceof ApiError && e.code === 'campaign_locked') emit('locked')
    genError.value = e
  } finally {
    generating.value = false
  }
}

function buildManually() {
  genError.value = null
  draft.value = { audience: 'filtered', conditions: [defaultCondition('city')], unmapped: [], explanation: '' }
  handEdited.value = true
}

// fromClaude: the saved rule (and its explanation) came from the model.
function applySaved(r: RuleResponse, fromClaude: boolean) {
  emit('saved', r)
  if (fromClaude) handEdited.value = false
  draft.value = clone(r.rule)
  livePreview.value = r.preview
  serverErrors.value = {}
}

onMounted(() => {
  if (!props.campaign.rule && appState.config?.llm_configured !== false) generate()
})

// ---- live preview (debounced) ----
let timer: ReturnType<typeof setTimeout> | undefined
let seq = 0
watch(
  draft,
  (r) => {
    clearTimeout(timer)
    if (!r || !dirty.value) return
    serverErrors.value = {}
    if (Object.keys(localErrors.value).length) return
    timer = setTimeout(async () => {
      const my = ++seq
      previewLoading.value = true
      try {
        const pv = await api.preview(r)
        if (my === seq) livePreview.value = pv
      } catch (e) {
        if (my === seq) serverErrors.value = errorsByPath(e)
      } finally {
        if (my === seq) previewLoading.value = false
      }
    }, 400)
  },
  { deep: true },
)
onUnmounted(() => clearTimeout(timer))

// ---- form edits ----
function updateCond(i: number, c: Condition) {
  if (!draft.value) return
  draft.value.conditions[i] = c
  handEdited.value = true
}
function removeCond(i: number) {
  if (!draft.value) return
  // Removing the last condition leaves an invalid filtered rule (shown
  // inline); targeting everyone is an explicit choice (selectAll).
  draft.value.conditions.splice(i, 1)
  handEdited.value = true
}
function selectAll() {
  if (!draft.value) return
  draft.value.audience = 'all'
  draft.value.conditions = []
  handEdited.value = true
}
function addCond() {
  if (!draft.value) return
  const used = new Set(draft.value.conditions.map((c) => c.field))
  const field = ['city', 'abandoned_cart', 'purchased', 'order_count', 'total_spent', 'signed_up', 'language'].find((f) => !used.has(f)) ?? 'city'
  draft.value.conditions.push(defaultCondition(field))
  draft.value.audience = 'filtered'
  handEdited.value = true
}
function discard() {
  if (props.campaign.rule) {
    draft.value = clone(props.campaign.rule)
    handEdited.value = !props.campaign.rule.explanation
  }
  livePreview.value = props.preview
  serverErrors.value = {}
}

// ---- save ----
async function save(rule: Rule, fromClaude = false): Promise<boolean> {
  saving.value = true
  saveError.value = null
  try {
    // Claude's explanation no longer describes a hand-edited rule: drop it.
    const body = fromClaude || !handEdited.value ? rule : { ...rule, explanation: '' }
    applySaved(await api.putSegment(props.campaign.id, body), fromClaude)
    return true
  } catch (e) {
    if (e instanceof ApiError && e.code === 'campaign_locked') emit('locked')
    serverErrors.value = errorsByPath(e)
    saveError.value = e
    return false
  } finally {
    saving.value = false
  }
}
async function next() {
  if (draft.value && dirty.value && !(await save(draft.value))) return
  emit('next')
}

// ---- refine ----
async function refine(instruction: string) {
  if (!draft.value) return
  refineError.value = null
  // Refine works on the saved rule; save pending form edits first.
  if (dirty.value && !(await save(draft.value))) return
  refineBusy.value = true
  try {
    proposal.value = await api.refineSegment(props.campaign.id, instruction)
  } catch (e) {
    refineError.value = e
  } finally {
    refineBusy.value = false
  }
}
async function acceptProposal() {
  if (!proposal.value) return
  if (await save(proposal.value.rule, true)) proposal.value = null
}
</script>

<template>
  <section class="stack">
    <!-- generating -->
    <div v-if="generating" class="card row" style="gap: 12px">
      <span class="spinner" style="color: var(--accent)" />
      <div>
        <strong>Claude brief'i kitle kuralına çeviriyor…</strong>
        <div class="muted small">Çıktı izin listesine göre doğrulanır; hatalıysa Claude'a düzelttirilir (en fazla 2 deneme).</div>
      </div>
    </div>

    <!-- generation failed / no LLM -->
    <div v-else-if="!draft" class="card stack">
      <div class="alert" :class="genError ? 'alert-danger' : 'alert-warn'">
        <span class="alert-icon">!</span>
        <div class="alert-body">
          <div class="alert-title">Kitle kuralı oluşturulamadı</div>
          <div>{{ genError ? errorText(genError) : 'Claude yapılandırılmadığı için kural otomatik çıkarılamaz.' }}</div>
          <ul v-if="genError instanceof ApiError && genError.fieldErrors.length">
            <li v-for="fe in genError.fieldErrors" :key="fe.path + fe.code"><span class="mono">{{ fe.path }}</span>: {{ fe.message }}</li>
          </ul>
        </div>
      </div>
      <div class="row">
        <button v-if="appState.config?.llm_configured !== false" class="btn" type="button" @click="generate">Tekrar dene</button>
        <button class="btn btn-primary" type="button" @click="buildManually">Kuralı elle kur</button>
      </div>
    </div>

    <template v-else>
      <div class="claude-note" v-if="draft.explanation">
        <div class="claude-label"><span class="claude-mark">C</span> Claude bunu şöyle anladı</div>
        <p class="claude-text">“{{ draft.explanation }}”</p>
        <p v-if="handEdited" class="small muted" style="margin-top: 6px">
          Kural form ile değiştirildi; bu açıklama Claude'un yorumudur, aşağıdaki koşullar geçerlidir.
        </p>
      </div>
      <p v-else-if="handEdited && campaign.rule" class="small muted">Kural elle düzenlendi.</p>

      <div class="card">
        <div class="card-title">
          <h2>Koşullar</h2>
          <span class="muted small">Tüm koşullar <strong>VE</strong> ile birleşir</span>
        </div>
        <div class="stack-sm">
          <div v-if="draft.audience === 'all'" class="alert alert-info">
            <span class="alert-icon">i</span>
            <div class="alert-body">Koşul yok — <strong>tüm kullanıcılar</strong> hedefleniyor.</div>
          </div>
          <ConditionRow
            v-for="(c, i) in draft.conditions"
            :key="i"
            :condition="c"
            :errors="condErrors(i)"
            :disabled="saving || !!proposal"
            @update="updateCond(i, $event)"
            @remove="removeCond(i)"
          />
          <div v-for="m in ruleLevelErrors" :key="m" class="field-error">{{ m }}</div>
          <div class="row-between">
            <div class="row" style="gap: 8px">
              <button type="button" class="btn btn-sm" :disabled="saving || !!proposal" @click="addCond">+ Koşul ekle</button>
              <button
                v-if="draft.audience !== 'all'"
                type="button"
                class="btn btn-ghost btn-sm"
                :disabled="saving || !!proposal"
                @click="selectAll"
              >
                Tüm kullanıcılar
              </button>
            </div>
            <div v-if="dirty" class="row" style="gap: 8px">
              <span class="badge badge-warn">Kaydedilmemiş değişiklik</span>
              <button v-if="campaign.rule" type="button" class="btn btn-ghost btn-sm" @click="discard">Geri al</button>
              <button type="button" class="btn btn-sm" :disabled="saving || hasErrors" @click="save(draft)">Kaydet</button>
            </div>
          </div>
        </div>
      </div>

      <div class="card stack" v-if="livePreview">
        <AudienceStats :preview="livePreview" :stale="hasErrors" :loading="previewLoading" />
        <WarningList :preview="livePreview" :unmapped="draft.unmapped" />
      </div>

      <div class="card">
        <RefinePanel
          :current="draft"
          :proposal="proposal"
          :busy="refineBusy || saving"
          :error="refineError"
          :disabled="appState.config?.llm_configured === false"
          @refine="refine"
          @accept="acceptProposal"
          @reject="proposal = null"
        />
      </div>

      <div v-if="saveError && !Object.keys(serverErrors).length" class="alert alert-danger">
        <span class="alert-icon">!</span>
        <div class="alert-body">{{ errorText(saveError) }}</div>
      </div>

      <div class="footer-bar">
        <span class="muted small">Devam edildiğinde kural kaydedilir; içerik bu kitlenin en uzun isim ve şehrine göre ölçülür.</span>
        <button class="btn btn-primary btn-lg" type="button" :disabled="saving || hasErrors || !!proposal" @click="next">
          <span v-if="saving" class="spinner" /> İçeriğe geç →
        </button>
      </div>
    </template>
  </section>
</template>
