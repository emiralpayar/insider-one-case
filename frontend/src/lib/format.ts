import type { CampaignStatus, Warning } from '../types'

export const CAMPAIGN_STATUS: Record<CampaignStatus, string> = {
  draft: 'Taslak',
  audience_ready: 'Kitle hazır',
  content_ready: 'İçerik hazır',
  sending: 'Gönderiliyor',
  completed: 'Tamamlandı',
}

export const CHANNEL_LABEL: Record<string, string> = { sms: 'SMS', email: 'E-posta' }

export const num = (n: number) => n.toLocaleString('tr-TR')

export const pct = (part: number, whole: number) =>
  whole > 0 ? `%${((part / whole) * 100).toLocaleString('tr-TR', { maximumFractionDigits: 1 })}` : '%0'

export function dateTime(iso: string | null): string {
  if (!iso) return '—'
  return new Date(iso).toLocaleString('tr-TR', { dateStyle: 'medium', timeStyle: 'short' })
}

export function time(iso: string): string {
  return new Date(iso).toLocaleTimeString('tr-TR')
}

/** Turkish text for an audience warning (the backend's messages are English). */
export function warningText(w: Warning, english: number, total: number, all: number): string {
  switch (w.code) {
    case 'empty_audience':
      return 'Bu kurala uyan kullanıcı yok — kampanya onaylanamaz.'
    case 'large_audience':
      return `Kitle tüm kullanıcıların yarısından fazla (${num(total)} / ${num(all)}, ${pct(total, all)}). Onayda ayrıca teyit istenir.`
    case 'unmapped':
      return 'Brief\'in bazı kısımları kurala çevrilemedi (aşağıda listelendi).'
    case 'mixed_language':
      return `Kitlede İngilizce dilli ${num(english)} kullanıcı var; içerik Türkçe gidecek.`
  }
  return w.message
}

export const WARNING_LABEL: Record<string, string> = {
  empty_audience: 'Boş kitle',
  large_audience: 'Geniş kitle',
  unmapped: 'Eşlenemeyen istek',
  mixed_language: 'Karışık dil',
  ineffective_condition: 'Etkisiz koşul',
}

/** danger: blocks approval; warn: needs attention; info: notice. */
export function warningLevel(code: string): 'danger' | 'warn' | 'info' {
  if (code === 'empty_audience') return 'danger'
  if (code === 'mixed_language') return 'info'
  return 'warn'
}
