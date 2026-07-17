package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cannedSequenceList is what the mock host returns for getSequenceList.
const cannedSequenceList = `{
	"count": 3,
	"active_sequence_id": "seq-b",
	"sequences": [
		{"index": 0, "name": "EXP014-hookA", "sequence_id": "seq-a"},
		{"index": 1, "name": "EXP014-hookB", "sequence_id": "seq-b"},
		{"index": 2, "name": "Hook A: v/1", "sequence_id": "seq-c"}
	]
}`

func newBatchTestEngine() (*Engine, *mockPremiereClient) {
	premiere := &mockPremiereClient{
		evalResults: map[string]string{
			"getSequenceList":   cannedSequenceList,
			"setActiveSequence": `{"ok":true}`,
			"exportSequence":    `{"queued":true}`,
		},
	}
	media := &mockMediaClient{probeResult: &AssetInfo{Video: &VideoInfo{DurationSeconds: 5}}}
	return newTestEngine(media, nil, premiere), premiere
}

// callsTo filters captured EvalCommand calls by host function name.
func callsTo(premiere *mockPremiereClient, fn string) []evalCall {
	var out []evalCall
	for _, c := range premiere.evalCalls {
		if c.Fn == fn {
			out = append(out, c)
		}
	}
	return out
}

// WarmEncoder stays false in every test: EnsureEncoderReady's ready-latch is
// process-global and would poll getExporters against the mock.

func TestBatchExport_ResolvesIDToIndex(t *testing.T) {
	e, premiere := newBatchTestEngine()
	res, err := e.BatchExportViaQueue(context.Background(), &BatchExportParams{
		SequenceIDs: []string{"seq-b"},
		OutputDir:   t.TempDir(),
		Await:       "none",
	})
	if err != nil {
		t.Fatalf("BatchExportViaQueue: %v", err)
	}
	if res.Queued != 1 || res.Items[0].Status != "queued" {
		t.Fatalf("want 1 queued, got %+v", res)
	}
	activations := callsTo(premiere, "setActiveSequence")
	if len(activations) != 1 {
		t.Fatalf("want exactly one setActiveSequence call, got %d", len(activations))
	}
	var args struct {
		SequenceIndex int `json:"sequenceIndex"`
	}
	if err := json.Unmarshal([]byte(activations[0].Args), &args); err != nil {
		t.Fatalf("bad setActiveSequence args: %v", err)
	}
	if args.SequenceIndex != 1 {
		t.Fatalf("seq-b must resolve to index 1, got %d", args.SequenceIndex)
	}
}

func TestBatchExport_ResolvesByNameAndRejectsBoth(t *testing.T) {
	e, _ := newBatchTestEngine()
	res, err := e.BatchExportViaQueue(context.Background(), &BatchExportParams{
		SequenceNames: []string{"EXP014-hookA"},
		OutputDir:     t.TempDir(),
		Await:         "none",
	})
	if err != nil || res.Queued != 1 || res.Items[0].SequenceID != "seq-a" {
		t.Fatalf("by-name resolution failed: %v / %+v", err, res)
	}

	if _, err := e.BatchExportViaQueue(context.Background(), &BatchExportParams{
		SequenceIDs:   []string{"seq-a"},
		SequenceNames: []string{"EXP014-hookB"},
		OutputDir:     t.TempDir(),
	}); err == nil {
		t.Fatalf("ids and names together must be rejected")
	}
}

func TestBatchExport_UnknownIDFailsItemOnly(t *testing.T) {
	e, _ := newBatchTestEngine()
	res, err := e.BatchExportViaQueue(context.Background(), &BatchExportParams{
		SequenceIDs: []string{"seq-bogus", "seq-a"},
		OutputDir:   t.TempDir(),
		Await:       "none",
	})
	if err != nil {
		t.Fatalf("BatchExportViaQueue: %v", err)
	}
	if res.Failed != 1 || res.Queued != 1 {
		t.Fatalf("want 1 failed + 1 queued, got %+v", res)
	}
	if res.Items[0].Status != "failed" || !strings.Contains(res.Items[0].Error, "not found") {
		t.Fatalf("unknown id item wrong: %+v", res.Items[0])
	}
	if res.Items[1].Status != "queued" {
		t.Fatalf("valid item must still queue: %+v", res.Items[1])
	}
}

