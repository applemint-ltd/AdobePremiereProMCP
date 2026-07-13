package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Frame Capture Operations
// ---------------------------------------------------------------------------

// captureCallTimeout allows for a real single-frame render: the in-process
// encoder can take ~90s cold on a heavy sequence, well past the default 30s
// bridge deadline.
const captureCallTimeout = 3 * time.Minute

// CaptureFrameAsBase64 captures the current frame at the playhead position
// and returns it as a base64-encoded image.
func (e *Engine) CaptureFrameAsBase64(ctx context.Context) (*FrameCaptureResult, error) {
	ctx, cancel := context.WithTimeout(ctx, captureCallTimeout)
	defer cancel()

	raw, err := e.premiere.EvalCommand(ctx, "captureFrameAsBase64", "{}")
	if err != nil {
		return nil, fmt.Errorf("CaptureFrameAsBase64: %w", err)
	}

	// EvalCommand already unwraps the {success,data} envelope; raw is the
	// inner payload (success:false became a Go error above).
	var data struct {
		ImageBase64 string  `json:"image_base64"`
		Format      string  `json:"format"`
		Width       int     `json:"width"`
		Height      int     `json:"height"`
		Timecode    float64 `json:"timecode"`
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, fmt.Errorf("CaptureFrameAsBase64: failed to parse response: %w", err)
	}
	if data.ImageBase64 == "" {
		return nil, fmt.Errorf("CaptureFrameAsBase64: host returned no image data")
	}

	return &FrameCaptureResult{
		ImageBase64: data.ImageBase64,
		Format:      data.Format,
		Width:       data.Width,
		Height:      data.Height,
		Timecode:    data.Timecode,
	}, nil
}

// ---------------------------------------------------------------------------
// Secure ExtendScript Execution Operations
// ---------------------------------------------------------------------------

// ExecuteSecureScript runs an arbitrary ExtendScript string with optional
// security validation that blocks dangerous operations such as system calls,
// file deletion, and infinite loops.
func (e *Engine) ExecuteSecureScript(ctx context.Context, script string, validate bool) (*GenericResult, error) {
	argsJSON, _ := json.Marshal(map[string]any{
		"script":   script,
		"validate": validate,
	})
	result, err := e.premiere.EvalCommand(ctx, "executeSecureScript", string(argsJSON))
	if err != nil {
		return nil, fmt.Errorf("ExecuteSecureScript: %w", err)
	}
	return &GenericResult{Status: "success", Message: result}, nil
}

// ExecuteQEScript runs an arbitrary ExtendScript string with QE DOM access
// enabled. The QE (Quality Engineering) DOM provides access to internal
// Premiere Pro functionality not available through the standard DOM.
func (e *Engine) ExecuteQEScript(ctx context.Context, script string) (*GenericResult, error) {
	argsJSON, _ := json.Marshal(map[string]any{
		"script": script,
	})
	result, err := e.premiere.EvalCommand(ctx, "executeQEScript", string(argsJSON))
	if err != nil {
		return nil, fmt.Errorf("ExecuteQEScript: %w", err)
	}
	return &GenericResult{Status: "success", Message: result}, nil
}
