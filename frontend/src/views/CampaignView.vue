<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../api'
import CampaignName from '../components/CampaignName.vue'
import Icon from '../components/Icon.vue'
import StatusBoard from '../components/StatusBoard.vue'
import Stepper from '../components/Stepper.vue'
import { FLOW_STEPS } from '../lib/steps'
import { errorText } from '../lib/errors'
import { dateTime } from '../lib/format'
import { describeRule } from '../lib/rule'
import type { Campaign, CampaignStatus } from '../types'

const props = defineProps<{ id: string }>()
const campaign = ref<Campaign | null>(null)
const error = ref('')

onMounted(async () => {
  try {
    campaign.value = await api.getCampaign(props.id)
  } catch (e) {
    error.value = errorText(e)
  }
})

const sendIndex = FLOW_STEPS.length - 1
const notReachable = () => false

function onStatus(s: CampaignStatus) {
  if (campaign.value) campaign.value.status = s
}
</script>

<template>
  <div v-if="error" class="alert alert-danger">
    <span class="alert-icon">!</span>
    <div class="alert-body">{{ error }} <RouterLink to="/">Kampanyalara dön</RouterLink></div>
  </div>
  <div v-else-if="!campaign" class="row muted"><span class="spinner" /> Yükleniyor…</div>
  <template v-else>
    <div class="page-head">
      <div style="min-width: 0; max-width: 820px">
        <div class="page-eyebrow">
          <RouterLink to="/"><Icon name="arrow-left" :size="13" /> Kampanyalar</RouterLink>
          <span>/</span>
          <span>Gönderim durumu</span>
          <span v-if="campaign.llm_provider === 'mock'" class="badge badge-mock" title="Kitle ve içerik mock LLM ile üretildi, Claude ile değil">MOCK</span>
        </div>
        <CampaignName :campaign="campaign" @renamed="campaign = { ...campaign, name: $event.name }" />
        <p class="brief-line">{{ campaign.brief }}</p>
        <div class="meta">
          <span><Icon name="clock" :size="13" /> Onay: {{ dateTime(campaign.approved_at) }}</span>
          <span><Icon name="filter" :size="13" /> Kitle: {{ campaign.rule ? describeRule(campaign.rule).join(' · ') : '—' }}</span>
        </div>
      </div>
      <RouterLink v-if="campaign.status !== 'sending' && campaign.status !== 'completed'" :to="`/campaigns/${campaign.id}/edit`" class="btn">
        Düzenlemeye dön
      </RouterLink>
    </div>
    <Stepper
      v-if="campaign.status === 'sending' || campaign.status === 'completed'"
      :steps="FLOW_STEPS"
      current="send"
      :current-index="campaign.status === 'completed' ? sendIndex + 1 : sendIndex"
      :reachable="notReachable"
    />
    <StatusBoard :campaign-id="campaign.id" @status="onStatus" />
  </template>
</template>

<style scoped>
.brief-line { font-size: 15px; color: var(--text-2); }
.meta { display: flex; flex-wrap: wrap; gap: 6px 18px; margin-top: 10px; font-size: 12.5px; color: var(--text-3); }
.meta span { display: inline-flex; align-items: center; gap: 6px; }
</style>