func TestBatchExport_FilenameTemplateAndSanitization(t *testing.T) {
	e, premiere := newBatchTestEngine()
	dir := t.TempDir()
	res, err := e.BatchExportViaQueue(context.Background(), &BatchExportParams{
		SequenceIDs: []string{"seq-c"}, // name "Hook A: v/1"
		OutputDir:   dir,
		Await:       "none",
	})
	if err != nil {
		t.Fatalf("BatchExportViaQueue: %v", err)
	}
	got := filepath.Base(res.Items[0].Path)
	if strings.ContainsAny(got, `/:*?"<>|`) {
		t.Fatalf("unsafe characters survived sanitization: %q", got)
	}
	exports := callsTo(premiere, "exportSequence")
	if len(exports) != 1 || !strings.Contains(exports[0].Args, got) {
		t.Fatalf("exportSequence args must carry the sanitized path, got %+v", exports)
	}
}

func TestBatchExport_OverwriteFalseSkips(t *testing.T) {
	e, premiere := newBatchTestEngine()
	dir := t.TempDir()
	pre := filepath.Join(dir, "EXP014-hookA.mp4")
	if err := os.WriteFile(pre, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	res, err := e.BatchExportViaQueue(context.Background(), &BatchExportParams{
		SequenceIDs: []string{"seq-a"},
		OutputDir:   dir,
		Await:       "none",
	})
	if err != nil {
		t.Fatalf("BatchExportViaQueue: %v", err)
	}
	if res.Skipped != 1 || res.Items[0].Status != "skipped" {
		t.Fatalf("existing output must be skipped without overwrite: %+v", res)
	}
	if len(callsTo(premiere, "exportSequence")) != 0 {
		t.Fatalf("skipped item must not be queued")
	}
}

func TestBatchExport_OverwriteTrueRemovesStaleFile(t *testing.T) {
	e, _ := newBatchTestEngine()
	dir := t.TempDir()
	pre := filepath.Join(dir, "EXP014-hookA.mp4")
	if err := os.WriteFile(pre, []byte("stale bytes from yesterday"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	res, err := e.BatchExportViaQueue(context.Background(), &BatchExportParams{
		SequenceIDs: []string{"seq-a"},
		OutputDir:   dir,
		Overwrite:   true,
		Await:       "none",
	})
	if err != nil {
		t.Fatalf("BatchExportViaQueue: %v", err)
	}
	if res.Queued != 1 {
		t.Fatalf("overwrite item must queue: %+v", res)
	}
	// The stale file would instantly read as size-stable and fake a "done" —
	// it must be gone before the queue call.
	if _, statErr := os.Stat(pre); statErr == nil {
		t.Fatalf("stale output must be removed before queueing")
	}
}

func TestBatchExport_DuplicateOutputCollision(t *testing.T) {
	e, _ := newBatchTestEngine()
	res, err := e.BatchExportViaQueue(context.Background(), &BatchExportParams{
		SequenceIDs:      []string{"seq-a", "seq-b"},
		OutputDir:        t.TempDir(),
		FilenameTemplate: "fixed.mp4",
		Await:            "none",
	})
	if err != nil {
		t.Fatalf("BatchExportViaQueue: %v", err)
	}
	if res.Queued != 1 || res.Failed != 1 {
		t.Fatalf("second collision item must fail pre-queue: %+v", res)
	}
	if !strings.Contains(res.Items[1].Error, "collides") {
		t.Fatalf("collision error missing: %+v", res.Items[1])
	}
}

func TestBatchExport_AwaitAllHonest(t *testing.T) {
	e, _ := newBatchTestEngine()
	dir := t.TempDir()

	// The mock queues instantly; simulate AME by writing outputs for hookA
	// and hookB shortly after, and never writing the third.
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(dir, "EXP014-hookA.mp4"), make([]byte, 4096), 0o644)
		_ = os.WriteFile(filepath.Join(dir, "EXP014-hookB.mp4"), make([]byte, 4096), 0o644)
	}()

	res, err := e.BatchExportViaQueue(context.Background(), &BatchExportParams{
		OutputDir:       dir, // no targets = every sequence (all 3)
		Await:           "all",
		PerFileTimeoutS: 1,
		PollIntervalS:   0.1,
	})
	if err != nil {
		t.Fatalf("BatchExportViaQueue: %v", err)
	}
	byName := map[string]BatchExportItemResult{}
	for _, it := range res.Items {
		byName[it.SequenceName] = it
	}
	if byName["EXP014-hookA"].Status != "done" || byName["EXP014-hookB"].Status != "done" {
		t.Fatalf("written files must be done: %+v", res.Items)
	}
	if byName["Hook A: v/1"].Status != "timeout" {
		t.Fatalf("never-written file must be timeout, never success: %+v", byName["Hook A: v/1"])
	}
	if res.Done != 2 || res.Failed != 1 {
		t.Fatalf("aggregate counts wrong: %+v", res)
	}
}

func TestBatchExport_DuplicateNameIsAmbiguous(t *testing.T) {
	// A project with two sequences sharing a name: targeting that name must
	// fail as ambiguous, not silently pick one (last-wins).
	premiere := &mockPremiereClient{
		evalResults: map[string]string{
			"getSequenceList": `{"count":2,"sequences":[
				{"index":0,"name":"Final","sequence_id":"seq-old"},
				{"index":1,"name":"Final","sequence_id":"seq-new"}]}`,
			"setActiveSequence": `{"ok":true}`,
			"exportSequence":    `{"queued":true}`,
		},
	}
	e := newTestEngine(&mockMediaClient{}, nil, premiere)
	res, err := e.BatchExportViaQueue(context.Background(), &BatchExportParams{
		SequenceNames: []string{"Final"}, OutputDir: t.TempDir(), Await: "none",
	})
	if err != nil {
		t.Fatalf("BatchExportViaQueue: %v", err)
	}
	if res.Failed != 1 || res.Queued != 0 {
		t.Fatalf("ambiguous name must fail, not export: %+v", res)
	}
	if !strings.Contains(res.Items[0].Error, "share the name") {
		t.Fatalf("want an ambiguity error, got: %+v", res.Items[0])
	}
	if len(callsTo(premiere, "exportSequence")) != 0 {
		t.Fatalf("nothing should be queued for an ambiguous name")
	}
}

func TestBatchExport_EmptyFilenameRefused(t *testing.T) {
	e, _ := newBatchTestEngine()
	res, err := e.BatchExportViaQueue(context.Background(), &BatchExportParams{
		SequenceIDs: []string{"seq-a"}, OutputDir: t.TempDir(),
		FilenameTemplate: "   ", // whitespace-only → TrimSpace to empty
		Await:            "none",
	})
	if err != nil {
		t.Fatalf("BatchExportViaQueue: %v", err)
	}
	if res.Failed != 1 || !strings.Contains(res.Items[0].Error, "empty name") {
		t.Fatalf("empty rendered filename must be refused (never os.Remove the output dir): %+v", res.Items[0])
	}
}

func TestBatchExport_AwaitNoneReturnsQueued(t *testing.T) {
	e, _ := newBatchTestEngine()
	res, err := e.BatchExportViaQueue(context.Background(), &BatchExportParams{
		OutputDir: t.TempDir(),
		Await:     "none",
	})
	if err != nil {
		t.Fatalf("BatchExportViaQueue: %v", err)
	}
	if res.Queued != 3 || res.Done != 0 {
		t.Fatalf("await=none must return queued items: %+v", res)
	}
}

func TestExport_EmptySequenceIDUsesActive(t *testing.T) {
	premiere := &mockPremiereClient{
		evalResults: map[string]string{
			"getActiveSequence": `{"sequence_id": "seq-active", "name": "Active Cut"}`,
		},
		exportResult: &ExportResult{JobID: "job-1", Status: "queued"},
	}
	e := newTestEngine(&mockMediaClient{}, nil, premiere)
	res, err := e.Export(context.Background(), &ExportParams{OutputPath: "/tmp/x.mp4"})
	if err != nil {
		t.Fatalf("Export with empty sequence_id must fall back to active: %v", err)
	}
	if res.JobID != "job-1" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestExport_EmptySequenceIDNoActiveFails(t *testing.T) {
	premiere := &mockPremiereClient{evalErr: contextDeadline{}}
	e := newTestEngine(&mockMediaClient{}, nil, premiere)
	_, err := e.Export(context.Background(), &ExportParams{OutputPath: "/tmp/x.mp4"})
	if err == nil || !strings.Contains(err.Error(), "no active sequence") {
		t.Fatalf("want a no-active-sequence error, got: %v", err)
	}
}

// contextDeadline is a trivial error for the no-active-sequence case.
type contextDeadline struct{}

func (contextDeadline) Error() string { return "no active sequence" }
