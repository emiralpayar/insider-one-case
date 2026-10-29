import { describe, expect, it } from 'vitest'
import { describeCondition, worstCaseCity, defaultCondition, FIELDS, CITIES, validateLocal } from './rule'

describe('whitelist copy', () => {
  it('has the 14 canonical cities and 7 fields', () => {
    expect(CITIES).toHaveLength(14)
    expect(CITIES[0]).toBe('İstanbul')
    expect(Object.keys(FIELDS)).toEqual(['city', 'language', 'abandoned_cart', 'purchased', 'order_count', 'total_spent', 'signed_up'])
  })
  it('default condition uses the first allowed operator and an in-range value', () => {
    expect(defaultCondition('purchased')).toEqual({ field: 'purchased', op: 'within_days', value: 30 })
    expect(defaultCondition('city')).toEqual({ field: 'city', op: 'in', value: ['İstanbul'] })
    expect(defaultCondition('language')).toEqual({ field: 'language', op: 'eq', value: 'tr' })
  })
})

describe('describeCondition', () => {
  it('reads like a sentence', () => {
    expect(describeCondition({ field: 'city', op: 'in', value: ['İstanbul', 'Ankara'] })).toBe('Şehir: İstanbul, Ankara')
    expect(describeCondition({ field: 'abandoned_cart', op: 'within_days', value: 30 })).toBe('Son 30 günde sepete ürün ekleyip almamış')
    expect(describeCondition({ field: 'total_spent', op: 'gte', value: 1500 })).toBe('Toplam harcama en az 1.500 ₺')
  })
})

describe('worstCaseCity', () => {
  it('uses the longest allowed city', () => {
    expect(worstCaseCity({ audience: 'filtered', conditions: [{ field: 'city', op: 'in', value: ['İzmir', 'Ankara'] }], unmapped: [], explanation: '' })).toBe('Ankara')
    expect(worstCaseCity({ audience: 'all', conditions: [], unmapped: [], explanation: '' })).toHaveLength(9)
  })
})

describe('validateLocal', () => {
  it('flags out-of-range numbers and empty city lists by path', () => {
    const errs = validateLocal({ audience: 'filtered', unmapped: [], explanation: '', conditions: [
      { field: 'order_count', op: 'gte', value: 5000 },
      { field: 'city', op: 'in', value: [] },
    ] })
    expect(errs.map((e) => e.path)).toEqual(['conditions[0].value', 'conditions[1].value'])
  })
  it('rejects a filtered rule without conditions; "all" must be explicit', () => {
    expect(validateLocal({ audience: 'filtered', unmapped: [], explanation: '', conditions: [] }).map((e) => e.code)).toEqual(['empty_conditions'])
    expect(validateLocal({ audience: 'all', unmapped: [], explanation: '', conditions: [] })).toEqual([])
  })
})
