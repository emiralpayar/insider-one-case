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
import type { Campaign, Channel, DuplicateSend, PreviewResult } from '../types'
import Icon from './Icon.vue'
import MessagePreview from './MessagePreview.vue'
import WarningList from './WarningList.vue'

const props = defineProps<{ campaign: Campaign; preview: PreviewResult | null }>()
const emit = defineEmits<{ campaign: [c: Campaign]; preview: [p: PreviewResult]; go: [step: 'audience' | 'content'] }>()
const router = useRouter()

const confirmLarge = ref(false)
const confirmDuplicate = ref(false)
const duplicates = ref<DuplicateSend[]>([])

// Earlier campaigns that already sent this exact text to people in this audience.
async function loadDuplicates() {
  try {
    duplicates.value = (await api.duplicates(props.campaign.id)).duplicates
  } catch {
    duplicates.value = [] // the server re-checks on approve anyway
  }
}
const approving = ref(false)
const approveError = ref<unknown>(null)
const channelBusy = ref(false)
const channelError = ref<unknown>(null)
const refreshing = ref(true)

onMounted(async () => {
  void loadDuplicates()
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

const shownDuplicates = computed(() => duplicates.value.filter((d) => enabled(d.channel)))
const CH_LABEL: Record<Channel, string> = { sms: 'SMS', email: 'e-posta' }
const fmtDate = (s: string) => new Date(s).toLocaleDateString('tr-TR', { day: 'numeric', month: 'long' })

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
  if (shownDuplicates.value.length && !confirmDuplicate.value) return 'Tekrar gönderim onay kutusunu işaretleyin.'
  return ''
})

async function toggle(ch: Channel, on: boolean) {
  const next = on ? [...props.campaign.channels, ch] : props.campaign.channels.filter((c) => c !== ch)
  channelBusy.value = true
  channelError.value = null
  try {
    emit('campaign', await api.putChannels(props.campaign.id, next))
    void loadDuplicates()
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
    await api.approve(props.campaign.id, confirmLarge.value, confirmDuplicate.value)
    router.push(`/campaigns/${props.campaign.id}`)
  } catch (e) {
    if (e instanceof ApiError && (e.code === 'already_approved' || e.code === 'campaign_locked')) {
      router.push(`/campaigns/${props.campaign.id}`)
      return
    }
    if (e instanceof ApiError && e.code === 'duplicate_not_confirmed') void loadDuplicates()
    approveError.value = e
  } finally {
    approving.value = false
  }
}
</script>

