// Template text ↔ editor segments for the variable-chip editor.
//
// Only the exact canonical spellings "{{first_name}}" and "{{city}}" become
// chips. Everything else — unknown names ({{phone}}), spaced forms
// ({{ city }}), unterminated or stray braces — stays plain text, so that
// fromSegments(toSegments(s)) === s for every string and the serialized value
// is always exactly what the user (or Claude) wrote.
import { scan } from './sms'

export type VarName = 'first_name' | 'city'

export type Segment = { kind: 'text'; text: string } | { kind: 'var'; name: VarName }

export const VARIABLES: Record<VarName, { label: string }> = {
  first_name: { label: 'Ad' },
  city: { label: 'Şehir' },
}

export const isVarName = (s: string): s is VarName => s === 'first_name' || s === 'city'

export const placeholder = (name: VarName) => `{{${name}}}`

/** Splits template text into text runs and chip variables. Adjacent text is merged. */
export function toSegments(s: string): Segment[] {
  const out: Segment[] = []
  const pushText = (t: string) => {
    if (!t) return
    const last = out[out.length - 1]
    if (last?.kind === 'text') last.text += t
    else out.push({ kind: 'text', text: t })
  }
  for (const t of scan(s)) {
    if (t.kind === 'var' && isVarName(t.text) && t.raw === placeholder(t.text)) out.push({ kind: 'var', name: t.text })
    else pushText(t.raw)
  }
  return out
}

/** The template string for a list of segments (inverse of toSegments). */
export function fromSegments(segs: readonly Segment[]): string {
  return segs.map((s) => (s.kind === 'text' ? s.text : placeholder(s.name))).join('')
}
