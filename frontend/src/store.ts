// App-wide state: the backend's LLM configuration (drives the global banner).
import { reactive } from 'vue'
import { api } from './api'
import type { AppConfig } from './types'

export const appState = reactive({
  config: null as AppConfig | null,
  configError: false,
})

export async function loadConfig(): Promise<void> {
  try {
    appState.config = await api.config()
    appState.configError = false
  } catch {
    appState.configError = true
  }
}
