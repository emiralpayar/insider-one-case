// Port of backend/internal/content/{sms,render}.go. The live counters and
// previews in the UI must give exactly the backend's answers, so this file
// mirrors the Go code line by line (same tables, same regexes, rune counts).

export type SmsEncoding = 'GSM-7' | 'UCS-2'

export interface SmsStats {
  chars: number
  encoding: SmsEncoding
  segments: number
  nonGsmChars: string[]
}

const GSM_BASE =
  '@£$¥èéùìòÇ\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞÆæßÉ !"#¤%&\'()*+,-./0123456789:;<=>?¡ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà'
const GSM_EXT = '^{}\\[~]|€\f'

const gsmBaseSet = new Set([...GSM_BASE])
const gsmExtSet = new Set([...GSM_EXT])

/** Number of Unicode code points (Go's utf8.RuneCountInString). */
export function runeCount(s: string): number {
  let n = 0
  for (const _ of s) n++
  return n
}

/**
 * Encoding, length and segment count of an SMS body (content.SMSInfo).
 * GSM-7 counts extended characters as 2; UCS-2 counts runes. An empty string
 * has 0 segments.
 */
export function smsStats(s: string): SmsStats {
  const nonGsmChars: string[] = []
  const seen = new Set<string>()
  let gsmLen = 0
  let runes = 0
  for (const r of s) {
    runes++
    if (gsmBaseSet.has(r)) gsmLen++
    else if (gsmExtSet.has(r)) gsmLen += 2
    else if (!seen.has(r)) {
      seen.add(r)
      nonGsmChars.push(r)
    }
  }
  const ucs = nonGsmChars.length > 0
  const chars = ucs ? runes : gsmLen
  const [single, multi] = ucs ? [70, 67] : [160, 153]
  let segments: number
  if (chars === 0) segments = 0
  else if (chars <= single) segments = 1
  else segments = Math.ceil(chars / multi)
  return { chars, encoding: ucs ? 'UCS-2' : 'GSM-7', segments, nonGsmChars }
}

// ---- Render ---------------------------------------------------------------

const ALLOWED_VARS = new Set(['first_name', 'city'])

type Token = { kind: 'lit' | 'var' | 'bad'; text: string; raw: string }

const nameRe = /^[a-z_]+$/

/** Trims the characters in cutset from both ends (Go strings.Trim). */
function trimSet(s: string, cutset: string): string {
  let a = 0
  let b = s.length
  while (a < b && cutset.includes(s[a])) a++
  while (b > a && cutset.includes(s[b - 1])) b--
  return s.slice(a, b)
}

/** Splits s into literals, {{ name }} placeholders and malformed pieces. */
export function scan(s: string): Token[] {
  const out: Token[] = []
  while (s.length > 0) {
    const o = s.indexOf('{{')
    const c = s.indexOf('}}')
    if (o < 0 && c < 0) {
      out.push({ kind: 'lit', text: s, raw: s })
      break
    }
    if (c >= 0 && (o < 0 || c < o)) {
      // stray "}}"
      if (c > 0) out.push({ kind: 'lit', text: s.slice(0, c), raw: s.slice(0, c) })
      out.push({ kind: 'bad', text: '}}', raw: '}}' })
      s = s.slice(c + 2)
      continue
    }
    if (o > 0) out.push({ kind: 'lit', text: s.slice(0, o), raw: s.slice(0, o) })
    let end = s.slice(o + 2).indexOf('}}')
    if (end < 0) {
      // unterminated
      out.push({ kind: 'bad', text: s.slice(o), raw: s.slice(o) })
      break
    }
    end += o + 2
    const raw = s.slice(o, end + 2)
    const name = trimSet(s.slice(o + 2, end), ' ')
    if (nameRe.test(name)) out.push({ kind: 'var', text: name, raw })
    else out.push({ kind: 'bad', text: raw, raw })
    s = s.slice(end + 2)
  }
  return out
}

/** Upper-cases one rune with Turkish rules (unicode.TurkishCase.ToUpper). */
function turkishUpper(r: string): string {
  if (r === 'i') return 'İ'
  if (r === 'ı') return 'I'
  const u = r.toUpperCase()
  // Go maps rune to rune; full case mappings that expand (ß → SS) are not applied.
  return runeCount(u) === 1 ? u : r
}

const multiSpaceRe = / {2,}/g
const spaceBeforeRe = / +([,.!?;:])/g
const dupCommaRe = /,(?: *,)+/g
const leadPunctRe = /^[,.!?;:] +/

/** Tidies a line after an empty value was substituted (content.cleanLine). */
function cleanLine(l: string): string {
  l = l.replace(multiSpaceRe, ' ')
  l = l.replace(spaceBeforeRe, '$1')
  l = l.replace(dupCommaRe, ',')
  l = trimSet(l, ' \t')
  const m = leadPunctRe.exec(l)
  if (m) {
    l = l.slice(m[0].length)
    const first = l.codePointAt(0)
    if (first !== undefined) {
      const r = String.fromCodePoint(first)
      l = turkishUpper(r) + l.slice(r.length)
    }
  }
  return l
}

/**
 * Substitutes {{first_name}} / {{city}} placeholders from vars (content.Render).
 * Unknown or malformed placeholders are left untouched. When a substituted
 * value is empty, the affected lines are tidied.
 */
export function render(tmpl: string, vars: Record<string, string>): string {
  const lines = ['']
  const dirty = [false]
  const write = (s: string) => {
    s.split('\n').forEach((p, i) => {
      if (i > 0) {
        lines.push('')
        dirty.push(false)
      }
      lines[lines.length - 1] += p
    })
  }
  for (const t of scan(tmpl)) {
    if (t.kind === 'var' && ALLOWED_VARS.has(t.text)) {
      const v = vars[t.text] ?? ''
      if (v === '') dirty[dirty.length - 1] = true
      write(v)
    } else {
      write(t.raw)
    }
  }
  return lines.map((l, i) => (dirty[i] ? cleanLine(l) : l)).join('\n')
}

/** Placeholder problems in a template, like content.checkField reports them. */
export function placeholderIssues(tmpl: string): string[] {
  const out: string[] = []
  for (const t of scan(tmpl)) {
    if (t.kind === 'bad') out.push(`Hatalı yer tutucu: "${t.raw}"`)
    else if (t.kind === 'var' && !ALLOWED_VARS.has(t.text))
      out.push(`Bilinmeyen değişken: ${t.text} (izin verilenler: first_name, city)`)
  }
  return out
}

/**
 * Worst-case length of a template, as content.checkField measures it: the
 * larger of the rendering with the longest values and with empty values.
 */
export function worstCaseLength(tmpl: string, longest: Record<string, string>): number {
  return Math.max(runeCount(render(tmpl, longest)), runeCount(render(tmpl, {})))
}
