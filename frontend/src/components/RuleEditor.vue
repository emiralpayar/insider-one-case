<script setup lang="ts">
// Kitle adımı: Claude's rule (explanation, conditions, unmapped), live audience
// preview with 400 ms debounce, natural-language refine and save.
// Subcomponents: ConditionRow, AudienceStats, WarningList, RefinePanel.
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { api, ApiError } from '../api'
import { errorText, errorsByPath } from '../lib/errors'
import { defaultCondition, isUnmappedEmpty, validateLocal } from '../lib/rule'
import { appState } from '../store'
import type { Campaign, Condition, PreviewResult, Rule, RuleResponse } from '../types'
import AudienceDrawer from './AudienceDrawer.vue'
import AudienceStats from './AudienceStats.vue'
import ConditionRow from './ConditionRow.vue'
import Icon from './Icon.vue'
import RefinePanel from './RefinePanel.vue'
import WarningList from './WarningList.vue'

const props = defineProps<{ campaign: Campaign; preview: PreviewResult | null }>()
const emit = defineEmits<{ saved: [r: RuleResponse]; dirty: [d: boolean]; next: []; locked: [] }>()

const clone = (r: Rule): Rule => JSON.parse(JSON.stringify(r))

const draft = ref<Rule | null>(props.campaign.rule ? clone(props.campaign.rule) : null)
const livePreview = ref<PreviewResult | null>(props.preview)
const previewLoading = ref(false)
// Content for an empty audience would be a paid generation for nobody.
const audienceEmpty = computed(() => !previewLoading.value && livePreview.value?.total === 0)
const serverErrors = ref<Record<string, string[]>>({})
const generating = ref(false)
const genError = ref<unknown>(null)
const saving = ref(false)
const saveError = ref<unknown>(null)
// The rule differs from what Claude produced (form edits, or saved that way:
// a hand-edited rule is saved with an empty explanation).
const handEdited = ref(!!props.campaign.rule && !props.campaign.rule.explanation)

const unmappedEmpty = computed(() => !!draft.value && isUnmappedEmpty(draft.value))
const unmappedPhrase = computed(() => {
  const xs = (draft.value?.unmapped ?? []).map((u) => `“${u}”`)
  if (xs.length <= 1) return { list: xs.join(''), noun: 'ifadesinin' }
  return { list: xs.slice(0, -1).join(', ') + ' ve ' + xs[xs.length - 1], noun: 'ifadelerinin' }
})

