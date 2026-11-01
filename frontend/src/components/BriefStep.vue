<script setup lang="ts">
import { computed, ref } from 'vue'
import { errorText, errorsByPath } from '../lib/errors'
import { runeCount } from '../lib/sms'
import { appState } from '../store'
import type { Campaign } from '../types'
import Icon from './Icon.vue'

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
  <section v-if="campaign" class="card card-flush">
    <div class="card-head">
      <span class="card-icon"><Icon name="megaphone" :size="17" /></span>
      <h2>Brief<span class="sub">Kampanya oluşturulduktan sonra değiştirilemez</span></h2>
    </div>
    <div class="card-body">
      <p class="brief-text">{{ campaign.brief }}</p>
      <p class="muted small" style="margin-top: 12px">
        Brief kampanya oluşturulduktan sonra değiştirilemez; farklı bir brief için yeni kampanya açın.
      </p>
    </div>
    <div class="card-foot">
      <RouterLink to="/new" class="btn"><Icon name="plus" :size="14" /> Yeni kampanya</RouterLink>
      <button class="btn btn-primary" type="button" @click="emit('next')">Kitleye geç <Icon name="arrow-right" :size="15" /></button>
    </div>
  </section>

  <div v-else class="brief-layout">
    <form class="card card-flush" @submit.prevent="submit">
      <div class="card-head">
        <span class="card-icon"><Icon name="megaphone" :size="17" /></span>
        <h2>Kampanya brief'i<span class="sub">Kime, ne, hangi tonda? Doğal dille yazın.</span></h2>
      </div>
      <div class="card-body stack">
        <div class="field" :class="{ 'has-error': fieldError }">
          <label class="sr-only" for="brief">Kampanya brief'i</label>
          <textarea
            id="brief"
            v-model="brief"
            class="textarea brief-input"
            rows="6"
            :disabled="busy"
            placeholder="Örn. Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder."
          />
          <div class="row-between">
            <span v-if="fieldError" class="field-error">{{ fieldError }}</span>
            <button v-else type="button" class="btn btn-ghost btn-sm" :disabled="busy" @click="brief = EXAMPLE">
              <Icon name="sparkles" :size="13" /> Örnek brief'i kullan
            </button>
            <span class="small tabular" :class="count > MAX ? 'field-error' : 'muted'">{{ count }} / {{ MAX }}</span>
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
      </div>
      <div class="card-foot">
        <span class="muted small">Sonraki adımda Claude kitleyi çıkarır ve kişi sayılarını gösterir.</span>
        <button class="btn btn-primary btn-lg" type="submit" :disabled="busy || count === 0 || count > MAX">
          <span v-if="busy" class="spinner" />
          Kitleyi oluştur <Icon name="arrow-right" :size="16" />
        </button>
      </div>
    </form>

    <aside class="card how">
      <div class="section-label">Nasıl çalışır?</div>
      <ol class="how-list">
        <li>
          <span class="card-icon"><Icon name="filter" :size="15" /></span>
          <div><strong>Kitle kuralı</strong><p>Claude brief'i yalnızca izin verilen alanlardan oluşan bir kurala çevirir: şehir, dil, sepet terki, satın alma, sipariş sayısı, harcama, kayıt tarihi.</p></div>
        </li>
        <li>
          <span class="card-icon amber"><Icon name="alert" :size="15" /></span>
          <div><strong>Sessiz atlama yok</strong><p>Karşılığı olmayan istekler (ör. “VIP”) kurala gizlice eklenmez; ayrıca gösterilir.</p></div>
        </li>
        <li>
          <span class="card-icon violet"><Icon name="sms" :size="15" /></span>
          <div><strong>SMS ve e-posta</strong><p>İçerik canlı akar, kanal limitlerine göre en kötü durumla ölçülür.</p></div>
        </li>
        <li>
          <span class="card-icon green"><Icon name="shield" :size="15" /></span>
          <div><strong>Son karar sizde</strong><p>Onaydan önce hiçbir mesaj gönderilmez; gönderimi canlı izlersiniz.</p></div>
        </li>
      </ol>
    </aside>
  </div>
</template>

<style scoped>
.brief-layout { display: grid; grid-template-columns: minmax(0, 1fr) 320px; gap: var(--space-4); align-items: start; }
.brief-input { font-size: 15px; min-height: 150px; padding: 12px 14px; }
.card-foot { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; padding: 14px var(--space-5); border-top: 1px solid var(--border); background: var(--surface-2); }
.brief-text {
  font-size: 16px;
  line-height: 1.6;
  padding: 14px 18px;
  background: var(--surface-2);
  border-left: 3px solid var(--accent);
  border-radius: var(--radius-sm);
}
.how { padding: var(--space-5); }
.how-list { list-style: none; margin: 8px 0 0; padding: 0; display: flex; flex-direction: column; gap: 16px; }
.how-list li { display: flex; gap: 12px; align-items: flex-start; }
.how-list .card-icon { width: 30px; height: 30px; }
.how-list p { font-size: 12.5px; color: var(--text-3); margin-top: 2px; line-height: 1.5; }
.sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; }
@media (max-width: 1100px) {
  .brief-layout { grid-template-columns: 1fr; }
}
</style>
