<script setup lang="ts">
import { computed, ref } from 'vue'
import { errorText, errorsByPath } from '../lib/errors'
import { runeCount } from '../lib/sms'
import { appState } from '../store'
import type { Campaign } from '../types'

const props = defineProps<{ campaign: Campaign | null; busy: boolean; error: unknown }>()
const emit = defineEmits<{ submit: [brief: string]; next: [] }>()

const EXAMPLE =
  "Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder. Samimi ama kısa olsun."
const MAX = 2000

const brief = ref('')
const count = computed(() => runeCount(brief.value.trim()))
const fieldError = computed(() => errorsByPath(props.error).brief?.[0] ?? '')
const llmMissing = computed(() => appState.config?.llm_configured === false)

function submit() {
  if (count.value === 0 || count.value > MAX || props.busy) return
  emit('submit', brief.value.trim())
}
</script>

<template>
  <section class="card">
    <template v-if="campaign">
      <div class="section-label">Brief</div>
      <p class="brief-text">{{ campaign.brief }}</p>
      <p class="muted small" style="margin-top: 12px">
        Brief kampanya oluşturulduktan sonra değiştirilemez; farklı bir brief için yeni kampanya açın.
      </p>
      <div class="footer-bar">
        <RouterLink to="/new" class="btn">Yeni kampanya</RouterLink>
        <button class="btn btn-primary" type="button" @click="emit('next')">Kitleye geç →</button>
      </div>
    </template>

    <form v-else class="stack" @submit.prevent="submit">
      <div class="field" :class="{ 'has-error': fieldError }">
        <label class="field-label" for="brief">Kampanya brief'i</label>
        <textarea
          id="brief"
          v-model="brief"
          class="textarea"
          rows="5"
          :disabled="busy"
          placeholder="Örn. Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder."
        />
        <div class="row-between">
          <span v-if="fieldError" class="field-error">{{ fieldError }}</span>
          <button v-else type="button" class="btn btn-ghost btn-sm" :disabled="busy" @click="brief = EXAMPLE">
            Örnek brief'i kullan
          </button>
          <span class="small" :class="count > MAX ? 'field-error' : 'muted'">{{ count }} / {{ MAX }}</span>
        </div>
      </div>

      <div class="alert alert-info">
        <span class="alert-icon">i</span>
        <div class="alert-body">
          Claude brief'i yalnızca izin verilen alanlardan oluşan bir kitle kuralına çevirir (şehir, dil, sepet terki,
          satın alma, sipariş sayısı, harcama, kayıt tarihi). Karşılığı olmayan istekler sessizce atlanmaz; ayrıca
          gösterilir.
        </div>
      </div>

      <div v-if="error && !fieldError" class="alert alert-danger">
        <span class="alert-icon">!</span>
        <div class="alert-body">{{ errorText(error) }}</div>
      </div>
      <div v-if="llmMissing" class="alert alert-warn">
        <span class="alert-icon">!</span>
        <div class="alert-body">
          Claude yapılandırılmadığı için kitle otomatik çıkarılamaz; kampanyayı oluşturup kuralı elle kurabilirsiniz.
        </div>
      </div>

      <div class="row-between">
        <span class="muted small">Sonraki adımda Claude kitleyi çıkarır ve kişi sayılarını gösterir.</span>
        <button class="btn btn-primary btn-lg" type="submit" :disabled="busy || count === 0 || count > MAX">
          <span v-if="busy" class="spinner" />
          Kitleyi oluştur →
        </button>
      </div>
    </form>
  </section>
</template>

<style scoped>
.brief-text {
  font-size: 16px;
  line-height: 1.55;
  padding: 14px 16px;
  background: var(--surface-2);
  border-left: 3px solid var(--accent);
  border-radius: var(--radius-sm);
}
</style>
