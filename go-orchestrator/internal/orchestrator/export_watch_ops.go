package orchestrator

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Export watching: the filesystem is the only honest export truth on
// Premiere 2026 — exports queue in Adobe Media Encoder, the scripting API
// exposes no real queue state, and "success" from an export call means
// queued, not rendered. These primitives generalize the awaitExportedFile
// pattern that export_preview already relies on.
// ---------------------------------------------------------------------------

// ExportFileStatus is the per-path outcome/snapshot.
type ExportFileStatus struct {
	Path              string  `json:"path"`
	Exists            bool    `json:"exists"`
	Bytes             int64   `json:"bytes"`
	Growing           bool    `json:"growing"`
	Stable            bool    `json:"stable"`
	Done              bool    `json:"done"`
	ProbedOK          *bool   `json:"probed_ok,omitempty"` // nil = probe not applicable/attempted
	WaitedSecs        float64 `json:"waited_seconds"`
	GrowthBytesPerSec float64 `json:"growth_bytes_per_sec,omitempty"`
	Note              string  `json:"note,omitempty"`
}

// AwaitExportsResult aggregates per-file outcomes.
type AwaitExportsResult struct {
	Status     string             `json:"status"` // all_done | timeout | cancelled | snapshot
	Files      []ExportFileStatus `json:"files"`
	WaitedSecs float64            `json:"waited_seconds"`
}

// videoExtensions gates the media-engine probe: the probe hard-requires a
// readable video stream, which would keep audio/caption/still outputs
// "not done" forever.
var videoExtensions = map[string]bool{
	".mp4": true, ".mov": true, ".m4v": true, ".mxf": true,
	".avi": true, ".mkv": true, ".webm": true,
}

// probeVideoOK is the completeness gate awaitExportedFile has always used:
// a truncated MP4 (mid-render mux pause) stats stable but won't probe.
func (e *Engine) probeVideoOK(ctx context.Context, path string) bool {
	asset, err := e.media.ProbeMedia(ctx, path)
	return err == nil && asset != nil && asset.Video != nil && asset.Video.DurationSeconds > 0
}

// videoProbeFor returns the probe gate for a path, or nil when probing is
// not applicable (non-video extension, or no media engine wired).
func (e *Engine) videoProbeFor(path string) func(context.Context, string) bool {
	if e.media == nil || !videoExtensions[strings.ToLower(filepath.Ext(path))] {
		return nil
	}
	return e.probeVideoOK
}

// fileWatchState is the per-path polling state machine shared by
// awaitExportedFile and AwaitExports. Semantics: size>0 held across
// consecutive polls builds stability; any change, disappearance, or failed
// probe resets it; the probe gate runs only once size stability is reached.
type fileWatchState struct {
	path          string
	lastSize      int64
	stableCount   int
	exists        bool
	growing       bool
	reachedStable bool // size held to threshold at least once (sticky)
	probedOK      *bool
	done          bool
	doneAtSecs    float64
}

func newFileWatchState(path string) *fileWatchState {
	return &fileWatchState{path: path, lastSize: -1}
}

func (st *fileWatchState) tick(ctx context.Context, stablePolls int, probe func(context.Context, string) bool, start time.Time) {
	info, err := os.Stat(st.path)
	if err != nil {
		st.exists = false
		st.growing = false
		st.stableCount = 0
		st.lastSize = -1
		return
	}
	st.exists = true
	size := info.Size()
	st.growing = st.lastSize >= 0 && size > st.lastSize
	if size > 0 && size == st.lastSize {
		st.stableCount++
	} else {
		st.stableCount = 0
	}
	st.lastSize = size
	if st.stableCount < stablePolls {
		return
	}
	st.reachedStable = true
	if probe != nil {
		ok := probe(ctx, st.path)
		st.probedOK = &ok
		if !ok {
			st.stableCount = 0 // not a finished file yet; keep waiting
			return
		}
	}
	st.done = true
	st.doneAtSecs = time.Since(start).Seconds()
}

