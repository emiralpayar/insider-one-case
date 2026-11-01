<script setup lang="ts">
// Rendered previews of a real audience member and the same member without a
// first name (render is the backend's Render, ported). SMS in a phone frame,
// e-mail in a mail-client frame.
// - default: every enabled channel × both personas (Onay adımı)
// - tabbed: one preview at a time, with channel (enabled channels only) and
//   persona switches (İçerik adımı, sticky column)
import { computed, ref, watch } from 'vue'
import { personas, pickChannel, type PersonaKey } from '../lib/preview'
import { render } from '../lib/sms'
import type { Channel, Content, SampleUser } from '../types'
import Icon from './Icon.vue'

const props = defineProps<{
  content: Content
  sample: SampleUser | null
  channels: Channel[]
  streaming?: boolean
  /** Which generation is running ('all' or a single channel): the tab follows a single-channel one. */
  streamingWhich?: 'all' | Channel | null
  tabbed?: boolean
}>()

const ORDER: Channel[] = ['sms', 'email']
const CH = { sms: { label: 'SMS', icon: 'sms' }, email: { label: 'E-posta', icon: 'mail' } } as const

const enabled = computed(() => ORDER.filter((c) => props.channels.includes(c)))
const variants = computed(() => personas(props.sample))

const channel = ref<Channel | null>(pickChannel(enabled.value, null, props.streamingWhich ?? null))
watch(
  [enabled, () => props.streamingWhich],
  () => (channel.value = pickChannel(enabled.value, channel.value, props.streamingWhich ?? null)),
)
const persona = ref<PersonaKey>('named')

const shownChannels = computed(() => (props.tabbed ? (channel.value ? [channel.value] : []) : enabled.value))
const shownVariants = computed(() => (props.tabbed ? variants.value.filter((v) => v.key === persona.value) : variants.value))
const r = (t: string, vars: Record<string, string>) => render(t, vars)
</script>

<template>
  <div class="stack-lg">
    <div v-if="tabbed && enabled.length" class="pv-toolbar">
      <div v-if="enabled.length > 1" class="segmented" role="tablist" aria-label="Kanal">
        <button
          v-for="c in enabled"
          :key="c"
          type="button"
          role="tab"
          :aria-selected="channel === c"
          @click="channel = c"
        >
          <Icon :name="CH[c].icon" :size="13" /> {{ CH[c].label }}
        </button>
      </div>
      <span v-else class="pv-channel" style="margin: 0"><Icon :name="CH[enabled[0]].icon" :size="14" /> {{ CH[enabled[0]].label }}</span>
      <div class="segmented" role="tablist" aria-label="Alıcı">
        <button
          v-for="v in variants"
          :key="v.key"
          type="button"
          role="tab"
          :aria-selected="persona === v.key"
          :title="v.title"
          @click="persona = v.key"
        >
          <Icon :name="v.icon" :size="13" /> {{ v.label }}
        </button>
      </div>
    </div>

    <div v-for="ch in shownChannels" :key="ch">
      <div v-if="!tabbed" class="pv-channel"><Icon :name="CH[ch].icon" :size="14" /> {{ CH[ch].label }}</div>
      <div :class="tabbed ? 'pv-single' : 'grid-2 pv-grid'">
        <div v-for="v in shownVariants" :key="v.key" class="stack-sm">
          <div class="pv-variant"><Icon :name="v.icon" :size="13" /> {{ v.title }}</div>

          <div v-if="ch === 'sms'" class="phone">
            <div class="phone-screen">
              <div class="phone-top">
                <span class="phone-notch" />
                <span class="phone-avatar">M</span>
                <span class="phone-sender">MARKA</span>
              </div>
              <div class="phone-body">
                <div class="phone-time">Bugün</div>
                <div v-if="content.sms" class="preview-sms" :class="{ streaming }">{{ r(content.sms, v.vars) }}</div>
                <div v-else-if="streaming" class="preview-sms" style="width: 70%"><span class="skeleton" /><span class="skeleton" style="width: 60%" /></div>
                <div v-else class="preview-sms preview-empty">SMS metni yok</div>
              </div>
            </div>
          </div>

          <div v-else class="preview-mail">
            <div class="mail-chrome"><i /><i /><i /><span>Gelen kutusu</span></div>
            <div class="preview-mail-head">
              <span class="mail-avatar">M</span>
              <div style="min-width: 0">
                <div class="mail-from"><strong>Marka</strong> &lt;kampanya@marka.com&gt;</div>
                <div class="preview-mail-subject">
                  <template v-if="content.email.subject">{{ r(content.email.subject, v.vars) }}</template>
                  <span v-else class="preview-empty">Konu yok</span>
                </div>
                <div class="preview-mail-pre">{{ r(content.email.preheader, v.vars) }}</div>
              </div>
            </div>
            <div v-if="content.email.body" class="preview-mail-body" :class="{ streaming }">{{ r(content.email.body, v.vars) }}</div>
            <div v-else-if="streaming" class="preview-mail-body"><span class="skeleton" /><span class="skeleton" style="width: 80%" /><span class="skeleton" style="width: 55%" /></div>
            <div v-else class="preview-mail-body preview-empty">Gövde yok</div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.pv-channel { display: flex; align-items: center; gap: 6px; font-size: 11.5px; font-weight: 650; letter-spacing: 0.07em; text-transform: uppercase; color: var(--text-3); margin-bottom: 10px; }
.pv-variant { display: flex; align-items: center; gap: 6px; font-size: 12.5px; font-weight: 600; color: var(--text-2); }
.pv-grid { align-items: start; }
.pv-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 8px; flex-wrap: wrap; }
</style>
