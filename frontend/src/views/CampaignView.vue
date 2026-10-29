<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../api'
import StatusBoard from '../components/StatusBoard.vue'
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
      <div style="max-width: 760px">
        <div class="row" style="gap: 8px; margin-bottom: 6px">
          <RouterLink to="/" class="small">← Kampanyalar</RouterLink>
          <span v-if="campaign.llm_provider === 'mock'" class="badge badge-mock" title="Kitle ve içerik mock LLM ile üretildi, Claude ile değil">MOCK</span>
        </div>
        <h1>Gönderim durumu</h1>
        <p>{{ campaign.brief }}</p>
        <p class="muted small" style="margin-top: 6px">
          Onay: {{ dateTime(campaign.approved_at) }} · Kitle: {{ campaign.rule ? describeRule(campaign.rule).join(' · ') : '—' }}
        </p>
      </div>
      <RouterLink v-if="campaign.status !== 'sending' && campaign.status !== 'completed'" :to="`/campaigns/${campaign.id}/edit`" class="btn">
        Düzenlemeye dön
      </RouterLink>
    </div>
    <StatusBoard :campaign-id="campaign.id" @status="onStatus" />
  </template>
</template>
