<script setup lang="ts">
import { onMounted } from 'vue'
import { appState, loadConfig } from './store'

onMounted(loadConfig)
</script>

<template>
  <div
    v-if="appState.config && !appState.config.llm_configured"
    class="global-banner danger"
    role="alert"
  >
    Claude API key tanımlı değil — içerik üretilemez
    <span class="sub">(ANTHROPIC_API_KEY ayarlanmadan segment ve içerik üretimi çalışmaz)</span>
  </div>
  <div v-else-if="appState.config?.llm_provider === 'mock'" class="global-banner mock" role="status">
    MOCK MOD — içerik Claude tarafından üretilmedi
    <span class="sub">(LLM_PROVIDER=mock: deterministik test çıktısı)</span>
  </div>
  <div v-if="appState.configError" class="global-banner danger" role="alert">
    Sunucuya ulaşılamadı — backend çalışıyor mu?
  </div>

  <header class="app-header">
    <div class="app-header-inner">
      <RouterLink to="/" class="brand">
        <span class="brand-logo">✦</span>
        Smart Campaign Composer
      </RouterLink>
      <nav class="nav">
        <RouterLink to="/">Kampanyalar</RouterLink>
        <RouterLink to="/new">Yeni kampanya</RouterLink>
      </nav>
      <span class="spacer" />
      <span v-if="appState.config?.llm_configured" class="muted small nowrap">
        Model: {{ appState.config.llm_provider === 'mock' ? 'mock' : appState.config.models.content }}
      </span>
    </div>
  </header>

  <main class="container">
    <RouterView />
  </main>
</template>
