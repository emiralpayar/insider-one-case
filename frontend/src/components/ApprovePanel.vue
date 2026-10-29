<script setup lang="ts">
// Onay adımı: channel toggles, summary of what will be sent to whom, the
// audience warnings again, the large-audience confirmation and approval.
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, ApiError } from '../api'
import { errorText } from '../lib/errors'
import { num, pct } from '../lib/format'
import { WORST_CASE_FIRST_NAME, describeRule, worstCaseCity } from '../lib/rule'
import { render, smsStats } from '../lib/sms'
import type { Campaign, Channel, PreviewResult } from '../types'
import MessagePreview from './MessagePreview.vue'
import WarningList from './WarningList.vue'

const props = defineProps<{ campaign: Campaign; preview: PreviewResult | null }>()
const emit = defineEmits<{ campaign: [c: Campaign]; preview: [p: PreviewResult]; go: [step: 'audience' | 'content'] }>()
const router = useRouter()

const confirmLarge = ref(false)
const approving = ref(false)
const approveError = ref<unknown>(null)
const channelBusy = ref(false)
const channelError = ref<unknown>(null)
const refreshing = ref(true)

onMounted(async () => {
  // Re-count right before approval: the data may have changed since the audience step.
  try {
    if (props.campaign.rule) emit('preview', await api.preview(props.campaign.rule))
  } catch {
    /* keep the previous preview */
  } finally {
    refreshing.value = false
  }
})

const pv = computed(() => props.preview)
const enabled = (ch: Channel) => props.campaign.channels.includes(ch)
const isLarge = computed(() => !!pv.value?.warnings.some((w) => w.code === 'large_audience'))
const isEmpty = computed(() => !!pv.value?.warnings.some((w) => w.code === 'empty_audience'))

const missingContent = computed(() => {
  const c = props.campaign.content
  const out: Channel[] = []
  if (enabled('sms') && !c?.sms.trim()) out.push('sms')
  if (enabled('email') && (!c?.email.subject.trim() || !c?.email.body.trim())) out.push('email')
  return out
})

const smsSegments = computed(() => {
  const c = props.campaign.content
  if (!c) return 0
  return smsStats(render(c.sms, { first_name: WORST_CASE_FIRST_NAME, city: worstCaseCity(props.campaign.rule) })).segments
})

const recipients = computed(() => {
  if (!pv.value) return 0
  return (enabled('sms') ? pv.value.sms_eligible : 0) + (enabled('email') ? pv.value.email_eligible : 0)
})

const blockReason = computed(() => {
  if (!pv.value) return 'Kitle sayıları yükleniyor…'
  if (isEmpty.value) return 'Kitle boş — onay verilemez.'
  if (recipients.value === 0) return 'Seçili kanallarda izni olan alıcı yok — onay verilemez.'
  if (missingContent.value.length) return 'Açık bir kanalın içeriği yok.'
  if (isLarge.value && !confirmLarge.value) return 'Geniş kitle onay kutusunu işaretleyin.'
  return ''
})

async function toggle(ch: Channel, on: boolean) {
  const next = on ? [...props.campaign.channels, ch] : props.campaign.channels.filter((c) => c !== ch)
  channelBusy.value = true
  channelError.value = null
  try {
    emit('campaign', await api.putChannels(props.campaign.id, next))
  } catch (e) {
    channelError.value = e
  } finally {
    channelBusy.value = false
  }
}

async function approve() {
  if (blockReason.value || approving.value) return
  approving.value = true
  approveError.value = null
  try {
    await api.approve(props.campaign.id, confirmLarge.value)
    router.push(`/campaigns/${props.campaign.id}`)
  } catch (e) {
    if (e instanceof ApiError && (e.code === 'already_approved' || e.code === 'campaign_locked')) {
      router.push(`/campaigns/${props.campaign.id}`)
      return
    }
    approveError.value = e
  } finally {
    approving.value = false
  }
}
</script>

