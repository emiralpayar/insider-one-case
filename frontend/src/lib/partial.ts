// Merging streamed partial content into the editor's draft.
import type { Channel, Content, PartialContent } from '../types'

export function emptyContent(): Content {
  return { sms: '', email: { subject: '', preheader: '', body: '' } }
}

export function cloneContent(c: Content | null | undefined): Content {
  return c ? { sms: c.sms, email: { ...c.email } } : emptyContent()
}

/**
 * Returns base with the fields of the channels being generated replaced by
 * what has streamed so far; the other channel keeps its saved text (the
 * backend merges a per-channel regeneration the same way).
 */
export function applyPartial(base: Content, p: PartialContent, channels: Channel[]): Content {
  const out = cloneContent(base)
  if (channels.includes('sms')) out.sms = p.sms
  if (channels.includes('email')) out.email = { subject: p.subject, preheader: p.preheader, body: p.body }
  return out
}

export function sameContent(a: Content | null, b: Content | null): boolean {
  if (!a || !b) return a === b
  return a.sms === b.sms && a.email.subject === b.email.subject &&
    a.email.preheader === b.email.preheader && a.email.body === b.email.body
}
