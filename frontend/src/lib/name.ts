// Campaign name editing (PUT /campaigns/{id}/name); mirrors
// campaign.ValidateName so most mistakes are caught before the request.

export const NAME_MAX = 120

/** Collapses whitespace and trims, as the backend does. */
export function normalizeName(s: string): string {
  return s.trim().split(/\s+/).filter(Boolean).join(' ')
}

/** Turkish error message, or null when the name is valid. */
export function nameError(s: string): string | null {
  const n = [...normalizeName(s)].length
  if (n === 0) return 'Kampanya adı boş olamaz'
  if (n > NAME_MAX) return `Kampanya adı en fazla ${NAME_MAX} karakter olabilir`
  return null
}