func (st *fileWatchState) status(totalWaited float64, pendingNote string) ExportFileStatus {
	fs := ExportFileStatus{
		Path:       st.path,
		Exists:     st.exists,
		Growing:    st.growing,
		Stable:     st.reachedStable,
		Done:       st.done,
		ProbedOK:   st.probedOK,
		WaitedSecs: totalWaited,
	}
	if st.lastSize > 0 {
		fs.Bytes = st.lastSize
	}
	if st.done {
		fs.WaitedSecs = st.doneAtSecs
	} else {
		fs.Note = pendingNote
	}
	return fs
}

// AwaitExports blocks until every path is a stable, readable file, or the
// timeout/context expires — in which case it returns the partial per-file
// state, never an error. All unfinished paths are polled in one loop so a
// batch of N files costs one watcher, not N sequential timeouts.
func (e *Engine) AwaitExports(ctx context.Context, paths []string, stableForS, timeoutS, pollIntervalS float64) (*AwaitExportsResult, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("await_exports: at least one path is required")
	}
	if stableForS <= 0 {
		stableForS = 3
	}
	if timeoutS <= 0 {
		timeoutS = 600
	}
	if pollIntervalS <= 0 {
		pollIntervalS = 2
	}
	if pollIntervalS < 0.2 {
		pollIntervalS = 0.2
	}
	poll := time.Duration(pollIntervalS * float64(time.Second))
	stablePolls := int(math.Ceil(stableForS / pollIntervalS))
	if stablePolls < 1 {
		stablePolls = 1
	}

	start := time.Now()
	deadline := start.Add(time.Duration(timeoutS * float64(time.Second)))
	states := make([]*fileWatchState, len(paths))
	for i, p := range paths {
		states[i] = newFileWatchState(p)
	}

	status := "timeout"
	pendingNote := "no stable, readable output file appeared in time; check Adobe Media Encoder on the hub"
watch:
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			status = "cancelled"
			pendingNote = "wait cancelled; the render may still be running in Adobe Media Encoder"
			break watch
		case <-time.After(poll):
		}
		remaining := 0
		for _, st := range states {
			if st.done {
				continue
			}
			st.tick(ctx, stablePolls, e.videoProbeFor(st.path), start)
			if !st.done {
				remaining++
			}
		}
		if remaining == 0 {
			status = "all_done"
			break
		}
	}

	res := &AwaitExportsResult{Status: status, WaitedSecs: time.Since(start).Seconds()}
	for _, st := range states {
		res.Files = append(res.Files, st.status(res.WaitedSecs, pendingNote))
	}
	return res, nil
}

// ExportStatus is a non-blocking (~1s) snapshot: bytes on disk plus a
// growth rate — honest progress without a fake percentage. A snapshot
// never certifies completion; use AwaitExports for that.
func (e *Engine) ExportStatus(ctx context.Context, paths []string) (*AwaitExportsResult, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("export_status: at least one path is required")
	}
	type sample struct {
		exists bool
		size   int64
	}
	first := make([]sample, len(paths))
	for i, p := range paths {
		if info, err := os.Stat(p); err == nil {
			first[i] = sample{true, info.Size()}
		}
	}
	start := time.Now()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
	}
	elapsed := time.Since(start).Seconds()

	res := &AwaitExportsResult{Status: "snapshot", WaitedSecs: elapsed}
	for i, p := range paths {
		fs := ExportFileStatus{Path: p, WaitedSecs: elapsed}
		if info, err := os.Stat(p); err == nil {
			fs.Exists = true
			fs.Bytes = info.Size()
			if first[i].exists {
				delta := info.Size() - first[i].size
				fs.Growing = delta > 0
				if elapsed > 0 {
					fs.GrowthBytesPerSec = float64(delta) / elapsed
				}
			} else {
				fs.Growing = true // appeared during the window
				fs.Note = "file appeared during the snapshot window"
			}
			fs.Stable = fs.Bytes > 0 && !fs.Growing
		} else {
			fs.Note = "file does not exist yet"
		}
		res.Files = append(res.Files, fs)
	}
	return res, nil
}
