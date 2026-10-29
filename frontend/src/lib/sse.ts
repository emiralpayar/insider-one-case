// Minimal Server-Sent Events parser for a fetch() body stream.
//
// The content stream is read with fetch instead of EventSource on purpose:
// EventSource cannot read the JSON error body of a non-200 response (503
// llm_not_configured, 409 campaign_locked) and it reconnects automatically
// when the stream ends, which would start a second, billed generation.

export interface SSEMessage {
  event: string
  data: string
}

export class SSEParser {
  private buf = ''
  private event = ''
  private data: string[] = []

  constructor(private readonly onMessage: (m: SSEMessage) => void) {}

  feed(chunk: string): void {
    this.buf += chunk
    let nl: number
    while ((nl = this.buf.indexOf('\n')) >= 0) {
      let line = this.buf.slice(0, nl)
      this.buf = this.buf.slice(nl + 1)
      if (line.endsWith('\r')) line = line.slice(0, -1)
      this.line(line)
    }
  }

  private line(line: string): void {
    if (line === '') {
      if (this.data.length > 0) this.onMessage({ event: this.event || 'message', data: this.data.join('\n') })
      this.event = ''
      this.data = []
      return
    }
    if (line.startsWith(':')) return // comment / heartbeat
    const i = line.indexOf(':')
    const field = i < 0 ? line : line.slice(0, i)
    let value = i < 0 ? '' : line.slice(i + 1)
    if (value.startsWith(' ')) value = value.slice(1)
    if (field === 'event') this.event = value
    else if (field === 'data') this.data.push(value)
  }
}
