import { describe, expect, it } from 'vitest'
import { NAME_MAX, nameError, normalizeName } from './name'

describe('campaign name', () => {
  it('collapses whitespace like the backend', () => {
    expect(normalizeName('  Bahar \n  kampanyası ')).toBe('Bahar kampanyası')
  })
  it('rejects empty and too-long names', () => {
    expect(nameError('   ')).toBe('Kampanya adı boş olamaz')
    expect(nameError('ğ'.repeat(NAME_MAX))).toBeNull()
    expect(nameError('ğ'.repeat(NAME_MAX + 1))).toContain('en fazla 120')
    expect(nameError('İstanbul sepet hatırlatma')).toBeNull()
  })
})
