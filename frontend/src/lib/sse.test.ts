import { describe, expect, it } from 'vitest'
import { SSEParser, type SSEMessage } from './sse'

function collect(chunks: string[]): SSEMessage[] {
  const out: SSEMessage[] = []
  const p = new SSEParser((m) => out.push(m))
  for (const c of chunks) p.feed(c)
  return out
}

describe('SSEParser', () => {
  it('parses events split across chunks', () => {
    const got = collect(['event: par', 'tial\ndata: {"sms":"Mer', 'haba"}\n', '\nevent: done\ndata: {}\n\n'])
    expect(got).toEqual([
      { event: 'partial', data: '{"sms":"Merhaba"}' },
      { event: 'done', data: '{}' },
    ])
  })
  it('ignores heartbeat comments', () => {
    expect(collect([': ping\n\n', 'event: x\ndata: 1\n\n', ': ping\n\n'])).toEqual([{ event: 'x', data: '1' }])
  })
  it('joins multi-line data and handles CRLF', () => {
    expect(collect(['data: a\r\ndata: b\r\n\r\n'])).toEqual([{ event: 'message', data: 'a\nb' }])
  })
  it('does not emit an unterminated event', () => {
    expect(collect(['event: x\ndata: 1\n'])).toEqual([])
  })
})
