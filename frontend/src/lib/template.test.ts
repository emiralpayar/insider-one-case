import { describe, expect, it } from 'vitest'
import { fromSegments, toSegments } from './template'

const roundTrip = (s: string) => expect(fromSegments(toSegments(s))).toBe(s)

describe('toSegments', () => {
  it('turns canonical placeholders into chips', () => {
    expect(toSegments('Merhaba {{first_name}}, {{city}} için %30!')).toEqual([
      { kind: 'text', text: 'Merhaba ' },
      { kind: 'var', name: 'first_name' },
      { kind: 'text', text: ', ' },
      { kind: 'var', name: 'city' },
      { kind: 'text', text: ' için %30!' },
    ])
  })
  it('keeps adjacent chips separate with no empty text between', () => {
    expect(toSegments('{{first_name}}{{city}}')).toEqual([
      { kind: 'var', name: 'first_name' },
      { kind: 'var', name: 'city' },
    ])
  })
  it('leaves unknown, spaced and malformed placeholders as plain text', () => {
    for (const s of ['{{phone}}', '{{ city }}', '{{first_name', 'first_name}}', '{{}}', '{{First_Name}}', '{{{city}}', '{{city}}}']) {
      const segs = toSegments(s)
      if (s === '{{city}}}') {
        // "{{city}}" then a stray "}" (the backend scan reads it the same way)
        expect(segs).toEqual([{ kind: 'var', name: 'city' }, { kind: 'text', text: '}' }])
      } else {
        expect(segs).toEqual([{ kind: 'text', text: s }])
      }
    }
  })
  it('empty string has no segments', () => {
    expect(toSegments('')).toEqual([])
    expect(fromSegments([])).toBe('')
  })
})

describe('round trip', () => {
  it.each([
    'Merhaba {{first_name}}!',
    '{{first_name}}{{city}}',
    'Satır 1\nSatır 2\n\n{{city}}\n',
    '\n{{first_name}}\n',
    'Sevgili {{first_name}}, {{phone}} numaranı doğrula',
    '{{first_name',
    'Bozuk }} kapanış {{city}}',
    '{{ first_name }} boşluklu',
    'Çiğdem, İstanbul’da ğüşıöç ÇĞÜŞİÖ €',
    '{{city}}’da',
    '{{{first_name}}}',
    '   ',
    '',
  ])('%j', roundTrip)

  it('random strings built from tricky pieces survive', () => {
    const pieces = ['{{', '}}', 'first_name', 'city', '{{first_name}}', '{{city}}', '\n', ' ', 'ş', 'a', '{', '}']
    let seed = 7
    const rnd = () => (seed = (seed * 1103515245 + 12345) % 2147483648) / 2147483648
    for (let i = 0; i < 500; i++) {
      let s = ''
      const n = Math.floor(rnd() * 12)
      for (let j = 0; j < n; j++) s += pieces[Math.floor(rnd() * pieces.length)]
      roundTrip(s)
    }
  })
})
