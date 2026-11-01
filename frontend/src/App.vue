<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import Icon from './components/Icon.vue'
import { appState, loadConfig } from './store'

onMounted(loadConfig)

const route = useRoute()
// Sidebar section: the composer (new / edit) or the campaign list and dashboards.
const section = computed(() => (route.name === 'new' || route.name === 'edit' ? 'compose' : 'campaigns'))
const llm = computed(() => {
  const c = appState.config
  if (!c) return { tone: 'off', name: appState.configError ? 'Sunucu yok' : 'Bağlanıyor…', model: '' }
  if (!c.llm_configured) return { tone: 'off', name: 'LLM yapılandırılmamış', model: 'ANTHROPIC_API_KEY yok' }
  if (c.llm_provider === 'mock') return { tone: 'mock', name: 'Mock LLM', model: 'deterministik fixture' }
  return { tone: '', name: 'Claude bağlı', model: c.models.content }
})
</script>

<template>
  <div
    v-if="appState.config && !appState.config.llm_configured"
    class="global-banner danger"
    role="alert"
  >
    <Icon name="alert" :size="18" />
    Claude API key tanımlı değil — içerik üretilemez
    <span class="sub">(ANTHROPIC_API_KEY ayarlanmadan segment ve içerik üretimi çalışmaz)</span>
  </div>
  <div v-else-if="appState.config?.llm_provider === 'mock'" class="global-banner mock" role="status">
    <Icon name="alert" :size="18" />
    MOCK MOD — içerik Claude tarafından üretilmedi
    <span class="sub">(LLM_PROVIDER=mock: deterministik test çıktısı)</span>
  </div>
  <div v-if="appState.configError" class="global-banner danger" role="alert">
    <Icon name="alert" :size="18" />
    Sunucuya ulaşılamadı — backend çalışıyor mu?
  </div>

  <div class="shell">
    <aside class="sidebar">
      <div class="sidebar-inner">
      <RouterLink to="/" class="brand" title="Smart Campaign Composer">
        <span class="brand-logo"><Icon name="sparkles" :size="18" /></span>
        <span class="brand-text">
          Campaign Composer
          <span class="brand-sub">Claude ile akıllı kampanyalar</span>
        </span>
      </RouterLink>

      <nav aria-label="Ana menü">
        <div class="side-label">Pazarlama</div>
        <div class="side-nav">
          <RouterLink to="/" :class="{ active: section === 'campaigns' }" title="Kampanyalar">
            <Icon name="megaphone" :size="18" /><span>Kampanyalar</span>
          </RouterLink>
          <RouterLink to="/new" :class="{ active: section === 'compose' }" title="Yeni kampanya">
            <Icon name="plus" :size="18" /><span>Yeni kampanya</span>
          </RouterLink>
        </div>
      </nav>

      <div class="side-foot">
        <div class="llm-card" :title="llm.name + (llm.model ? ' · ' + llm.model : '')">
          <div class="row">
            <span class="llm-dot" :class="llm.tone" />
            <span class="llm-name hide-rail">{{ llm.name }}</span>
          </div>
          <div v-if="llm.model" class="llm-model hide-rail">{{ llm.model }}</div>
        </div>
      </div>
      </div>
    </aside>

    <div class="main">
      <main class="container">
        <RouterView />
      </main>
    </div>
  </div>
</template>
