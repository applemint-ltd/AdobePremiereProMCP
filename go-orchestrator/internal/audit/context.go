// Package audit provides a persistent, correlated record of every MCP tool
// call so operators can answer "what did the AI do and where did it go
// wrong" after the fact. A Span travels through the request context from the
// MCP middleware down to the gRPC layer, collecting per-hop timings; the
// Auditor persists one JSONL record per tool call.
package audit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// ESCall records one ExtendScript/bridge hop made while handling a tool call.
type ESCall struct {
	FunctionName string `json:"fn"`
	DurationMs   int64  `json:"ms"`
}

// Span carries the correlation ID for one MCP tool call and accumulates the
// bridge hops made on its behalf. It is placed in the request context by the
// audit middleware and read by the gRPC client interceptor.
type Span struct {
	CID  string
	Tool string

	mu      sync.Mutex
	esCalls []ESCall
}

// NewSpan creates a Span with a fresh correlation ID.
func NewSpan(tool string) *Span {
	return &Span{CID: NewCID(), Tool: tool}
}

// AddESCall appends one bridge hop to the span. Safe for concurrent use and
// safe on a nil span (no-op), so callers never need to guard.
func (s *Span) AddESCall(fn string, ms int64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.esCalls = append(s.esCalls, ESCall{FunctionName: fn, DurationMs: ms})
	s.mu.Unlock()
}

// ESCalls returns a copy of the recorded hops.
func (s *Span) ESCalls() []ESCall {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ESCall, len(s.esCalls))
	copy(out, s.esCalls)
	return out
}

type ctxKey struct{}

// WithSpan attaches a span to the context.
func WithSpan(ctx context.Context, span *Span) context.Context {
	return context.WithValue(ctx, ctxKey{}, span)
}

// SpanFrom returns the span attached to the context, or nil.
func SpanFrom(ctx context.Context) *Span {
	span, _ := ctx.Value(ctxKey{}).(*Span)
	return span
}

// NewCID returns a short random correlation ID (12 hex chars).
func NewCID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing is effectively unrecoverable; fall back to a
		// fixed marker rather than panicking inside request handling.
		return "cid-rand-err"
	}
	return hex.EncodeToString(b[:])
}
