import { describe, expect, it } from 'vitest'
import type { MessageError } from '../types'
import { freshRows, humanReason, outcomeText, prependRow, revealDelay, rowKey } from './log'

const err = (id: string, status: string, attempts: number, error = 'HTTP 503: provider unavailable'): MessageError => ({
  message_id: id, user_id: 1, channel: 'sms', status, attempts, error, at: '2026-11-01T10:00:00Z',
})
const row = (e: MessageError) => ({ ...e, key: rowKey(e) })

describe('humanReason', () => {
  it.each([
    ['HTTP 429', 'Hız sınırı (429)'],
    ['HTTP 503: provider unavailable', 'Sağlayıcı geçici olarak kullanılamıyor (503)'],
    ['HTTP 502: bad gateway', 'Ağ geçidi hatası (502)'],
    ['Post "http://mock-provider:8081/v1/sms": context deadline exceeded (Client.Timeout exceeded while awaiting headers)', 'Zaman aşımı'],
    ['Post "http://x": net/http: request canceled (Client.Timeout exceeded)', 'Zaman aşımı'],
    ['dial tcp: connection refused', 'dial tcp: connection refused'],
    ['HTTP 400: bad request', 'HTTP 400: bad request'],
  ])('%s', (raw, want) => expect(humanReason(raw)).toBe(want))
})

describe('outcomeText', () => {
  it('phrases each status', () => {
    expect(outcomeText('sent', 2)).toBe('2. denemede gönderildi')
    expect(outcomeText('sent', 1)).toBe('Yeniden denemede gönderildi')
    expect(outcomeText('failed', 5)).toBe('Kalıcı hata')
    expect(outcomeText('unknown', 1)).toBe('Belirsiz — tekrar denenmedi')
    expect(outcomeText('queued', 1)).toBe('Yeniden denenecek')
    expect(outcomeText('sending', 2)).toBe('Yeniden denenecek')
    expect(outcomeText('weird', 1)).toBe('weird')
  })
})

describe('freshRows / prependRow', () => {
  it('returns only rows not on screen, newest first', () => {
    const visible = [row(err('a', 'queued', 1))]
    const fresh = freshRows(visible, [err('b', 'queued', 1), err('a', 'queued', 1)])
    expect(fresh.map((r) => r.message_id)).toEqual(['b'])
  })
  it('a changed message counts as fresh and replaces its old row at the top', () => {
    const a1 = row(err('a', 'queued', 1))
    const b1 = row(err('b', 'queued', 1))
    let rows = [b1, a1]
    const fresh = freshRows(rows, [err('a', 'sent', 2), err('b', 'queued', 1)])
    expect(fresh.map((r) => r.key)).toEqual([rowKey(err('a', 'sent', 2))])
    rows = prependRow(rows, fresh[0])
    expect(rows.map((r) => r.message_id)).toEqual(['a', 'b'])
    expect(rows[1]).toBe(b1) // untouched rows keep identity
  })
  it('ignores duplicate ids within one poll and caps the list', () => {
    expect(freshRows([], [err('a', 'sent', 2), err('a', 'queued', 1)])).toHaveLength(1)
    let rows: ReturnType<typeof row>[] = []
    for (let i = 0; i < 10; i++) rows = prependRow(rows, row(err(String(i), 'failed', 1)), 5)
    expect(rows.map((r) => r.message_id)).toEqual(['9', '8', '7', '6', '5'])
  })
})

describe('revealDelay', () => {
  it('spreads a batch over the poll interval within bounds', () => {
    expect(revealDelay(1, 2000)).toBe(0)
    expect(revealDelay(4, 2000)).toBe(400)
    expect(revealDelay(10, 2000)).toBe(160)
    expect(revealDelay(100, 2000)).toBe(40)
  })
})

describe('humanReason queue notes', () => {
  it('explains the 429 cap and possibly delivered attempts', () => {
    expect(humanReason('HTTP 429 Too Many Requests; gave up after 20 rate-limited answers')).toBe(
      'Hız sınırı (429) · 20 kez hız sınırına takıldı, vazgeçildi',
    )
    expect(humanReason('HTTP 400 Bad Request; an earlier attempt may have been delivered')).toBe(
      'HTTP 400 Bad Request · önceki deneme ulaşmış olabilir',
    )
    expect(humanReason('worker crashed mid-send')).toBe('Worker gönderim sırasında durdu')
    expect(humanReason('consent revoked after a possibly delivered attempt')).toBe('İzin geri çekildi · önceki deneme ulaşmış olabilir')
    expect(humanReason('crashed mid-send; consent since revoked')).toBe('Worker gönderim sırasında durdu · izin sonra geri çekildi')
  })
})