// "Kitleyi gör" drawer: lists the current (possibly unsaved) rule's audience.
const showAudience = ref(false)

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
    <div v-if="generating" class="rule-layout">
      <div class="stack">
        <div class="card gen-card">
          <div class="row" style="gap: 14px; flex-wrap: nowrap">
            <span class="card-icon"><span class="spinner" /></span>
            <div>
              <strong>Claude brief'i kitle kuralına çeviriyor…</strong>
              <div class="muted small">Çıktı izin listesine göre doğrulanır; hatalıysa Claude'a düzelttirilir (en fazla 2 deneme).</div>
            </div>
          </div>
          <div class="gen-skel">
            <span class="skeleton" style="width: 72%" />
            <span class="skeleton" style="width: 54%" />
          </div>
        </div>
        <div class="card">
          <span class="skeleton" style="width: 30%; height: 14px" />
          <div class="skel-cond"><span class="skeleton" style="width: 85%" /></div>
          <div class="skel-cond"><span class="skeleton" style="width: 65%" /></div>
        </div>
      </div>
      <div class="card">
        <span class="skeleton" style="width: 40%" />
        <span class="skeleton" style="width: 60%; height: 28px" />
        <span class="skeleton" style="width: 90%" />
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
        <button v-if="appState.config?.llm_configured !== false" class="btn" type="button" @click="generate">
          <Icon name="refresh" :size="14" /> Tekrar dene
        </button>
        <button class="btn btn-primary" type="button" @click="buildManually"><Icon name="edit" :size="14" /> Kuralı elle kur</button>
      </div>
    </div>

    <template v-else>
      <div class="claude-note" v-if="draft.explanation">
        <div class="claude-label"><span class="claude-mark"><Icon name="sparkles" :size="13" /></span> Claude bunu şöyle anladı</div>
        <p class="claude-text">“{{ draft.explanation }}”</p>
        <p v-if="handEdited" class="small muted" style="margin-top: 6px">
          Kural form ile değiştirildi; bu açıklama Claude'un yorumudur, aşağıdaki koşullar geçerlidir.
        </p>
      </div>
      <p v-else-if="handEdited && campaign.rule" class="small muted row" style="gap: 6px"><Icon name="edit" :size="13" /> Kural elle düzenlendi.</p>

      <div class="rule-layout">
        <div class="stack">
          <div class="card card-flush">
            <div class="card-head">
              <span class="card-icon"><Icon name="filter" :size="17" /></span>
              <h2>Koşullar<span class="sub">Tüm koşullar <strong>VE</strong> ile birleşir</span></h2>
              <span v-if="dirty" class="badge badge-warn badge-dot">Kaydedilmemiş değişiklik</span>
            </div>
            <div class="card-body stack-sm">
              <div v-if="unmappedEmpty" class="unmapped-callout" role="alert">
                <span class="card-icon amber"><Icon name="alert" :size="18" /></span>
                <div class="stack-sm" style="gap: 10px">
                  <div>
                    <div class="unmapped-title">Bu brief için kural kurulamadı</div>
                    <p>
                      Brief'teki <strong>{{ unmappedPhrase.list }}</strong> {{ unmappedPhrase.noun }} bir karşılığı yok.
                      Kural boş bırakıldı — aşağıdan koşul ekleyin.
                    </p>
                    <p class="small" style="margin-top: 4px">Boş kitleyle kampanya onaylanamaz.</p>
                  </div>
                  <div class="row" style="gap: 8px">
                    <button type="button" class="btn btn-primary btn-sm" :disabled="saving || !!proposal" @click="addCond">
                      <Icon name="plus" :size="14" /> Koşul ekle
                    </button>
                    <span class="small muted">ya da aşağıda Claude'a doğal dille tarif edin.</span>
                  </div>
                </div>
              </div>
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
              <div v-for="m in ruleLevelErrors" :key="m" class="field-error"><Icon name="alert" :size="13" style="margin-top: 2px" /> {{ m }}</div>
              <div class="row-between" style="margin-top: 4px">
                <div class="row" style="gap: 8px">
                  <button type="button" class="btn btn-sm btn-soft" :disabled="saving || !!proposal" @click="addCond">
                    <Icon name="plus" :size="14" /> Koşul ekle
                  </button>
                  <button
                    v-if="draft.audience !== 'all'"
                    type="button"
                    class="btn btn-ghost btn-sm"
                    :disabled="saving || !!proposal"
                    @click="selectAll"
                  >
                    <Icon name="users" :size="14" /> Tüm kullanıcılar
                  </button>
                </div>
                <div v-if="dirty" class="row" style="gap: 8px">
                  <button v-if="campaign.rule" type="button" class="btn btn-ghost btn-sm" @click="discard"><Icon name="undo" :size="14" /> Geri al</button>
                  <button type="button" class="btn btn-sm" :disabled="saving || hasErrors" @click="save(draft)"><Icon name="save" :size="14" /> Kaydet</button>
                </div>
              </div>
            </div>
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
        </div>

        <aside class="rule-aside">
          <div v-if="livePreview" class="card card-flush">
            <div class="card-head">
              <span class="card-icon teal"><Icon name="users" :size="17" /></span>
              <h2>Kitle<span class="sub">Canlı önizleme</span></h2>
              <button
                type="button"
                class="btn btn-sm btn-soft"
                :disabled="hasErrors || livePreview.total === 0"
                :title="hasErrors ? 'Önce kuraldaki hataları düzeltin' : 'Kurala uyan tüm kişileri listele'"
                @click="showAudience = true"
              >
                <Icon name="list" :size="14" /> Kitleyi gör
              </button>
            </div>
            <div class="card-body stack">
              <AudienceStats :preview="livePreview" :stale="hasErrors" :loading="previewLoading" />
              <WarningList :preview="livePreview" :unmapped="draft.unmapped" />
            </div>
          </div>
        </aside>
      </div>

      <div v-if="saveError && !Object.keys(serverErrors).length" class="alert alert-danger">
        <span class="alert-icon">!</span>
        <div class="alert-body">{{ errorText(saveError) }}</div>
      </div>

      <AudienceDrawer v-if="showAudience && draft" :rule="draft" @close="showAudience = false" />

      <div class="footer-bar">
        <span v-if="audienceEmpty" class="muted small">Kitle boş — içerik üretmek için önce kişi seçen bir koşul ekleyin.</span>
        <span v-else class="muted small">Devam edildiğinde kural kaydedilir; içerik bu kitlenin en uzun isim ve şehrine göre ölçülür.</span>
        <button class="btn btn-primary btn-lg" type="button" :disabled="saving || hasErrors || !!proposal || audienceEmpty" @click="next">
          <span v-if="saving" class="spinner" /> İçeriğe geç <Icon name="arrow-right" :size="16" />
        </button>
      </div>
    </template>
  </section>
</template>

<style scoped>
.rule-layout { display: grid; grid-template-columns: minmax(0, 1fr) 340px; gap: var(--space-4); align-items: start; }
.rule-aside { position: sticky; top: var(--space-4); display: flex; flex-direction: column; gap: var(--space-4); }
.gen-skel { margin-top: 16px; padding-left: 48px; }
.skel-cond { margin-top: 14px; padding: 14px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface-2); }
.unmapped-callout {
  display: flex; gap: 14px; align-items: flex-start;
  padding: 16px 18px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--warn-border);
  border-left: 4px solid var(--warn-strong);
  background: linear-gradient(135deg, #fffbeb, #fff7ed);
  color: var(--warn-text);
}
.unmapped-title { font-weight: 700; font-size: 15px; color: #78350f; margin-bottom: 2px; }
.unmapped-callout p strong { color: #78350f; }
@media (max-width: 1100px) {
  .rule-layout { grid-template-columns: 1fr; }
  .rule-aside { position: static; }
}
</style>