<template>
  <section class="stack">
    <div class="grid-2" style="align-items: start">
      <div class="card stack">
        <div class="card-title" style="margin: 0">
          <h2>Kitle</h2>
          <button class="btn btn-ghost btn-sm" type="button" @click="emit('go', 'audience')">Düzenle</button>
        </div>
        <p v-if="campaign.rule?.explanation" class="small">
          <span class="muted">Claude'un yorumu:</span> {{ campaign.rule.explanation }}
        </p>
        <ul class="rule-lines">
          <li v-for="l in campaign.rule ? describeRule(campaign.rule) : []" :key="l">{{ l }}</li>
        </ul>
        <div v-if="pv" class="small" :class="{ stale: refreshing }">
          <strong>{{ num(pv.total) }}</strong> kişi ({{ pct(pv.total, pv.all_users) }} / {{ num(pv.all_users) }} kullanıcı)
        </div>
      </div>

      <div class="card stack">
        <div class="card-title" style="margin: 0"><h2>Kanallar ve alıcılar</h2></div>
        <div v-for="ch in (['sms', 'email'] as Channel[])" :key="ch" class="channel-row">
          <label class="checkbox">
            <input
              type="checkbox"
              :checked="enabled(ch)"
              :disabled="channelBusy || (enabled(ch) && campaign.channels.length === 1)"
              @change="toggle(ch, ($event.target as HTMLInputElement).checked)"
            />
            <span>
              <strong>{{ ch === 'sms' ? 'SMS' : 'E-posta' }}</strong>
              <span v-if="pv" class="muted small" style="display: block">
                {{ num(ch === 'sms' ? pv.sms_eligible : pv.email_eligible) }} izinli alıcı
                <template v-if="ch === 'sms' && smsSegments > 1"> · mesaj başına {{ smsSegments }} parça</template>
              </span>
            </span>
          </label>
          <span v-if="enabled(ch) && missingContent.includes(ch)" class="badge badge-danger">İçerik yok</span>
        </div>
        <div v-if="campaign.channels.length === 1" class="muted small">En az bir kanal açık kalmalı.</div>
        <div v-if="channelError" class="field-error">{{ errorText(channelError) }}</div>
        <div v-if="missingContent.length" class="alert alert-danger">
          <span class="alert-icon">!</span>
          <div class="alert-body">
            Açık kanal için içerik yok.
            <button class="btn btn-sm" type="button" style="margin-left: 6px" @click="emit('go', 'content')">İçerik adımına dön</button>
          </div>
        </div>
      </div>
    </div>

    <div v-if="pv" class="card stack">
      <div class="card-title" style="margin: 0"><h2>Uyarılar</h2></div>
      <WarningList v-if="pv.warnings.length" :preview="pv" :unmapped="campaign.rule?.unmapped ?? []" />
      <p v-else class="muted">Uyarı yok.</p>
    </div>

    <div v-if="campaign.content" class="card">
      <div class="card-title">
        <h2>Gönderilecek mesajlar</h2>
        <button class="btn btn-ghost btn-sm" type="button" @click="emit('go', 'content')">Düzenle</button>
      </div>
      <MessagePreview
        :content="campaign.content"
        :sample="pv?.sample.find((u) => u.first_name) ?? pv?.sample[0] ?? null"
        :channels="campaign.channels"
      />
    </div>

    <div class="card stack approve-box">
      <label v-if="isLarge" class="checkbox large-confirm">
        <input v-model="confirmLarge" type="checkbox" />
        <span>
          <strong>Geniş kitleyi onaylıyorum.</strong>
          Bu kampanya tüm kullanıcıların yarısından fazlasına ({{ num(pv?.total ?? 0) }} / {{ num(pv?.all_users ?? 0) }}) gidecek.
        </span>
      </label>
      <div v-if="approveError" class="alert alert-danger">
        <span class="alert-icon">!</span>
        <div class="alert-body">
          {{ errorText(approveError) }}
          <ul v-if="approveError instanceof ApiError && approveError.fieldErrors.length">
            <li v-for="fe in approveError.fieldErrors" :key="fe.path + fe.code"><span class="mono">{{ fe.path }}</span>: {{ fe.message }}</li>
          </ul>
        </div>
      </div>
      <div class="row-between">
        <div>
          <div><strong>{{ num(recipients) }}</strong> mesaj kuyruğa alınacak</div>
          <div v-if="blockReason" class="small" style="color: var(--danger)">{{ blockReason }}</div>
          <div v-else class="muted small">Onaydan sonra kampanya düzenlenemez.</div>
        </div>
        <button class="btn btn-primary btn-lg" type="button" :disabled="!!blockReason || approving" @click="approve">
          <span v-if="approving" class="spinner" /> Onayla ve gönder
        </button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.rule-lines { margin: 0; padding-left: 18px; }
.channel-row { display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--radius-sm); }
.approve-box { border-color: var(--border-strong); }
.large-confirm { padding: 12px 14px; border-radius: var(--radius-sm); background: var(--warn-soft); border: 1px solid var(--warn-border); color: #6b4300; }
</style>
