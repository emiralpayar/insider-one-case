// JSON shapes of the Go API, mirrored field by field.
// Sources: backend/internal/{segment,content,campaign,verr,api,llm}.

// ---- segment (rule.go, preview.go) ----

export type Audience = 'all' | 'filtered'

/** segment.Condition; value is json.RawMessage (city list, "tr"/"en" or an integer). */
export interface Condition {
  field: string
  op: string
  value: unknown
}

/** segment.Rule */
export interface Rule {
  audience: Audience
  conditions: Condition[]
  unmapped: string[]
  explanation: string
}

/** segment.SampleUser */
export interface SampleUser {
  id: number
  first_name: string
  city: string
}

/** segment.Warning: empty_audience | large_audience | unmapped | mixed_language */
export interface Warning {
  code: string
  message: string
}

/** segment.PreviewResult */
export interface PreviewResult {
  total: number
  sms_eligible: number
  email_eligible: number
  all_users: number
  english_speakers: number
  sample: SampleUser[]
  warnings: Warning[]
}

/** segment.Member (POST /segments/members) */
export interface Member {
  id: number
  first_name: string
  last_name: string
  city: string
  language: 'tr' | 'en'
  sms_consent: boolean | null
  email_consent: boolean | null
  order_count: number
  total_spent: number
}

/** segment.MemberPage */
export interface MemberPage {
  members: Member[]
  total: number
  page: number
  per_page: number
}

/** api.ruleResponse (generate / refine / PUT segment) */
export interface RuleResponse {
  rule: Rule
  preview: PreviewResult
  previous_preview?: PreviewResult
}

// ---- content (content.go, sms.go) ----

export interface Email {
  subject: string
  preheader: string
  body: string
}

/** content.Content */
export interface Content {
  sms: string
  email: Email
}

/** content.SMSStats */
export interface SMSStats {
  chars: number
  encoding: 'GSM-7' | 'UCS-2'
  segments: number
  non_gsm_chars: string[]
}

/** llm.PartialContent (SSE "partial" event) */
export interface PartialContent {
  sms: string
  subject: string
  preheader: string
  body: string
}

/** api.doneEvent (SSE "done" event) */
export interface DoneEvent {
  content: Content
  issues: VError[]
  saved: boolean
  sms_stats: SMSStats
  rendered_sample: { named: Content; nameless: Content }
}

/** PUT /content response */
export interface PutContentResponse {
  content: Content
  sms_stats: SMSStats
}

// ---- verr ----

/** verr.Error */
export interface VError {
  path: string
  code: string
  message: string
}

// ---- campaign (store.go, approve.go, status.go) ----

export type Channel = 'sms' | 'email'

export type CampaignStatus = 'draft' | 'audience_ready' | 'content_ready' | 'sending' | 'completed'

/** campaign.Campaign */
export interface Campaign {
  id: string
  name: string
  brief: string
  llm_provider: string
  channels: Channel[]
  status: CampaignStatus
  rule: Rule | null
  content: Content | null
  approved_at: string | null
  created_at: string
}

/** campaign.ApproveRequest */
export interface ApproveRequest {
  confirm_large_audience: boolean
}

/** campaign.ApproveResult */
export interface ApproveResult {
  enqueued: Record<string, number>
}

export type MessageStatus = 'queued' | 'sending' | 'sent' | 'failed' | 'unknown' | 'skipped_no_consent'

/** campaign.ChannelStatus */
export interface ChannelStatus {
  total: number
  by_status: Partial<Record<MessageStatus, number>> & Record<string, number>
  sent_last_10s: number
  rate_per_second: number
}

/** campaign.MessageError */
export interface MessageError {
  message_id: string
  user_id: number
  channel: Channel
  status: string
  attempts: number
  error: string
  at: string
}

/** campaign.MessageRow (GET /campaigns/{id}/messages) */
export interface MessageRow {
  id: string
  user_id: number
  first_name: string
  last_name: string
  channel: Channel
  recipient: string
  status: MessageStatus
  attempts: number
  last_error: string | null
  sent_at: string | null
  updated_at: string
}

/** campaign.MessagePage */
export interface MessagePage {
  messages: MessageRow[]
  total: number
  page: number
  per_page: number
}

/** campaign.StatusReport */
export interface StatusReport {
  campaign_status: CampaignStatus
  channels: Record<string, ChannelStatus>
  recent_errors: MessageError[]
}

// ---- api (health.go, errors.go) ----

/** GET /api/config */
export interface AppConfig {
  llm_provider: string
  llm_configured: boolean
  models: { segment: string; content: string }
}

/** Error envelope body: {"error": ApiErrorBody} */
export interface ApiErrorBody {
  code: string
  message: string
  details?: unknown
}

/** An earlier campaign that already sent the same text to part of this audience. */
export interface DuplicateSend {
  campaign_id: string
  name: string
  channel: Channel
  recipients: number
  approved_at: string
  /** text: identical text; brief: same brief; similar: LLM judged same purpose (see reason). */
  match: 'text' | 'brief' | 'similar'
  reason?: string
}

/** GET /api/campaigns/{id}/duplicates */
export interface DuplicateReport {
  duplicates: DuplicateSend[]
  /** off: LLM check disabled; ok: ran; error: failed, only exact checks applied. */
  similarity_check: 'off' | 'ok' | 'error'
}
