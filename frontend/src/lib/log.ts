// "Gönderim günlüğü": turning the status endpoint's recent_errors (newest
// first, up to 50, including messages later sent) into a stable client-side
// log. A row is identified by message id + attempts + status, so a message
// that changes state appears again at the top instead of repainting in place.
import type { MessageError } from '../types'

export interface LogRow extends MessageError {
  /** Stable identity for the list (TransitionGroup key). */
  key: string
}

export const rowKey = (e: MessageError) => `${e.message_id}:${e.attempts}:${e.status}:${e.error}`

/** Human-readable reason for a worker/provider error string. */
export function humanReason(raw: string): string {
  const s = raw.trim()
  const http = /\bHTTP (\d{3})\b/.exec(s)
  if (http) {
    switch (http[1]) {
      case '429':
        return 'Hız sınırı (429)'
      case '503':
        return 'Sağlayıcı geçici olarak kullanılamıyor (503)'
      case '502':
        return 'Ağ geçidi hatası (502)'
      case '504':
        return 'Ağ geçidi zaman aşımı (504)'
    }
  }
  if (/context deadline exceeded|Client\.Timeout/i.test(s)) return 'Zaman aşımı'
  return s
}

/** One-line outcome for a row. */
export function outcomeText(status: string, attempts: number): string {
  switch (status) {
    case 'sent':
      // attempts can stay 1 after a 429 (rate limits don't use up an attempt).
      return attempts > 1 ? `${attempts}. denemede gönderildi` : 'Yeniden denemede gönderildi'
    case 'failed':
      return 'Kalıcı hata'
    case 'unknown':
      return 'Belirsiz — tekrar denenmedi'
    case 'queued':
    case 'sending':
      return 'Yeniden denenecek'
    case 'skipped_no_consent':
      return 'İzin yok, atlandı'
  }
  return status
}

/**
 * The rows of a poll that are not on screen yet, newest first. A message
 * whose status/attempts/error changed counts as new (it moves to the top).
 */
export function freshRows(visible: readonly LogRow[], incoming: readonly MessageError[]): LogRow[] {
  const seen = new Set(visible.map((r) => r.key))
  const out: LogRow[] = []
  const ids = new Set<string>()
  for (const e of incoming) {
    if (ids.has(e.message_id)) continue
    ids.add(e.message_id)
    const key = rowKey(e)
    if (!seen.has(key)) out.push({ ...e, key })
  }
  return out
}

/**
 * Puts row at the top of the list, dropping the older row of the same
 * message, and caps the list. Other rows keep their identity and order.
 */
export function prependRow(rows: readonly LogRow[], row: LogRow, cap = 200): LogRow[] {
  return [row, ...rows.filter((r) => r.message_id !== row.message_id)].slice(0, cap)
}

/** Delay between revealed rows so one poll's batch spreads over the interval. */
export function revealDelay(count: number, intervalMs: number): number {
  if (count <= 1) return 0
  return Math.max(40, Math.min(400, Math.floor((intervalMs * 0.8) / count)))
}
