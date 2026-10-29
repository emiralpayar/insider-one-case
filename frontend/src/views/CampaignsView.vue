<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../api'
import { errorText } from '../lib/errors'
import { CAMPAIGN_STATUS, CHANNEL_LABEL, dateTime } from '../lib/format'
import type { Campaign } from '../types'

const campaigns = ref<Campaign[] | null>(null)
const error = ref('')

onMounted(async () => {
  try {
    campaigns.value = await api.listCampaigns()
  } catch (e) {
    error.value = errorText(e)
  }
})

const locked = (c: Campaign) => c.status === 'sending' || c.status === 'completed'
const link = (c: Campaign) => (locked(c) ? `/campaigns/${c.id}` : `/campaigns/${c.id}/edit`)
</script>

<template>
  <div class="page-head">
    <div>
      <h1>Kampanyalar</h1>
      <p>Doğal dille yazılan brief'ten kitle ve içerik üretilen kampanyalar.</p>
    </div>
    <RouterLink to="/new" class="btn btn-primary btn-lg">+ Yeni kampanya</RouterLink>
  </div>

  <div v-if="error" class="alert alert-danger"><span class="alert-icon">!</span><div class="alert-body">{{ error }}</div></div>

  <div v-else-if="campaigns === null" class="row muted"><span class="spinner" /> Yükleniyor…</div>

  <div v-else-if="campaigns.length === 0" class="card empty-state">
    <h2>Henüz kampanya yok</h2>
    <p class="muted">Bir brief yazın; Claude kitleyi ve mesajları önersin, siz onaylayın.</p>
    <p style="margin-top: 16px"><RouterLink to="/new" class="btn btn-primary">İlk kampanyayı oluştur</RouterLink></p>
  </div>

  <div v-else class="table-wrap">
    <table class="table">
      <thead>
        <tr>
          <th>Brief</th>
          <th>Durum</th>
          <th>Kanallar</th>
          <th>Oluşturuldu</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="c in campaigns" :key="c.id">
          <td style="max-width: 520px">
            <RouterLink :to="link(c)" class="brief-link">{{ c.brief }}</RouterLink>
          </td>
          <td>
            <div class="row" style="gap: 6px">
              <span class="badge" :class="'status-' + c.status">{{ CAMPAIGN_STATUS[c.status] ?? c.status }}</span>
              <span v-if="c.llm_provider === 'mock'" class="badge badge-mock" title="İçerik ve kitle mock LLM ile üretildi">MOCK</span>
            </div>
          </td>
          <td class="nowrap">{{ c.channels.map((ch) => CHANNEL_LABEL[ch]).join(' + ') }}</td>
          <td class="nowrap muted">{{ dateTime(c.created_at) }}</td>
          <td class="nowrap" style="text-align: right">
            <RouterLink :to="link(c)" class="btn btn-sm">{{ locked(c) ? 'Durumu izle' : 'Düzenle' }}</RouterLink>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.brief-link {
  color: var(--text);
  font-weight: 500;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
</style>
