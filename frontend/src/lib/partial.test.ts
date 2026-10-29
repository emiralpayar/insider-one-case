import { describe, expect, it } from 'vitest'
import { applyPartial, emptyContent } from './partial'

const base = { sms: 'eski sms', email: { subject: 'eski konu', preheader: 'eski ön', body: 'eski gövde' } }
const p = { sms: 'yeni', subject: 'Ko', preheader: '', body: '' }

describe('applyPartial', () => {
  it('fills every generated channel', () => {
    expect(applyPartial(emptyContent(), p, ['sms', 'email'])).toEqual({
      sms: 'yeni', email: { subject: 'Ko', preheader: '', body: '' },
    })
  })
  it('keeps the other channel when one channel is regenerated', () => {
    expect(applyPartial(base, p, ['sms'])).toEqual({ ...base, sms: 'yeni' })
    expect(applyPartial(base, p, ['email']).sms).toBe('eski sms')
    expect(applyPartial(base, p, ['email']).email.subject).toBe('Ko')
  })
  it('does not mutate its input', () => {
    applyPartial(base, p, ['sms', 'email'])
    expect(base.sms).toBe('eski sms')
  })
})
