package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// encoderReady records whether Adobe Media Encoder has been confirmed ready
// in this process's lifetime. A cold AME launch takes 1-2 minutes; once up
// it stays up across turns, so we only pay the wait once per server process
// and skip it thereafter.
var (
	encoderReadyMu   sync.Mutex
	encoderReadyOnce bool
)

// EnsureEncoderReady launches Adobe Media Encoder if needed and waits
// (bounded) until it reports installed exporters — i.e. it can accept render
// jobs. This is deliberately separate from any render timeout: a cold AME
// launch should add latency, never cause a render to be reported as a false
// "queued_not_confirmed" timeout.
//
// force re-checks even if a previous call in this process already confirmed
// readiness (used by the explicit warm tool).
func (e *Engine) EnsureEncoderReady(ctx context.Context, force bool) error {
	encoderReadyMu.Lock()
	already := encoderReadyOnce
	encoderReadyMu.Unlock()
	if already && !force {
		return nil
	}

	// GetExporters drives the host getExporters, which launches AME and polls
	// briefly for exporters. On a cold AME the first calls return an empty
	// list; retry until non-empty or the deadline. 4 minutes comfortably
	// covers a cold launch without blocking forever.
	deadline := time.Now().Add(4 * time.Minute)
	attempt := 0
	for {
		attempt++
		res, err := e.GetExporters(ctx)
		if err == nil && res != nil && res.Count > 0 {
			encoderReadyMu.Lock()
			encoderReadyOnce = true
			encoderReadyMu.Unlock()
			e.logger.Info("encoder ready", zap.Int("exporters", res.Count), zap.Int("attempts", attempt))
			return nil
		}
		if time.Now().After(deadline) {
			detail := "no exporters reported"
			if err != nil {
				detail = err.Error()
			}
			return fmt.Errorf("Adobe Media Encoder did not become ready in time (%s) — is it installed and able to launch on the hub?", detail)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for Adobe Media Encoder was cancelled: %w", ctx.Err())
		case <-time.After(3 * time.Second):
		}
	}
}

// WarmEncoder is the tool-facing wrapper: proactively bring AME up so the
// first user-facing export/preview/frame-capture of a session isn't the one
// that pays the cold-launch cost.
func (e *Engine) WarmEncoder(ctx context.Context) (*GenericResult, error) {
	start := time.Now()
	if err := e.EnsureEncoderReady(ctx, true); err != nil {
		return nil, err
	}
	return &GenericResult{
		Status:  "success",
		Message: fmt.Sprintf("Adobe Media Encoder is ready (warmed in %.0fs).", time.Since(start).Seconds()),
	}, nil
}
