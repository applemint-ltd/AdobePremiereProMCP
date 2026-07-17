package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// Batch export, rebuilt in Go. The old host-side batchExportSequences /
// exportAllSequences called seq.exportAsMediaDirect, which on Premiere 2026
// returns success while writing NOTHING (verified live) — they were deleted,
// not promoted. This path loops the one verified render route (the
// AME-queueing exportSequence host call, the same one export_preview and
// premiere_export trust) and certifies completion per file with the
// file-truth watcher: a result says done only when the file exists, held a
// stable size, and probed as readable.
// ---------------------------------------------------------------------------

// BatchExportParams selects sequences and delivery options.
type BatchExportParams struct {
	SequenceIDs      []string `json:"sequence_ids,omitempty"`
	SequenceNames    []string `json:"sequence_names,omitempty"`
	Preset           string   `json:"preset"` // semantic key, default h264_1080p
	OutputDir        string   `json:"output_dir"`
	FilenameTemplate string   `json:"filename_template"` // default "{sequence_name}.mp4"
	WarmEncoder      bool     `json:"warm_encoder"`
	Await            string   `json:"await"`              // "all" (default) | "none"
	PerFileTimeoutS  float64  `json:"per_file_timeout_s"` // default 600
	PollIntervalS    float64  `json:"poll_interval_s"`    // default 2
	Overwrite        bool     `json:"overwrite"`
}

// BatchExportItemResult is one sequence's outcome.
type BatchExportItemResult struct {
	SequenceID   string  `json:"sequence_id,omitempty"`
	SequenceName string  `json:"sequence_name,omitempty"`
	Status       string  `json:"status"` // done | timeout | failed | skipped | queued
	Path         string  `json:"path,omitempty"`
	Bytes        int64   `json:"bytes,omitempty"`
	WallSeconds  float64 `json:"wall_seconds,omitempty"`
	Error        string  `json:"error,omitempty"`
}

// BatchExportResult aggregates a batch run.
type BatchExportResult struct {
	Requested int                     `json:"requested"`
	Done      int                     `json:"done"`
	Queued    int                     `json:"queued"`
	Failed    int                     `json:"failed"` // failed + timeout
	Skipped   int                     `json:"skipped"`
	Items     []BatchExportItemResult `json:"items"`
}

