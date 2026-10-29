// Same vectors as backend/internal/content/sms_test.go and render_test.go:
// the live counter in the UI must agree with the backend validator.
import { describe, expect, it } from 'vitest'
import { render, smsStats, worstCaseLength } from './sms'

const rep = (s: string, n: number) => s.repeat(n)

describe('smsStats (content.SMSInfo)', () => {
  const cases: Array<[string, string, number, 'GSM-7' | 'UCS-2', number, string[]]> = [
    ['short gsm', 'Merhaba Ali', 11, 'GSM-7', 1, []],
    ['empty', '', 0, 'GSM-7', 0, []],
    ['160 gsm', rep('a', 160), 160, 'GSM-7', 1, []],
    ['161 gsm', rep('a', 161), 161, 'GSM-7', 2, []],
    ['306 gsm', rep('a', 306), 306, 'GSM-7', 2, []],
    ['307 gsm', rep('a', 307), 307, 'GSM-7', 3, []],
    ['turkish', 'Merhaba Ayşe', 12, 'UCS-2', 1, ['ş']],
    ['70 ucs', rep('ş', 70), 70, 'UCS-2', 1, ['ş']],
    ['71 ucs', rep('ş', 71), 71, 'UCS-2', 2, ['ş']],
    ['134 ucs', rep('ş', 134), 134, 'UCS-2', 2, ['ş']],
    ['135 ucs', rep('ş', 135), 135, 'UCS-2', 3, ['ş']],
    ['euro', '€', 2, 'GSM-7', 1, []],
    ['brace', '{', 2, 'GSM-7', 1, []],
    ['ext pushes over', rep('a', 159) + '€', 161, 'GSM-7', 2, []],
    ['ucs ext counts one', 'ş{', 2, 'UCS-2', 1, ['ş']],
    ['dedupe order', 'çşç ğ', 5, 'UCS-2', 1, ['ç', 'ş', 'ğ']],
  ]
  for (const [name, input, chars, enc, segments, nonGsm] of cases) {
    it(name, () => {
      const got = smsStats(input)
      expect(got.chars).toBe(chars)
      expect(got.encoding).toBe(enc)
      expect(got.segments).toBe(segments)
      expect(got.nonGsmChars).toEqual(nonGsm)
    })
  }

  it('full base table is GSM-7', () => {
    const base =
      '@£$¥èéùìòÇ\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞÆæßÉ !"#¤%&\'()*+,-./0123456789:;<=>?¡ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà'
    expect(smsStats(base).encoding).toBe('GSM-7')
    const ext = smsStats('^{}\\[~]|€\f')
    expect(ext.encoding).toBe('GSM-7')
    expect(ext.chars).toBe(20)
  })

  it('counts astral characters as one rune (utf8.RuneCountInString)', () => {
    const got = smsStats('a😀')
    expect(got.encoding).toBe('UCS-2')
    expect(got.chars).toBe(2)
    expect(got.nonGsmChars).toEqual(['😀'])
  })
})

describe('render (content.Render)', () => {
  const ayse = { first_name: 'Ayşe', city: 'İzmir' }
  const empty = { first_name: '', city: '' }
  const cases: Array<[string, string, Record<string, string> | undefined, string]> = [
    ['basic', 'Merhaba {{first_name}}, sepetin seni bekliyor!', ayse, 'Merhaba Ayşe, sepetin seni bekliyor!'],
    ['empty name', 'Merhaba {{first_name}}, sepetin seni bekliyor!', empty, 'Merhaba, sepetin seni bekliyor!'],
    ['leading', '{{first_name}}, sepetin seni bekliyor!', empty, 'Sepetin seni bekliyor!'],
    ['turkish upper', '{{first_name}}, indirim seni bekliyor!', empty, 'İndirim seni bekliyor!'],
    ['leading bang', '{{first_name}}! ışık', empty, 'Işık'],
    ['space before bang', 'Selam {{first_name}} !', empty, 'Selam!'],
    ['double space', 'Selam  {{first_name}}  dünya', empty, 'Selam dünya'],
    ['spaced braces', 'Selam {{ first_name }}!', ayse, 'Selam Ayşe!'],
    ['city', "{{city}}'de fırsat", ayse, "İzmir'de fırsat"],
    ['no cleanup when not empty', 'A  B , C', ayse, 'A  B , C'],
    ['no cleanup when no placeholder', 'A  b !', empty, 'A  b !'],
    ['double comma', 'Merhaba {{first_name}}, {{city}}, hoş geldin', empty, 'Merhaba, hoş geldin'],
    ['unknown left', 'Hi {{phone}}', ayse, 'Hi {{phone}}'],
    ['malformed left', 'Hi {{first_name', ayse, 'Hi {{first_name'],
    ['stray close', 'Hi }} {{first_name}}', ayse, 'Hi }} Ayşe'],
    ['multiline', '{{first_name}}, merhaba\n\nİyi günler {{first_name}} !', empty, 'Merhaba\n\nİyi günler!'],
    ['missing var is empty', 'Selam {{first_name}} !', undefined, 'Selam!'],
    ['multiline keeps untouched lines', 'a  b\nSelam {{first_name}} !', empty, 'a  b\nSelam!'],
  ]
  for (const [name, tmpl, vars, want] of cases) {
    it(name, () => {
      expect(render(tmpl, vars ?? {})).toBe(want)
    })
  }
})

describe('worstCaseLength (content.checkField measurement)', () => {
  it('takes the longer of the longest-value and empty-value renderings', () => {
    const longest = { first_name: 'Abdurrahman Muhammed', city: 'Eskişehir' }
    expect(worstCaseLength('Merhaba {{first_name}}!', longest)).toBe(29)
    expect(worstCaseLength('{{first_name}}', { first_name: 'A', city: '' })).toBe(1)
    expect(worstCaseLength('Selam {{ phone }}', longest)).toBe(17)
  })
})
