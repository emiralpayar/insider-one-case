import { describe, expect, it } from 'vitest'
import {
  fullName, MESSAGE_STATUS_LABEL, MESSAGE_STATUSES, messagesQuery, money, NOT_DELIVERED, pageCount, rangeText, sameStatuses, toggleStatus,
} from './lists'

describe('message statuses', () => {
  it('covers every backend status with a Turkish label', () => {
    expect(MESSAGE_STATUSES.map((s) => s.key)).toEqual(['sent', 'sending', 'queued', 'failed', 'unknown', 'skipped_no_consent'])
    expect(MESSAGE_STATUS_LABEL.failed).toBe('Başarısız')
    expect(MESSAGE_STATUS_LABEL.skipped_no_consent).toBe('İzin yok, atlandı')
  })
  it('"Gitmeyenler" is failed + unknown + skipped_no_consent', () => {
    expect([...NOT_DELIVERED]).toEqual(['failed', 'unknown', 'skipped_no_consent'])
  })
})

describe('messagesQuery', () => {
  it('is empty without filters on page 1', () => {
    expect(messagesQuery({ statuses: [], channel: '', page: 1 })).toBe('')
  })
  it('joins statuses and adds channel and page', () => {
    expect(messagesQuery({ statuses: [...NOT_DELIVERED], channel: 'sms', page: 3 })).toBe(
      '?status=failed%2Cunknown%2Cskipped_no_consent&channel=sms&page=3',
    )
    expect(messagesQuery({ statuses: ['sent'], channel: '', page: 1 })).toBe('?status=sent')
  })
})

describe('status set helpers', () => {
  it('compares ignoring order', () => {
    expect(sameStatuses(['unknown', 'failed', 'skipped_no_consent'], NOT_DELIVERED)).toBe(true)
    expect(sameStatuses(['failed'], NOT_DELIVERED)).toBe(false)
    expect(sameStatuses([], [])).toBe(true)
  })
  it('toggles and keeps canonical order', () => {
    expect(toggleStatus(['failed'], 'sent')).toEqual(['sent', 'failed'])
    expect(toggleStatus(['sent', 'failed'], 'sent')).toEqual(['failed'])
  })
})

describe('paging', () => {
  it('counts pages, at least one', () => {
    expect(pageCount(0, 50)).toBe(1)
    expect(pageCount(50, 50)).toBe(1)
    expect(pageCount(51, 50)).toBe(2)
    expect(pageCount(1091, 50)).toBe(22)
  })
  it('describes the visible range', () => {
    expect(rangeText(1, 50, 648)).toBe('1–50 / 648')
    expect(rangeText(13, 50, 648)).toBe('601–648 / 648')
    expect(rangeText(1, 50, 1091)).toBe('1–50 / 1.091')
    expect(rangeText(1, 50, 0)).toBe('0')
  })
})

describe('formatting', () => {
  it('joins names, marks nameless users', () => {
    expect(fullName('Merve', 'Yılmaz')).toBe('Merve Yılmaz')
    expect(fullName('', 'Yılmaz')).toBe('Yılmaz')
    expect(fullName(' ', '')).toBe('(isimsiz)')
  })
  it('formats lira amounts', () => {
    expect(money(0)).toBe('₺0')
    expect(money(1234)).toBe('₺1.234')
    expect(money(12.5)).toBe('₺12,50')
  })
})
