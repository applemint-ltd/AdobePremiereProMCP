package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// Remote review loop: a low-bitrate preview export plus an ffmpeg-backed
// contact sheet over the exported file. Both produce real files a Slack user
// can be shown — the working replacement for the exportFramePNG-based
// storyboard/thumbnail tools that Premiere 2026 removed the API for.
// ---------------------------------------------------------------------------

// ExportPreviewResult reports a preview export honestly: "completed" only
// when the output file exists and has stopped growing.
type ExportPreviewResult struct {
	Status      string  `json:"status"` // completed | queued_not_confirmed
	OutputPath  string  `json:"output_path"`
	SizeBytes   int64   `json:"size_bytes,omitempty"`
	WaitedSecs  float64 `json:"waited_seconds"`
	Note        string  `json:"note,omitempty"`
}

// ExportPreview renders the active sequence to a low-bitrate MP4 under the
// project's Previews folder and waits (bounded) for the file to finish.
func (e *Engine) ExportPreview(ctx context.Context, outputName string) (*ExportPreviewResult, error) {
	if outputName == "" {
		outputName = fmt.Sprintf("preview_%s.mp4", time.Now().Format("20060102_150405"))
	}
	outputPath := filepath.Join(e.projectSubDir(ctx, "Previews"), outputName)

	// Make sure AME is up BEFORE the render's own clock starts, so a cold
	// launch (1-2 min) doesn't get charged against the render timeout and
	// reported as a false "queued_not_confirmed". Non-fatal: if warm-up
	// errors we still try the export.
	if err := e.EnsureEncoderReady(ctx, false); err != nil {
		e.logger.Warn("encoder warm-up before preview failed; trying anyway", zap.Error(err))
	}

	// Same host export path as premiere_export; AME queues the render, so
	// give the call and the file-wait a long leash.
	ctx, cancel := context.WithTimeout(ctx, exportCallTimeout)
	defer cancel()

	argsJSON, _ := json.Marshal(map[string]any{
		"outputPath": outputPath,
		"presetPath": "h264_preview",
	})
	if _, err := e.premiere.EvalCommand(ctx, "exportSequence", string(argsJSON)); err != nil {
		return nil, fmt.Errorf("preview export failed to start: %w", err)
	}

	res := e.awaitExportedFile(ctx, outputPath, 10*time.Minute)
	e.logger.Info("preview export",
		zap.String("path", res.OutputPath),
		zap.String("status", res.Status),
		zap.Int64("bytes", res.SizeBytes),
	)
	return res, nil
}

// awaitExportedFile polls until the output file is done, bounded by
// maxWait/ctx. "Done" means the size held steady across several consecutive
// polls AND the media engine can probe a readable video stream from it — a
// mid-render mux pause can freeze the size for a couple of seconds, so a
// single stable repeat is not enough, and a truncated MP4 won't probe.
// Timing out is reported as queued_not_confirmed — never as success.
func (e *Engine) awaitExportedFile(ctx context.Context, path string, maxWait time.Duration) *ExportPreviewResult {
	const requiredStablePolls = 3 // ~6s of no growth before we even probe
	start := time.Now()
	deadline := start.Add(maxWait)
	var lastSize int64 = -1
	stable := 0

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return &ExportPreviewResult{
				Status: "queued_not_confirmed", OutputPath: path,
				WaitedSecs: time.Since(start).Seconds(),
				Note:       "wait cancelled; the render may still be running in Adobe Media Encoder",
			}
		case <-time.After(2 * time.Second):
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.Size() > 0 && info.Size() == lastSize {
			stable++
		} else {
			stable = 0
		}
		lastSize = info.Size()
		if stable < requiredStablePolls {
			continue
		}
		// Size has held steady; confirm it's a complete, readable file before
		// declaring success (guards against a paused-mid-render truncation).
		if asset, perr := e.media.ProbeMedia(ctx, path); perr != nil || asset == nil || asset.Video == nil || asset.Video.DurationSeconds <= 0 {
			stable = 0 // not a finished video yet; keep waiting
			continue
		}
		return &ExportPreviewResult{
			Status: "completed", OutputPath: path,
			SizeBytes: info.Size(), WaitedSecs: time.Since(start).Seconds(),
		}
	}
	return &ExportPreviewResult{
		Status: "queued_not_confirmed", OutputPath: path,
		WaitedSecs: time.Since(start).Seconds(),
		Note:       "no stable, readable output file appeared in time; check Adobe Media Encoder on the hub",
	}
}

// ContactSheetResult is a rendered grid of frames from a video file.
type ContactSheetResult struct {
	OutputPath string    `json:"output_path"`
	Columns    int       `json:"columns"`
	Rows       int       `json:"rows"`
	Timestamps []float64 `json:"timestamps_seconds"`
	Warnings   []string  `json:"warnings,omitempty"`
}

// GenerateReviewContactSheet samples cols*rows evenly spaced frames from a
// video file via the rust media engine (ffmpeg) and composites them into one
// PNG grid. Works on any file on disk — typically the preview export — and
// therefore sidesteps the removed seq.exportFramePNG entirely.
func (e *Engine) GenerateReviewContactSheet(ctx context.Context, videoPath string, cols, rows int) (*ContactSheetResult, error) {
	if cols <= 0 {
		cols = 4
	}
	if rows <= 0 {
		rows = 3
	}
	if _, err := os.Stat(videoPath); err != nil {
		return nil, fmt.Errorf("video file not found: %s", videoPath)
	}

	asset, err := e.media.ProbeMedia(ctx, videoPath)
	if err != nil {
		return nil, fmt.Errorf("could not probe %s: %w", filepath.Base(videoPath), err)
	}
	if asset == nil || asset.Video == nil || asset.Video.DurationSeconds <= 0 {
		return nil, fmt.Errorf("%s has no readable video stream", filepath.Base(videoPath))
	}
	duration := asset.Video.DurationSeconds

	const thumbW, thumbH = 480, 270
	n := cols * rows
	result := &ContactSheetResult{Columns: cols, Rows: rows}

	sheet := image.NewRGBA(image.Rect(0, 0, cols*thumbW, rows*thumbH))
	draw.Draw(sheet, sheet.Bounds(), image.Black, image.Point{}, draw.Src)

	for i := 0; i < n; i++ {
		ts := duration * (float64(i) + 0.5) / float64(n)
		result.Timestamps = append(result.Timestamps, ts)

		data, err := e.media.GenerateThumbnail(ctx, videoPath, ts, thumbW, thumbH, "png")
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("frame at %.1fs failed: %v", ts, err))
			continue
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("frame at %.1fs undecodable: %v", ts, err))
			continue
		}
		cell := image.Rect((i%cols)*thumbW, (i/cols)*thumbH, (i%cols+1)*thumbW, (i/cols+1)*thumbH)
		draw.Draw(sheet, cell, img, img.Bounds().Min, draw.Src)
	}

	if len(result.Warnings) >= n {
		return nil, fmt.Errorf("no frames could be extracted from %s: %s", filepath.Base(videoPath), result.Warnings[0])
	}

	outPath := filepath.Join(e.projectSubDir(ctx, "Previews"),
		fmt.Sprintf("contact_sheet_%s.png", time.Now().Format("20060102_150405")))
	f, err := os.Create(outPath)
	if err != nil {
		return nil, fmt.Errorf("could not write contact sheet: %w", err)
	}
	defer f.Close()
	if err := png.Encode(f, sheet); err != nil {
		return nil, fmt.Errorf("could not encode contact sheet: %w", err)
	}
	result.OutputPath = outPath
	return result, nil
}
