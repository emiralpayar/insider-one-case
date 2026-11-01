<script setup lang="ts">
// Composer wizard: Brief → Kitle → İçerik → Onay. Holds the campaign and the
// audience preview; each step component talks to the API and reports back.
import { computed, nextTick, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api'
import BriefStep from '../components/BriefStep.vue'
import RuleEditor from '../components/RuleEditor.vue'
import ContentEditor from '../components/ContentEditor.vue'
import ApprovePanel from '../components/ApprovePanel.vue'
import Icon from '../components/Icon.vue'
import Stepper from '../components/Stepper.vue'
import { FLOW_STEPS, type Step } from '../lib/steps'
import { errorText } from '../lib/errors'
import { CAMPAIGN_STATUS } from '../lib/format'
import type { Campaign, Content, PreviewResult, RuleResponse } from '../types'

const props = defineProps<{ id?: string }>()
const route = useRoute()
const router = useRouter()

const campaign = ref<Campaign | null>(null)
const preview = ref<PreviewResult | null>(null)
const step = ref<Step>('brief')
const loadError = ref('')
const creating = ref(false)
const createError = ref<unknown>(null)
const dirty = ref(false)

function reachable(s: Step | string): boolean {
  const c = campaign.value
  switch (s) {
    case 'brief':
      return true
    case 'audience':
      return !!c
    case 'content':
      // An empty audience (e.g. an unmapped "VIP" draft) gets no content.
      return !!c?.rule && preview.value?.total !== 0
    case 'approve':
      return !!c?.rule && !!c?.content
  }
  return false // 'send': only by approving
}
const stepIndex = computed(() => FLOW_STEPS.findIndex((s) => s.key === step.value))

// Each step starts at the top of the page (e.g. "Onaya geç" clicked at the
// bottom of the content step).
watch(step, async () => {
  await nextTick()
  window.scrollTo({ top: 0, left: 0, behavior: 'auto' })
})

function defaultStep(c: Campaign): Step {
  if (!c.rule) return 'audience'
  if (!c.content) return 'content'
  return 'approve'
}

function go(target: Step | string) {
  const s = target as Step
  if (!reachable(s) || s === step.value) return
  if (dirty.value && !window.confirm('Kaydedilmemiş değişiklikler var. Yine de adım değiştirilsin mi?')) return
  dirty.value = false
  step.value = s
  if (campaign.value) router.replace({ query: { step: s } })
}

async function load(id: string) {
  loadError.value = ''
  try {
    const c = await api.getCampaign(id)
    if (c.status === 'sending' || c.status === 'completed') {
      router.replace(`/campaigns/${c.id}`)
      return
    }
    campaign.value = c
    preview.value = null
    if (c.rule) preview.value = await api.preview(c.rule)
    const q = route.query.step as Step | undefined
    const want = q && reachable(q) ? q : defaultStep(c)
    step.value = reachable(want) ? want : 'audience'
  } catch (e) {
    loadError.value = errorText(e)
  }
}

watch(
  () => props.id,
  (id) => {
    if (!id) {
      campaign.value = null
      preview.value = null
      step.value = 'brief'
    } else if (campaign.value?.id !== id) {
      load(id)
    }
  },
  { immediate: true },
)

async function createCampaign(brief: string) {
  creating.value = true
  createError.value = null
  try {
    const c = await api.createCampaign(brief)
    campaign.value = c
    step.value = 'audience'
    await router.replace({ name: 'edit', params: { id: c.id }, query: { step: 'audience' } })
  } catch (e) {
    createError.value = e
  } finally {
    creating.value = false
  }
}

function onRuleSaved(r: RuleResponse) {
  if (!campaign.value) return
  campaign.value = {
    ...campaign.value,
    rule: r.rule,
    status: campaign.value.content ? 'content_ready' : 'audience_ready',
  }
  preview.value = r.preview
}

function onContentSaved(content: Content) {
  if (!campaign.value) return
  campaign.value = { ...campaign.value, content, status: 'content_ready' }
}

function onCampaign(c: Campaign) {
  campaign.value = c
}

function onLocked() {
  if (campaign.value) router.replace(`/campaigns/${campaign.value.id}`)
}
</script>

<template>
  <div class="page-head">
    <div>
      <div class="page-eyebrow">
        <RouterLink to="/"><Icon name="arrow-left" :size="13" /> Kampanyalar</RouterLink>
        <template v-if="campaign">
          <span>/</span>
          <span class="mono">{{ campaign.id.slice(0, 8) }}</span>
        </template>
      </div>
      <h1>{{ campaign ? 'Kampanyayı düzenle' : 'Yeni kampanya' }}</h1>
      <p v-if="campaign" class="row" style="gap: 8px">
        <span class="badge badge-dot" :class="'status-' + campaign.status">{{ CAMPAIGN_STATUS[campaign.status] }}</span>
        <span v-if="campaign.llm_provider === 'mock'" class="badge badge-mock">MOCK</span>
        <span class="muted small mono" :title="campaign.id">{{ campaign.id }}</span>
      </p>
      <p v-else>Brief'inizi yazın; Claude kitleyi ve mesajları önersin, son kararı siz verin.</p>
    </div>
  </div>

  <Stepper :steps="FLOW_STEPS" :current="step" :current-index="stepIndex" :reachable="reachable" @go="go" />

  <div v-if="loadError" class="alert alert-danger">
    <span class="alert-icon">!</span>
    <div class="alert-body">{{ loadError }} <RouterLink to="/">Kampanyalara dön</RouterLink></div>
  </div>

  <div v-else-if="id && !campaign" class="row muted"><span class="spinner" /> Yükleniyor…</div>

  <template v-else>
    <BriefStep
      v-if="step === 'brief'"
      :campaign="campaign"
      :busy="creating"
      :error="createError"
      @submit="createCampaign"
      @next="go('audience')"
    />
    <RuleEditor
      v-else-if="step === 'audience' && campaign"
      :campaign="campaign"
      :preview="preview"
      @saved="onRuleSaved"
      @dirty="dirty = $event"
      @locked="onLocked"
      @next="dirty = false; go('content')"
    />
    <ContentEditor
      v-else-if="step === 'content' && campaign"
      :campaign="campaign"
      :preview="preview"
      @saved="onContentSaved"
      @dirty="dirty = $event"
      @locked="onLocked"
      @back="go('audience')"
      @next="dirty = false; go('approve')"
    />
    <ApprovePanel
      v-else-if="step === 'approve' && campaign"
      :campaign="campaign"
      :preview="preview"
      @campaign="onCampaign"
      @preview="preview = $event"
      @go="go"
    />
  </template>
</template>
