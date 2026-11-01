// Message preview personas and tab selection (İçerik adımı).
import type { Channel, SampleUser } from '../types'

export type PersonaKey = 'named' | 'nameless'

export interface Persona {
  key: PersonaKey
  /** Short tab label: the sample's first name, or "İsimsiz". */
  label: string
  /** Longer caption: "Merve · İstanbul". */
  title: string
  icon: string
  vars: { first_name: string; city: string }
}

/** A real audience member and the same member without a first name. */
export function personas(sample: SampleUser | null): Persona[] {
  const city = sample?.city ?? 'İstanbul'
  const name = sample?.first_name ?? ''
  return [
    {
      key: 'named',
      label: name || 'Örnek kullanıcı',
      title: name ? `${name} · ${city}` : `Örnek kullanıcı · ${city}`,
      icon: 'user',
      vars: { first_name: name, city },
    },
    { key: 'nameless', label: 'İsimsiz', title: `İsmi olmayan kullanıcı · ${city}`, icon: 'help', vars: { first_name: '', city } },
  ]
}

/**
 * The channel tab to show: the one being regenerated alone, else the current
 * one if still enabled, else the first enabled channel.
 */
export function pickChannel(enabled: readonly Channel[], current: Channel | null, streaming: 'all' | Channel | null = null): Channel | null {
  if (streaming && streaming !== 'all' && enabled.includes(streaming)) return streaming
  if (current && enabled.includes(current)) return current
  return enabled[0] ?? null
}