<template>
  <section class="stack">
    <div class="stats stats-4" :class="{ stale: refreshing }">
      <div class="stat">
        <div class="stat-head"><span class="stat-icon" :class="isEmpty ? 'red' : ''"><Icon name="target" :size="15" /></span><span class="stat-label">Kitle</span></div>
        <div class="stat-value" :style="isEmpty ? 'color: var(--danger)' : ''">{{ pv ? num(pv.total) : '—' }}<span class="unit">kişi</span></div>
        <div class="stat-sub">{{ pv ? `${pct(pv.total, pv.all_users)} / ${num(pv.all_users)} kullanıcı` : 'yükleniyor' }}</div>
      </div>
      <div class="stat" :class="{ 'stat-off': !enabled('sms') }">
        <div class="stat-head"><span class="stat-icon violet"><Icon name="sms" :size="14" /></span><span class="stat-label">SMS alıcısı</span></div>
        <div class="stat-value">{{ pv && enabled('sms') ? num(pv.sms_eligible) : '—' }}</div>
        <div class="stat-sub">{{ enabled('sms') ? (smsSegments > 1 ? `mesaj başına ${smsSegments} parça` : 'izinli alıcı') : 'kanal kapalı' }}</div>
      </div>
      <div class="stat" :class="{ 'stat-off': !enabled('email') }">
        <div class="stat-head"><span class="stat-icon teal"><Icon name="mail" :size="14" /></span><span class="stat-label">E-posta alıcısı</span></div>
        <div class="stat-value">{{ pv && enabled('email') ? num(pv.email_eligible) : '—' }}</div>
        <div class="stat-sub">{{ enabled('email') ? 'izinli alıcı' : 'kanal kapalı' }}</div>
      </div>
      <div class="stat stat-accent">
        <div class="stat-head"><span class="stat-icon green"><Icon name="send" :size="14" /></span><span class="stat-label">Kuyruğa alınacak</span></div>
        <div class="stat-value">{{ num(recipients) }}<span class="unit">mesaj</span></div>
        <div class="stat-sub">kanal başına ≤ 100 mesaj/sn</div>
      </div>
    </div>

    <div class="grid-2" style="align-items: start">
      <div class="card card-flush">
        <div class="card-head">
          <span class="card-icon"><Icon name="filter" :size="17" /></span>
          <h2>Kitle kuralı</h2>
          <button class="btn btn-ghost btn-sm" type="button" @click="emit('go', 'audience')"><Icon name="edit" :size="13" /> Düzenle</button>
        </div>
        <div class="card-body stack-sm">
          <p v-if="campaign.rule?.explanation" class="small">
            <span class="muted">Claude'un yorumu:</span> {{ campaign.rule.explanation }}
          </p>
          <p v-else-if="campaign.rule" class="small muted">Kural elle düzenlendi.</p>
          <ul class="rule-lines">
            <li v-for="l in campaign.rule ? describeRule(campaign.rule) : []" :key="l"><Icon name="check" :size="13" :stroke="2.5" /> {{ l }}</li>
          </ul>
        </div>
      </div>

      <div class="card card-flush">
        <div class="card-head">
          <span class="card-icon teal"><Icon name="layers" :size="17" /></span>
          <h2>Kanallar ve alıcılar</h2>
        </div>
        <div class="card-body stack-sm">
          <div v-for="ch in (['sms', 'email'] as Channel[])" :key="ch" class="channel-row" :class="{ on: enabled(ch) }">
            <span class="card-icon" :class="ch === 'sms' ? 'violet' : 'teal'"><Icon :name="ch === 'sms' ? 'sms' : 'mail'" :size="16" /></span>
            <span style="flex: 1; min-width: 0">
              <strong>{{ ch === 'sms' ? 'SMS' : 'E-posta' }}</strong>
              <span v-if="pv" class="muted small" style="display: block">
                {{ num(ch === 'sms' ? pv.sms_eligible : pv.email_eligible) }} izinli alıcı
                <template v-if="ch === 'sms' && smsSegments > 1"> · mesaj başına {{ smsSegments }} parça</template>
              </span>
            </span>
            <span v-if="enabled(ch) && missingContent.includes(ch)" class="badge badge-danger">İçerik yok</span>
            <label class="switch" :title="enabled(ch) ? 'Kanalı kapat' : 'Kanalı aç'">
              <input
                type="checkbox"
                :aria-label="(ch === 'sms' ? 'SMS' : 'E-posta') + ' kanalı'"
                :checked="enabled(ch)"
                :disabled="channelBusy || (enabled(ch) && campaign.channels.length === 1)"
                @change="toggle(ch, ($event.target as HTMLInputElement).checked)"
              />
              <span class="switch-track" />
            </label>
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
    </div>

    <div v-if="pv" class="card card-flush">
      <div class="card-head">
        <span class="card-icon" :class="pv.warnings.length ? 'amber' : 'green'"><Icon :name="pv.warnings.length ? 'alert' : 'shield'" :size="17" /></span>
        <h2>Uyarılar</h2>
        <span v-if="!pv.warnings.length" class="badge badge-success">Temiz</span>
      </div>
      <div class="card-body">
        <WarningList v-if="pv.warnings.length" :preview="pv" :unmapped="campaign.rule?.unmapped ?? []" />
        <p v-else class="muted">Uyarı yok — kitle ve içerik kontrollerinden geçti.</p>
      </div>
    </div>

    <div v-if="campaign.content" class="card card-flush">
      <div class="card-head">
        <span class="card-icon slate"><Icon name="eye" :size="17" /></span>
        <h2>Gönderilecek mesajlar<span class="sub">Gerçek bir alıcıyla ve ismi boş bir alıcıyla</span></h2>
        <button class="btn btn-ghost btn-sm" type="button" @click="emit('go', 'content')"><Icon name="edit" :size="13" /> Düzenle</button>
      </div>
      <div class="card-body">
        <MessagePreview
          :content="campaign.content"
          :sample="pv?.sample.find((u) => u.first_name) ?? pv?.sample[0] ?? null"
          :channels="campaign.channels"
        />
      </div>
    </div>

    <div class="card stack approve-box" :class="{ blocked: !!blockReason }">
      <label v-if="isLarge" class="checkbox large-confirm">
        <input v-model="confirmLarge" type="checkbox" />
        <span>
          <strong>Geniş kitleyi onaylıyorum.</strong>
          Bu kampanya tüm kullanıcıların yarısından fazlasına ({{ num(pv?.total ?? 0) }} / {{ num(pv?.all_users ?? 0) }}) gidecek.
        </span>
      </label>
      <div v-if="shownDuplicates.length" class="checkbox large-confirm dup-confirm">
        <div>
          <strong>Bu kitledeki kişiler bu kampanyayı zaten almış olabilir.</strong>
          <ul>
            <li v-for="d in shownDuplicates" :key="d.campaign_id + d.channel">
              <RouterLink :to="`/campaigns/${d.campaign_id}`">{{ d.name || 'İsimsiz kampanya' }}</RouterLink>
              ({{ fmtDate(d.approved_at) }}): {{ d.same_text ? `aynı ${CH_LABEL[d.channel]} metni` : `aynı brief'le üretilmiş ${CH_LABEL[d.channel]}` }} bu kitleden {{ num(d.recipients) }} kişiye gitti.
            </li>
          </ul>
          <label class="row" style="gap: 8px; margin-top: 6px">
            <input v-model="confirmDuplicate" type="checkbox" />
            <span>Aynı kişilere tekrar göndermeyi onaylıyorum.</span>
          </label>
        </div>
      </div>
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
        <div class="row" style="gap: 14px; flex-wrap: nowrap">
          <span class="card-icon" :class="blockReason ? 'red' : 'green'"><Icon :name="blockReason ? 'x-circle' : 'send'" :size="18" /></span>
          <div>
            <div style="font-size: 15px"><strong>{{ num(recipients) }}</strong> mesaj kuyruğa alınacak</div>
            <div v-if="blockReason" class="small" style="color: var(--danger)">{{ blockReason }}</div>
            <div v-else class="muted small">Onaydan sonra kampanya düzenlenemez.</div>
          </div>
        </div>
        <button class="btn btn-primary btn-lg" type="button" :disabled="!!blockReason || approving" @click="approve">
          <span v-if="approving" class="spinner" /><Icon v-else name="send" :size="16" /> Onayla ve gönder
        </button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.rule-lines { margin: 0; padding: 0; list-style: none; display: flex; flex-direction: column; gap: 6px; }
.rule-lines li { display: flex; align-items: center; gap: 8px; padding: 8px 10px; background: var(--surface-2); border: 1px solid var(--border); border-radius: var(--radius-xs); font-weight: 500; }
.rule-lines li .icon { color: var(--success); }
.channel-row { display: flex; align-items: center; gap: 12px; padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface-2); transition: border-color 0.12s, background 0.12s; }
.channel-row.on { background: var(--surface); border-color: var(--accent-border); }
.channel-row .card-icon { width: 30px; height: 30px; }
.stat-off { opacity: 0.55; }
.stat-accent { border-color: var(--success-border); background: linear-gradient(180deg, #fff, var(--success-soft)); }
.approve-box { border: 1px solid var(--accent-border); box-shadow: var(--shadow-md); background: linear-gradient(180deg, #fff, #fafaff); }
.approve-box.blocked { border-color: var(--danger-border); background: linear-gradient(180deg, #fff, #fffafa); }
.dup-confirm ul { margin: 6px 0 0 18px; padding: 0; }
.large-confirm { padding: 12px 14px; border-radius: var(--radius-sm); background: var(--warn-soft); border: 1px solid var(--warn-border); color: var(--warn-text); }
</style>
