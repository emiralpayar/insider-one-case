<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '../api'
import { errorText } from '../lib/errors'
import { CAMPAIGN_STATUS, CHANNEL_LABEL, dateTime, num } from '../lib/format'
import Icon from '../components/Icon.vue'
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
const counts = computed(() => {
  const cs = campaigns.value ?? []
  return {
    total: cs.length,
    sending: cs.filter((c) => c.status === 'sending').length,
    completed: cs.filter((c) => c.status === 'completed').length,
    draft: cs.filter((c) => !locked(c)).length,
  }
})
const link = (c: Campaign) => (locked(c) ? `/campaigns/${c.id}` : `/campaigns/${c.id}/edit`)
</script>

<template>
  <div class="page-head">
    <div>
      <h1>Kampanyalar</h1>
      <p>Doğal dille yazılan brief'ten kitle ve içerik üretilen kampanyalar.</p>
    </div>
    <RouterLink to="/new" class="btn btn-primary btn-lg"><Icon name="plus" :size="16" /> Yeni kampanya</RouterLink>
  </div>

  <div v-if="error" class="alert alert-danger"><span class="alert-icon">!</span><div class="alert-body">{{ error }}</div></div>

  <div v-else-if="campaigns === null" class="stack">
    <div class="stats stats-4">
      <div v-for="i in 4" :key="i" class="stat"><span class="skeleton" style="width: 50%" /><span class="skeleton" style="width: 30%; height: 24px" /></div>
    </div>
    <div class="card"><span class="skeleton" /><span class="skeleton" style="width: 80%" /><span class="skeleton" style="width: 60%" /></div>
  </div>

  <div v-else-if="campaigns.length === 0" class="card empty-state">
    <div class="empty-art"><Icon name="megaphone" :size="30" /></div>
    <h2>Henüz kampanya yok</h2>
    <p class="muted">Bir brief yazın; Claude kitleyi ve mesajları önersin, siz onaylayın.</p>
    <p style="margin-top: 20px"><RouterLink to="/new" class="btn btn-primary"><Icon name="plus" :size="14" /> İlk kampanyayı oluştur</RouterLink></p>
  </div>

  <div v-else class="stack">
    <div class="stats stats-4">
      <div class="stat">
        <div class="stat-head"><span class="stat-icon"><Icon name="megaphone" :size="14" /></span><span class="stat-label">Toplam kampanya</span></div>
        <div class="stat-value">{{ num(counts.total) }}</div>
      </div>
      <div class="stat">
        <div class="stat-head"><span class="stat-icon blue"><Icon name="send" :size="14" /></span><span class="stat-label">Gönderiliyor</span></div>
        <div class="stat-value">{{ num(counts.sending) }}</div>
      </div>
      <div class="stat">
        <div class="stat-head"><span class="stat-icon green"><Icon name="check-circle" :size="14" /></span><span class="stat-label">Tamamlandı</span></div>
        <div class="stat-value">{{ num(counts.completed) }}</div>
      </div>
      <div class="stat">
        <div class="stat-head"><span class="stat-icon amber"><Icon name="edit" :size="14" /></span><span class="stat-label">Taslak</span></div>
        <div class="stat-value">{{ num(counts.draft) }}</div>
      </div>
    </div>

    <div class="table-wrap">
      <table class="table">
        <thead>
          <tr>
            <th>Kampanya</th>
            <th>Durum</th>
            <th>Kanallar</th>
            <th>Oluşturuldu</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="c in campaigns" :key="c.id">
            <td style="max-width: 520px">
              <div class="row" style="gap: 12px; flex-wrap: nowrap">
                <span class="card-icon" :class="c.status === 'completed' ? 'green' : c.status === 'sending' ? '' : 'slate'"><Icon name="megaphone" :size="15" /></span>
                <div style="min-width: 0">
                  <RouterLink :to="link(c)" class="name-link">{{ c.name || 'İsimsiz kampanya' }}</RouterLink>
                  <div class="brief-line muted small" :title="c.brief">{{ c.brief }}</div>
                </div>
              </div>
            </td>
            <td>
              <div class="row" style="gap: 6px">
                <span class="badge badge-dot" :class="'status-' + c.status">{{ CAMPAIGN_STATUS[c.status] ?? c.status }}</span>
                <span v-if="c.llm_provider === 'mock'" class="badge badge-mock" title="İçerik ve kitle mock LLM ile üretildi">MOCK</span>
              </div>
            </td>
            <td class="nowrap">
              <span class="row" style="gap: 6px">
                <span v-for="ch in c.channels" :key="ch" class="ch-pill" :class="'ch-' + ch">
                  <Icon :name="ch === 'sms' ? 'sms' : 'mail'" :size="12" /> {{ CHANNEL_LABEL[ch] }}
                </span>
              </span>
            </td>
            <td class="nowrap muted small">{{ dateTime(c.created_at) }}</td>
            <td class="nowrap" style="text-align: right">
              <RouterLink :to="link(c)" class="btn btn-sm">
                <Icon :name="locked(c) ? 'activity' : 'edit'" :size="13" /> {{ locked(c) ? 'Durumu izle' : 'Düzenle' }}
              </RouterLink>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
.name-link { color: var(--text); font-weight: 600; overflow-wrap: anywhere; }
.name-link:hover { color: var(--accent-text); text-decoration: none; }
.brief-line {
  margin-top: 2px;
  display: -webkit-box;
  -webkit-line-clamp: 1;
  line-clamp: 1;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.card-icon { width: 32px; height: 32px; }
.ch-pill { display: inline-flex; align-items: center; gap: 4px; height: 22px; padding: 0 8px; border-radius: 6px; font-size: 12px; font-weight: 600; }
.ch-sms { background: var(--var-name-soft); color: var(--var-name); }
.ch-email { background: var(--var-city-soft); color: var(--var-city); }
</style>