// BatchExportViaQueue exports each target sequence through the AME queue and
// (unless await=none) blocks until every output file is confirmed on disk.
// Per-item failures never abort the batch — each item reports honestly.
func (e *Engine) BatchExportViaQueue(ctx context.Context, params *BatchExportParams) (*BatchExportResult, error) {
	if params == nil {
		return nil, fmt.Errorf("batch_export: params must not be nil")
	}
	if params.OutputDir == "" {
		return nil, fmt.Errorf("batch_export: output_dir is required")
	}
	if len(params.SequenceIDs) > 0 && len(params.SequenceNames) > 0 {
		return nil, fmt.Errorf("batch_export: provide sequence_ids or sequence_names, not both")
	}
	preset := params.Preset
	if preset == "" {
		preset = "h264_1080p"
	}
	tpl := params.FilenameTemplate
	if tpl == "" {
		tpl = "{sequence_name}.mp4"
	}
	await := params.Await
	if await == "" {
		await = "all"
	}
	if await != "all" && await != "none" {
		return nil, fmt.Errorf("batch_export: await must be \"all\" or \"none\", got %q", await)
	}
	perFileTimeout := params.PerFileTimeoutS
	if perFileTimeout <= 0 {
		perFileTimeout = 600
	}
	pollInterval := params.PollIntervalS
	if pollInterval <= 0 {
		pollInterval = 2
	}
	if err := os.MkdirAll(params.OutputDir, 0o755); err != nil {
		return nil, fmt.Errorf("batch_export: could not create output_dir: %w", err)
	}

	// Resolve targets against a fresh sequence list — indices go stale the
	// moment sequences are created or deleted, so this happens immediately
	// before queueing, and ids remain the durable handle in results.
	list, err := e.GetSequenceList(ctx)
	if err != nil {
		return nil, fmt.Errorf("batch_export: %w", err)
	}
	byID := map[string]*SequenceListEntry{}
	byName := map[string]*SequenceListEntry{}
	nameCount := map[string]int{}
	for _, entry := range list.Sequences {
		byID[entry.SequenceID] = entry
		nameCount[entry.Name]++
	}
	for _, entry := range list.Sequences {
		// Only unique names are addressable by name; duplicates are handled
		// as an ambiguity error below rather than silently last-wins.
		if nameCount[entry.Name] == 1 {
			byName[entry.Name] = entry
		}
	}

	type workItem struct {
		result BatchExportItemResult
		entry  *SequenceListEntry
		queued time.Time
	}
	var items []*workItem
	addUnresolved := func(idOrName, errMsg string, isID bool) {
		it := &workItem{result: BatchExportItemResult{Status: "failed", Error: errMsg}}
		if isID {
			it.result.SequenceID = idOrName
		} else {
			it.result.SequenceName = idOrName
		}
		items = append(items, it)
	}
	switch {
	case len(params.SequenceIDs) > 0:
		for _, id := range params.SequenceIDs {
			if entry, ok := byID[id]; ok {
				items = append(items, &workItem{entry: entry})
			} else {
				addUnresolved(id, "sequence not found — check premiere_get_sequence_list", true)
			}
		}
	case len(params.SequenceNames) > 0:
		for _, name := range params.SequenceNames {
			if entry, ok := byName[name]; ok {
				items = append(items, &workItem{entry: entry})
			} else if nameCount[name] > 1 {
				addUnresolved(name, fmt.Sprintf("%d sequences share the name %q — target it by sequence_id instead (premiere_get_sequence_list)", nameCount[name], name), false)
			} else {
				addUnresolved(name, "sequence not found — check premiere_get_sequence_list", false)
			}
		}
	default:
		for _, entry := range list.Sequences {
			items = append(items, &workItem{entry: entry})
		}
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("batch_export: the project has no sequences to export")
	}

	// Render output paths; duplicates within the batch are refused up front —
	// two AME jobs writing one file would destroy the file-truth signal.
	usedPaths := map[string]string{}
	for i, it := range items {
		if it.entry == nil {
			continue
		}
		it.result.SequenceID = it.entry.SequenceID
		it.result.SequenceName = it.entry.Name
		name := strings.ReplaceAll(tpl, "{sequence_name}", it.entry.Name)
		name = strings.ReplaceAll(name, "{sequence_id}", it.entry.SequenceID)
		name = strings.ReplaceAll(name, "{index}", fmt.Sprintf("%d", i))
		name = strings.TrimSpace(driveUnsafeFilenameChars.ReplaceAllString(name, "_"))
		if name == "" {
			// An all-unsafe/whitespace template would make path == OutputDir,
			// and with overwrite=true os.Remove would target the batch's own
			// output directory. Refuse it.
			it.result.Status = "failed"
			it.result.Error = "filename_template rendered an empty name for this sequence — include {sequence_name}, {sequence_id}, or {index}"
			it.entry = nil
			continue
		}
		path := filepath.Join(params.OutputDir, name)
		if owner, dup := usedPaths[path]; dup {
			it.result.Status = "failed"
			it.result.Error = fmt.Sprintf("output filename collides with sequence %q — include {sequence_id} or {index} in filename_template", owner)
			it.entry = nil
			continue
		}
		usedPaths[path] = it.entry.Name
		it.result.Path = path

		if _, statErr := os.Stat(path); statErr == nil {
			if !params.Overwrite {
				it.result.Status = "skipped"
				it.result.Error = "output exists; pass overwrite=true to replace it"
				it.entry = nil
				continue
			}
			// A stale file would instantly read as "stable" and fake a done.
			if rmErr := os.Remove(path); rmErr != nil {
				it.result.Status = "failed"
				it.result.Error = fmt.Sprintf("could not remove existing output: %v", rmErr)
				it.entry = nil
				continue
			}
		}
	}

	// Warm AME once so the 1-2 minute cold start isn't charged against the
	// first file's wait. Non-fatal, same as export_preview.
	if params.WarmEncoder {
		if err := e.EnsureEncoderReady(ctx, false); err != nil {
			e.logger.Warn("batch_export: encoder warm-up failed; queueing anyway", zap.Error(err))
		}
	}

	// Queue everything first: AME renders serially and starts on job 1 while
	// the rest are still being queued, so queue-all-then-await-all wastes
	// nothing and overlaps best.
	for _, it := range items {
		if it.entry == nil {
			continue
		}
		if _, err := e.SetActiveSequence(ctx, it.entry.Index); err != nil {
			it.result.Status = "failed"
			it.result.Error = fmt.Sprintf("could not activate sequence: %v", err)
			it.entry = nil
			continue
		}
		argsJSON, _ := json.Marshal(map[string]any{
			"outputPath": it.result.Path,
			"presetPath": preset,
		})
		// exportSequence resolves semantic presets and queues via AME; the
		// call itself can block while AME launches, so it gets the long
		// leash, not the default 30s EvalCommand deadline.
		callCtx, cancel := context.WithTimeout(ctx, exportCallTimeout)
		_, err := e.premiere.EvalCommand(callCtx, "exportSequence", string(argsJSON))
		cancel()
		if err != nil {
			it.result.Status = "failed"
			it.result.Error = fmt.Sprintf("export failed to start: %v", err)
			it.entry = nil
			continue
		}
		it.result.Status = "queued"
		it.queued = time.Now()
	}

	queuedPaths := []string{}
	for _, it := range items {
		if it.result.Status == "queued" {
			queuedPaths = append(queuedPaths, it.result.Path)
		}
	}

	if await == "all" && len(queuedPaths) > 0 {
		// One watcher over every queued file; AME renders in queue order, so
		// a shared budget of perFileTimeout per file is the honest bound.
		awaitStart := time.Now()
		watch, werr := e.AwaitExports(ctx, queuedPaths,
			3*pollInterval, perFileTimeout*float64(len(queuedPaths)), pollInterval)
		if werr == nil {
			statusByPath := map[string]ExportFileStatus{}
			for _, f := range watch.Files {
				statusByPath[f.Path] = f
			}
			for _, it := range items {
				if it.result.Status != "queued" {
					continue
				}
				f := statusByPath[it.result.Path]
				it.result.WallSeconds = awaitStart.Sub(it.queued).Seconds() + f.WaitedSecs
				if f.Done {
					it.result.Status = "done"
					it.result.Bytes = f.Bytes
				} else {
					it.result.Status = "timeout"
					it.result.Error = "no stable, readable output file appeared in time; the render may still be running — check Adobe Media Encoder or re-check with premiere_await_exports"
				}
			}
		}
	}

	res := &BatchExportResult{Requested: len(items)}
	for _, it := range items {
		switch it.result.Status {
		case "done":
			res.Done++
		case "queued":
			res.Queued++
		case "skipped":
			res.Skipped++
		default:
			res.Failed++
		}
		res.Items = append(res.Items, it.result)
		e.logger.Info("batch_export item",
			zap.String("sequence", it.result.SequenceName),
			zap.String("status", it.result.Status),
			zap.String("path", it.result.Path),
			zap.Int64("bytes", it.result.Bytes),
		)
	}
	return res, nil
}
