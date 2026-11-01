// TS copy of the segment whitelist (backend/internal/segment/rule.go) and the
// canonical city list (backend/internal/geo/cities.go), with Turkish labels.
import type { Condition, Rule, VError } from '../types'

export const CITIES = [
  'İstanbul', 'Ankara', 'İzmir', 'Bursa', 'Antalya', 'Adana', 'Konya',
  'Gaziantep', 'Kocaeli', 'Mersin', 'Eskişehir', 'Trabzon', 'Kayseri', 'Samsun',
] as const

export type ValueKind = 'cities' | 'language' | 'int'

export interface FieldSpec {
  label: string
  kind: ValueKind
  ops: string[]
  min?: number
  max?: number
  unit?: string
  defaultValue: unknown
}

// Order and ranges as in segment.whitelist.
export const FIELDS: Record<string, FieldSpec> = {
  city: { label: 'Şehir', kind: 'cities', ops: ['in', 'not_in'], defaultValue: ['İstanbul'] },
  language: { label: 'Dil', kind: 'language', ops: ['eq'], defaultValue: 'tr' },
  abandoned_cart: { label: 'Sepeti terk etti', kind: 'int', ops: ['within_days'], min: 1, max: 365, unit: 'gün', defaultValue: 30 },
  purchased: { label: 'Satın aldı', kind: 'int', ops: ['within_days', 'not_within_days'], min: 1, max: 365, unit: 'gün', defaultValue: 30 },
  order_count: { label: 'Sipariş sayısı', kind: 'int', ops: ['gte', 'lte'], min: 0, max: 1000, unit: 'adet', defaultValue: 1 },
  total_spent: { label: 'Toplam harcama', kind: 'int', ops: ['gte', 'lte'], min: 0, max: 1000000, unit: '₺', defaultValue: 1000 },
  signed_up: { label: 'Kayıt oldu', kind: 'int', ops: ['within_days'], min: 1, max: 3650, unit: 'gün', defaultValue: 30 },
}

export const OP_LABELS: Record<string, string> = {
  in: 'şunlardan biri',
  not_in: 'şunlar hariç',
  eq: 'eşittir',
  within_days: 'son … gün içinde',
  not_within_days: 'son … gün içinde değil',
  gte: 'en az',
  lte: 'en fazla',
}

export const LANGUAGES: Record<string, string> = { tr: 'Türkçe', en: 'İngilizce' }

export function defaultCondition(field: string): Condition {
  const spec = FIELDS[field]
  const v = spec.defaultValue
  return { field, op: spec.ops[0], value: Array.isArray(v) ? [...v] : v }
}

/** A filtered rule left empty because nothing in the brief had a matching field. */
export const isUnmappedEmpty = (r: Rule) => r.audience === 'filtered' && r.conditions.length === 0 && (r.unmapped?.length ?? 0) > 0

const fmtInt = (n: unknown) => (typeof n === 'number' ? n.toLocaleString('tr-TR') : String(n))

/** A human-readable sentence for a condition (used in summaries and the refine diff). */
export function describeCondition(c: Condition): string {
  const v = c.value
  switch (`${c.field}:${c.op}`) {
    case 'city:in':
      return `Şehir: ${(v as string[]).join(', ')}`
    case 'city:not_in':
      return `Şu şehirler hariç: ${(v as string[]).join(', ')}`
    case 'language:eq':
      return `Dil: ${LANGUAGES[v as string] ?? String(v)}`
    case 'abandoned_cart:within_days':
      return `Son ${fmtInt(v)} günde sepete ürün ekleyip almamış`
    case 'purchased:within_days':
      return `Son ${fmtInt(v)} günde alışveriş yapmış`
    case 'purchased:not_within_days':
      return `Son ${fmtInt(v)} günde alışveriş yapmamış`
    case 'order_count:gte':
      return `Sipariş sayısı en az ${fmtInt(v)}`
    case 'order_count:lte':
      return `Sipariş sayısı en fazla ${fmtInt(v)}`
    case 'total_spent:gte':
      return `Toplam harcama en az ${fmtInt(v)} ₺`
    case 'total_spent:lte':
      return `Toplam harcama en fazla ${fmtInt(v)} ₺`
    case 'signed_up:within_days':
      return `Son ${fmtInt(v)} günde kayıt olmuş`
  }
  return `${c.field} ${c.op} ${JSON.stringify(v)}`
}

export function describeRule(r: Rule): string[] {
  if (r.audience === 'all') return ['Tüm kullanıcılar']
  if (r.conditions.length === 0) return ['Koşul yok — kitle boş']
  return r.conditions.map(describeCondition)
}

const longest = (xs: readonly string[]) =>
  xs.reduce((a, b) => ([...b].length > [...a].length ? b : a), '')

/**
 * The longest city a recipient of this rule can have. The backend measures
 * length limits with the longest real value in the audience; the city part of
 * that is known from the rule itself.
 */
export function worstCaseCity(r: Rule | null): string {
  let allowed: string[] = [...CITIES]
  for (const c of r?.conditions ?? []) {
    if (c.field !== 'city' || !Array.isArray(c.value)) continue
    const vs = c.value as string[]
    allowed = c.op === 'in' ? allowed.filter((x) => vs.includes(x)) : allowed.filter((x) => !vs.includes(x))
  }
  return longest(allowed.length ? allowed : [...CITIES])
}

/**
 * The longest first name in the data set (seed.longestFirstName, which is
 * also content's default worst case). Using it keeps the live counter on the
 * safe side of the backend check.
 */
export const WORST_CASE_FIRST_NAME = 'Abdurrahman Muhammed'

/**
 * Client-side pre-check of the form so obviously invalid rows are flagged
 * without a round trip; the backend remains the authority.
 */
export function validateLocal(r: Rule): VError[] {
  const errs: VError[] = []
  // "Everyone" is never implied by an empty list (DECISIONS #8); same as the
  // backend's empty_conditions. An empty list is a valid draft when the brief
  // could not be mapped at all (unmapped is set): the audience is then empty
  // and approval is refused on the 0-person preview.
  if (r.audience === 'filtered' && r.conditions.length === 0 && !(r.unmapped?.length)) {
    errs.push({ path: 'conditions', code: 'empty_conditions', message: 'En az bir koşul ekleyin ya da açıkça "Tüm kullanıcılar"ı seçin' })
  }
  r.conditions.forEach((c, i) => {
    const spec = FIELDS[c.field]
    const path = `conditions[${i}].value`
    if (!spec) return
    if (spec.kind === 'cities' && (!Array.isArray(c.value) || c.value.length === 0)) {
      errs.push({ path, code: 'invalid_value', message: 'En az bir şehir seçin' })
    } else if (spec.kind === 'int') {
      const n = c.value
      if (typeof n !== 'number' || !Number.isInteger(n)) {
        errs.push({ path, code: 'invalid_value', message: 'Tam sayı girin' })
      } else if (n < (spec.min ?? 0) || n > (spec.max ?? Infinity)) {
        errs.push({ path, code: 'out_of_range', message: `${fmtInt(spec.min)}–${fmtInt(spec.max)} arasında olmalı` })
      }
    }
  })
  return errs
}
