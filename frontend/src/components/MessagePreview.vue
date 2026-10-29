<script setup lang="ts">
// Rendered previews: a real audience member and the same member without a
// first name, side by side (render is the backend's Render, ported).
import { computed } from 'vue'
import { render } from '../lib/sms'
import type { Channel, Content, SampleUser } from '../types'

const props = defineProps<{ content: Content; sample: SampleUser | null; channels: Channel[]; streaming?: boolean }>()

const variants = computed(() => {
  const city = props.sample?.city ?? 'İstanbul'
  const name = props.sample?.first_name ?? ''
  return [
    { key: 'named', title: name ? `${name} · ${city}` : `Örnek kullanıcı · ${city}`, vars: { first_name: name, city } },
    { key: 'nameless', title: `İsmi olmayan kullanıcı · ${city}`, vars: { first_name: '', city } },
  ]
})
const r = (t: string, vars: Record<string, string>) => render(t, vars)
</script>

<template>
  <div class="grid-2">
    <div v-for="v in variants" :key="v.key" class="stack-sm">
      <div class="section-label">{{ v.title }}</div>
      <div v-if="channels.includes('sms')" class="stack-sm">
        <span class="muted small">SMS</span>
        <div class="preview-sms" :class="{ streaming }">{{ r(content.sms, v.vars) || '—' }}</div>
      </div>
      <div v-if="channels.includes('email')" class="stack-sm">
        <span class="muted small">E-posta</span>
        <div class="preview-mail">
          <div class="preview-mail-head">
            <div class="preview-mail-subject">{{ r(content.email.subject, v.vars) || '—' }}</div>
            <div class="preview-mail-pre">{{ r(content.email.preheader, v.vars) }}</div>
          </div>
          <div class="preview-mail-body" :class="{ streaming }">{{ r(content.email.body, v.vars) }}</div>
        </div>
      </div>
    </div>
  </div>
</template>
