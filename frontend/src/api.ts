// HTTP client for the composer API (backend/internal/api).
import { SSEParser } from './lib/sse'
import type {
  AppConfig, ApproveResult, Campaign, Channel, DoneEvent, PartialContent, PreviewResult,
  PutContentResponse, Rule, RuleResponse, StatusReport, VError, Content, ApiErrorBody,
} from './types'

/** A failed API call: the {"error":{code,message,details}} envelope plus the HTTP status. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly details?: unknown,
  ) {
    super(message)
  }

  /** details as a verr list (validation_failed, llm_invalid_output). */
  get fieldErrors(): VError[] {
    return Array.isArray(this.details) ? (this.details as VError[]) : []
  }
}

async function toApiError(res: Response): Promise<ApiError> {
  try {
    const body = (await res.json()) as { error?: ApiErrorBody }
    if (body.error) return new ApiError(res.status, body.error.code, body.error.message, body.error.details)
  } catch {
    /* not JSON */
  }
  return new ApiError(res.status, 'http_' + res.status, `Sunucu hatası (${res.status})`)
}

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  let res: Response
  try {
    res = await fetch('/api' + path, {
      method,
      headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch {
    throw new ApiError(0, 'network', 'Sunucuya ulaşılamadı')
  }
  if (!res.ok) throw await toApiError(res)
  return (await res.json()) as T
}

const c = (id: string) => `/campaigns/${encodeURIComponent(id)}`

export const api = {
  config: () => req<AppConfig>('GET', '/config'),
  listCampaigns: () => req<Campaign[]>('GET', '/campaigns'),
  getCampaign: (id: string) => req<Campaign>('GET', c(id)),
  createCampaign: (brief: string) => req<Campaign>('POST', '/campaigns', { brief }),
  generateSegment: (id: string) => req<RuleResponse>('POST', c(id) + '/segment/generate'),
  refineSegment: (id: string, instruction: string) =>
    req<RuleResponse>('POST', c(id) + '/segment/refine', { instruction }),
  putSegment: (id: string, rule: Rule) => req<RuleResponse>('PUT', c(id) + '/segment', { rule }),
  preview: (rule: Rule) => req<PreviewResult>('POST', '/segments/preview', { rule }),
  putContent: (id: string, content: Content) => req<PutContentResponse>('PUT', c(id) + '/content', { content }),
  putChannels: (id: string, channels: Channel[]) => req<Campaign>('PUT', c(id) + '/channels', { channels }),
  approve: (id: string, confirmLargeAudience: boolean) =>
    req<ApproveResult>('POST', c(id) + '/approve', { confirm_large_audience: confirmLargeAudience }),
  status: (id: string) => req<StatusReport>('GET', c(id) + '/status'),
}

export interface StreamHandlers {
  partial(p: PartialContent): void
  repair(issues: VError[]): void
}

/**
 * Generates content over SSE (GET /content/stream). Resolves with the "done"
 * event; rejects with an ApiError for a pre-stream JSON error, an "error"
 * event, or a stream that ends without "done". Abort with signal.
 */
export async function streamContent(
  id: string,
  channel: 'all' | Channel,
  h: StreamHandlers,
  signal: AbortSignal,
): Promise<DoneEvent> {
  let res: Response
  try {
    res = await fetch(`/api${c(id)}/content/stream?channel=${channel}`, {
      headers: { Accept: 'text/event-stream' },
      signal,
    })
  } catch (e) {
    if (signal.aborted) throw e
    throw new ApiError(0, 'network', 'Sunucuya ulaşılamadı')
  }
  if (!res.ok || !res.body) throw await toApiError(res)

  let done = null as DoneEvent | null
  let failure = null as ApiError | null
  const parser = new SSEParser(({ event, data }) => {
    const v = JSON.parse(data)
    switch (event) {
      case 'partial':
        h.partial(v as PartialContent)
        break
      case 'repair':
        h.repair((v ?? []) as VError[])
        break
      case 'done':
        done = v as DoneEvent
        break
      case 'error': {
        const e = v as ApiErrorBody
        failure = new ApiError(502, e.code, e.message, e.details)
        break
      }
    }
  })
  const reader = res.body.pipeThrough(new TextDecoderStream()).getReader()
  for (;;) {
    const { value, done: eof } = await reader.read()
    if (eof) break
    parser.feed(value)
  }
  if (failure) throw failure
  if (!done) throw new ApiError(0, 'stream_closed', 'Bağlantı içerik tamamlanmadan kapandı')
  return done
}
