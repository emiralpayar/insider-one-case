import { describe, expect, it } from 'vitest'
import { personas, pickChannel } from './preview'

describe('personas', () => {
  it('uses the sample user and a nameless copy in the same city', () => {
    const [named, nameless] = personas({ id: 1, first_name: 'Merve', city: 'İzmir' })
    expect(named).toMatchObject({ key: 'named', label: 'Merve', title: 'Merve · İzmir', vars: { first_name: 'Merve', city: 'İzmir' } })
    expect(nameless).toMatchObject({ key: 'nameless', label: 'İsimsiz', vars: { first_name: '', city: 'İzmir' } })
  })
  it('falls back without a sample', () => {
    const [named] = personas(null)
    expect(named.label).toBe('Örnek kullanıcı')
    expect(named.vars).toEqual({ first_name: '', city: 'İstanbul' })
  })
})

describe('pickChannel', () => {
  it('keeps the current tab while it is enabled', () => {
    expect(pickChannel(['sms', 'email'], 'email')).toBe('email')
  })
  it('moves to the first enabled channel when the current one is off', () => {
    expect(pickChannel(['email'], 'sms')).toBe('email')
    expect(pickChannel(['sms', 'email'], null)).toBe('sms')
    expect(pickChannel([], 'sms')).toBeNull()
  })
  it('follows a single-channel regeneration', () => {
    expect(pickChannel(['sms', 'email'], 'sms', 'email')).toBe('email')
    expect(pickChannel(['sms', 'email'], 'email', 'all')).toBe('email')
  })
})
