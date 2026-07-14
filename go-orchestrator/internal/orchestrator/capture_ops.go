package orchestrator

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"time"
)

// ---------------------------------------------------------------------------
// Frame Capture Operations
// ---------------------------------------------------------------------------

// captureCallTimeout is a generous ceiling (not a fixed wait): a frame
// capture now renders a preview via AME, which on a cold Media Encoder can
// spend a couple of minutes launching before the render even starts. Warm
// calls still return in seconds.
const captureCallTimeout = 8 * time.Minute

// CaptureFrameAsBase64 captures the current frame at the playhead position
// and returns it as a base64-encoded PNG.
//
// Premiere 2026 has NO working single-still export via scripting:
// exportAsMediaDirect reports "No Error" but writes nothing, and
// encodeSequence with a PNG/still-sequence preset returns a job id but AME
// never produces a file (both verified live). The only render path that
// works is H.264 via AME. So this exports a preview through the proven
// export path and pulls the requested frame out of it with the media
// engine's ffmpeg thumbnailer — the same extractor premiere_generate_
// contact_sheet uses. Heavier than a native still (it renders the sequence
// once), but reliable, which is the whole point of "show me the frame".
func (e *Engine) CaptureFrameAsBase64(ctx context.Context) (*FrameCaptureResult, error) {
	ctx, cancel := context.WithTimeout(ctx, captureCallTimeout)
	defer cancel()

	// Where is the playhead? Best-effort; default to the start.
	atSeconds := 0.0
	if ph, err := e.GetPlayheadPosition(ctx); err == nil {
		atSeconds = ph.Seconds
	}

	previewName := fmt.Sprintf("frame_capture_%d.mp4", time.Now().UnixNano())
	preview, err := e.ExportPreview(ctx, previewName)
	if err != nil {
		return nil, fmt.Errorf("CaptureFrameAsBase64: could not render a preview to grab the frame from: %w", err)
	}
	if preview.Status != "completed" || preview.OutputPath == "" {
		return nil, fmt.Errorf("CaptureFrameAsBase64: preview render did not complete (%s); Adobe Media Encoder may still be starting", preview.Status)
	}
	defer os.Remove(preview.OutputPath)

	// Extract at the preview's native size; the ffmpeg thumbnailer needs a
	// concrete WxH (0x0 makes it fail). Clamp the timestamp inside the
	// rendered file's duration.
	width, height := 1280, 720
	if asset, perr := e.media.ProbeMedia(ctx, preview.OutputPath); perr == nil && asset != nil && asset.Video != nil {
		if asset.Video.Resolution.Width > 0 && asset.Video.Resolution.Height > 0 {
			width = int(asset.Video.Resolution.Width)
			height = int(asset.Video.Resolution.Height)
		}
		if dur := asset.Video.DurationSeconds; dur > 0 && atSeconds > dur-0.05 {
			atSeconds = dur / 2
		}
	}

	png, err := e.media.GenerateThumbnail(ctx, preview.OutputPath, atSeconds, width, height, "png")
	if err != nil {
		return nil, fmt.Errorf("CaptureFrameAsBase64: could not extract the frame: %w", err)
	}
	if len(png) == 0 {
		return nil, fmt.Errorf("CaptureFrameAsBase64: frame extraction returned no image data")
	}

	res := &FrameCaptureResult{
		ImageBase64: base64.StdEncoding.EncodeToString(png),
		Format:      "png",
		Timecode:    atSeconds,
	}
	if cfg, _, derr := image.DecodeConfig(bytes.NewReader(png)); derr == nil {
		res.Width, res.Height = cfg.Width, cfg.Height
	}
	return res, nil
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
