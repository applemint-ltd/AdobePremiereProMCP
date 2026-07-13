package orchestrator

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
//
// The host's exportFrame QUEUES a one-frame render in Adobe Media Encoder
// and returns immediately — on Premiere 2026 the old synchronous
// exportAsMediaDirect path reports "No Error" without ever writing a file,
// and polling inside ExtendScript blocks the engine the render needs. So
// the file wait and the base64 encoding both happen here, engine-free.
func (e *Engine) CaptureFrameAsBase64(ctx context.Context) (*FrameCaptureResult, error) {
	ctx, cancel := context.WithTimeout(ctx, captureCallTimeout)
	defer cancel()

	outPath := filepath.Join(os.TempDir(), fmt.Sprintf("mcp_frame_%d.png", time.Now().UnixNano()))
	argsJSON, _ := json.Marshal(map[string]any{"outputPath": outPath, "format": "PNG"})
	raw, err := e.premiere.EvalCommand(ctx, "exportFrame", string(argsJSON))
	if err != nil {
		return nil, fmt.Errorf("CaptureFrameAsBase64: %w", err)
	}

	var data struct {
		OutputPath string  `json:"output_path"`
		Format     string  `json:"format"`
		Width      int     `json:"width"`
		Height     int     `json:"height"`
		Time       float64 `json:"time_seconds"`
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, fmt.Errorf("CaptureFrameAsBase64: failed to parse response: %w", err)
	}

	finalPath, err := awaitFrameFile(ctx, outPath)
	if err != nil {
		return nil, fmt.Errorf("CaptureFrameAsBase64: %w", err)
	}
	defer os.Remove(finalPath)

	bytes, err := os.ReadFile(finalPath)
	if err != nil {
		return nil, fmt.Errorf("CaptureFrameAsBase64: read rendered frame: %w", err)
	}

	format := strings.ToLower(data.Format)
	if format == "" {
		format = "png"
	}
	return &FrameCaptureResult{
		ImageBase64: base64.StdEncoding.EncodeToString(bytes),
		Format:      format,
		Width:       data.Width,
		Height:      data.Height,
		Timecode:    data.Time,
	}, nil
}

// awaitFrameFile polls (bounded by ctx) until the queued single-frame
// render lands on disk with a stable size. Still-sequence presets may add a
// numeric suffix before the extension ("frame.png" -> "frame00000.png"), so
// sibling variants are matched too.
func awaitFrameFile(ctx context.Context, outPath string) (string, error) {
	ext := filepath.Ext(outPath)
	base := strings.TrimSuffix(filepath.Base(outPath), ext)
	dir := filepath.Dir(outPath)

	find := func() string {
		if info, err := os.Stat(outPath); err == nil && info.Size() > 0 {
			return outPath
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return ""
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasPrefix(name, base) || !strings.HasSuffix(name, ext) {
				continue
			}
			mid := strings.TrimSuffix(strings.TrimPrefix(name, base), ext)
			isNum := mid != ""
			for _, r := range mid {
				if r < '0' || r > '9' {
					isNum = false
					break
				}
			}
			if isNum {
				return filepath.Join(dir, name)
			}
		}
		return ""
	}

	var lastSize int64 = -1
	var lastPath string
	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("frame render did not finish in time (Adobe Media Encoder may still be starting); the job stays queued")
		case <-time.After(500 * time.Millisecond):
		}
		p := find()
		if p == "" {
			continue
		}
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if p == lastPath && info.Size() > 0 && info.Size() == lastSize {
			return p, nil
		}
		lastPath, lastSize = p, info.Size()
	}
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
