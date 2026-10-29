package llm

import (
	"context"
	"errors"
	"sync"
)

// Scripted is a test Client returning canned raw texts in order and recording
// every request it receives.
type Scripted struct {
	mu        sync.Mutex
	responses []string
	calls     []Request
}

// ErrScriptExhausted is returned when Scripted has no responses left.
var ErrScriptExhausted = errors.New("llm: scripted client has no responses left")

// NewScripted returns a Scripted client that answers with responses in order.
func NewScripted(responses ...string) *Scripted {
	return &Scripted{responses: responses}
}

// Name implements Client.
func (s *Scripted) Name() string { return "scripted" }

// Calls returns a copy of the requests received so far.
func (s *Scripted) Calls() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.calls...)
}

// Complete implements Client. The response is streamed to onDelta in chunks.
func (s *Scripted) Complete(ctx context.Context, req Request, onDelta func(string)) (Response, error) {
	s.mu.Lock()
	req.Messages = append([]Message(nil), req.Messages...)
	s.calls = append(s.calls, req)
	if len(s.responses) == 0 {
		s.mu.Unlock()
		return Response{}, ErrScriptExhausted
	}
	text := s.responses[0]
	s.responses = s.responses[1:]
	s.mu.Unlock()
	if err := streamChunks(ctx, text, onDelta); err != nil {
		return Response{}, err
	}
	return Response{Text: text, Model: "scripted", StopReason: "end_turn"}, nil
}

// streamChunks sends text to onDelta in chunks of chunkRunes runes.
func streamChunks(ctx context.Context, text string, onDelta func(string)) error {
	if onDelta == nil {
		return nil
	}
	const chunkRunes = 8
	r := []rune(text)
	for i := 0; i < len(r); i += chunkRunes {
		if err := ctx.Err(); err != nil {
			return err
		}
		onDelta(string(r[i:min(i+chunkRunes, len(r))]))
	}
	return nil
}
