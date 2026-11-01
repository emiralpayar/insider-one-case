// Helpers for the server-paginated lists: the audience list ("Kitleyi gör")
// and the per-recipient message table on the dashboard.
import type { Channel, MessageStatus } from '../types'

/** Message statuses with the dashboard's Turkish labels (colors: .bg-<key>). */
export const MESSAGE_STATUSES: readonly { key: MessageStatus; label: string; tip: string }[] = [
  { key: 'sent', label: 'Gönderildi', tip: 'Provider kabul etti' },
  { key: 'sending', label: 'Gönderiliyor', tip: 'Şu an bir worker tarafından işleniyor' },
  { key: 'queued', label: 'Kuyrukta', tip: 'Sırasını bekliyor (yeniden deneme dahil)' },
  { key: 'failed', label: 'Başarısız', tip: 'Kalıcı hata ya da deneme hakkı bitti' },
  { key: 'unknown', label: 'Belirsiz', tip: 'Gönderilmiş olabilir; çift gönderim riskine karşı tekrar denenmedi' },
  { key: 'skipped_no_consent', label: 'İzin yok, atlandı', tip: 'Gönderimden hemen önce kullanıcının izni kalkmış' },
]

export const MESSAGE_STATUS_LABEL: Record<string, string> = Object.fromEntries(MESSAGE_STATUSES.map((s) => [s.key, s.label]))

/** "Gitmeyenler": messages that did not (or may not have) reached the recipient. */
export const NOT_DELIVERED: readonly MessageStatus[] = ['failed', 'unknown', 'skipped_no_consent']

export interface MessageFilter {
  statuses: MessageStatus[]
  channel: '' | Channel
  page: number
}

/** Query string for GET /campaigns/{id}/messages ("" when nothing is set). */
export function messagesQuery(f: MessageFilter): string {
  const p = new URLSearchParams()
  if (f.statuses.length) p.set('status', [...f.statuses].join(','))
  if (f.channel) p.set('channel', f.channel)
  if (f.page > 1) p.set('page', String(f.page))
  const s = p.toString()
  return s ? '?' + s : ''
}

/** Same set of statuses, ignoring order. */
export function sameStatuses(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((s) => b.includes(s))
}

/** Toggles one status in a filter set (order follows MESSAGE_STATUSES). */
export function toggleStatus(set: readonly MessageStatus[], s: MessageStatus): MessageStatus[] {
  const next = set.includes(s) ? set.filter((x) => x !== s) : [...set, s]
  return MESSAGE_STATUSES.map((x) => x.key).filter((k) => next.includes(k))
}

/** Number of pages (at least 1). */
export function pageCount(total: number, perPage: number): number {
  return perPage > 0 ? Math.max(1, Math.ceil(total / perPage)) : 1
}

/** "51–100 / 648" for the rows a page shows; "0" when empty. */
export function rangeText(page: number, perPage: number, total: number): string {
  if (total <= 0) return '0'
  const from = (page - 1) * perPage + 1
  const to = Math.min(total, page * perPage)
  const n = (x: number) => x.toLocaleString('tr-TR')
  return `${n(from)}–${n(to)} / ${n(total)}`
}

/** "Ad Soyad"; "(isimsiz)" when both are empty. */
export function fullName(first: string, last: string): string {
  return [first, last].map((s) => s.trim()).filter(Boolean).join(' ') || '(isimsiz)'
}

/** Turkish lira amount without decimals for whole numbers: "₺1.234" / "₺12,50". */
export function money(n: number): string {
  return '₺' + n.toLocaleString('tr-TR', { minimumFractionDigits: Number.isInteger(n) ? 0 : 2, maximumFractionDigits: 2 })
}
