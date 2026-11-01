<script setup lang="ts">
// Rendered previews: a real audience member and the same member without a
// first name, side by side (render is the backend's Render, ported). SMS in a
// phone frame, e-mail in a mail-client frame.
import { computed } from 'vue'
import { render } from '../lib/sms'
import type { Channel, Content, SampleUser } from '../types'
import Icon from './Icon.vue'

const props = defineProps<{ content: Content; sample: SampleUser | null; channels: Channel[]; streaming?: boolean }>()

const variants = computed(() => {
  const city = props.sample?.city ?? 'İstanbul'
  const name = props.sample?.first_name ?? ''
  return [
    { key: 'named', title: name ? `${name} · ${city}` : `Örnek kullanıcı · ${city}`, icon: 'user', vars: { first_name: name, city } },
    { key: 'nameless', title: `İsmi olmayan kullanıcı · ${city}`, icon: 'help', vars: { first_name: '', city } },
  ]
})
const r = (t: string, vars: Record<string, string>) => render(t, vars)
</script>

<template>
  <div class="stack-lg">
    <div v-if="channels.includes('sms')">
      <div class="pv-channel"><Icon name="sms" :size="14" /> SMS</div>
      <div class="grid-2 pv-grid">
        <div v-for="v in variants" :key="v.key" class="stack-sm">
          <div class="pv-variant"><Icon :name="v.icon" :size="13" /> {{ v.title }}</div>
          <div class="phone">
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
        </div>
      </div>
    </div>

    <div v-if="channels.includes('email')">
      <div class="pv-channel"><Icon name="mail" :size="14" /> E-posta</div>
      <div class="grid-2 pv-grid">
        <div v-for="v in variants" :key="v.key" class="stack-sm">
          <div class="pv-variant"><Icon :name="v.icon" :size="13" /> {{ v.title }}</div>
          <div class="preview-mail">
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
</style>
