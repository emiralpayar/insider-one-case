// Turkish, action-oriented text for API error codes.
import { ApiError } from '../api'

const MESSAGES: Record<string, string> = {
  network: 'Sunucuya ulaşılamadı. Backend çalışıyor mu?',
  llm_not_configured: 'Claude API anahtarı tanımlı değil; Claude gerektiren adımlar çalışmaz. Kuralı elle kurabilirsiniz.',
  llm_invalid_output: 'Claude onarım denemelerinden sonra da geçerli bir çıktı üretemedi.',
  llm_refused: 'Claude bu isteği yanıtlamayı reddetti. Brief\'i ya da talimatı değiştirip tekrar deneyin.',
  llm_truncated: "Claude'un yanıtı uzunluk sınırında kesildi; tekrar deneyin.",
  llm_error: 'Claude çağrısı başarısız oldu (zaman aşımı ya da bağlantı). Tekrar deneyin.',
  validation_failed: 'Doğrulama başarısız; işaretli alanları düzeltin.',
  campaign_locked: 'Kampanya onaylandı; artık düzenlenemez.',
  not_ready: 'Kampanya bu adım için hazır değil (önce önceki adımı tamamlayın).',
  already_approved: 'Kampanya zaten onaylanmış.',
  large_audience_not_confirmed: 'Geniş kitle için onay kutusunu işaretleyin.',
  empty_audience: 'Seçili kanallarda mesaj alabilecek kimse yok.',
  not_found: 'Kampanya bulunamadı.',
  stream_closed: 'Bağlantı içerik tamamlanmadan kapandı. Tekrar deneyin.',
}

export function errorText(e: unknown): string {
  if (e instanceof ApiError) return MESSAGES[e.code] ?? e.message
  if (e instanceof Error) return e.message
  return String(e)
}

/** Field errors of a validation-style ApiError, grouped by path. */
export function errorsByPath(e: unknown): Record<string, string[]> {
  const out: Record<string, string[]> = {}
  if (e instanceof ApiError) {
    for (const fe of e.fieldErrors) (out[fe.path] ??= []).push(fe.message)
  }
  return out
}
