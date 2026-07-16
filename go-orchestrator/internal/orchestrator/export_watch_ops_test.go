package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func appendEvery(t *testing.T, path string, chunk []byte, interval time.Duration, times int) {
	t.Helper()
	go func() {
		for i := 0; i < times; i++ {
			time.Sleep(interval)
			f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				return
			}
			_, _ = f.Write(chunk)
			_ = f.Close()
		}
	}()
}

func TestAwaitExports_GrowingFileCompletes(t *testing.T) {
	e := newTestEngine(&mockMediaClient{}, nil, &mockPremiereClient{})
	path := filepath.Join(t.TempDir(), "out.bin") // non-video: no probe gate
	appendEvery(t, path, make([]byte, 4096), 100*time.Millisecond, 5)

	res, err := e.AwaitExports(context.Background(), []string{path}, 0.3, 10, 0.1)
	if err != nil {
		t.Fatalf("AwaitExports: %v", err)
	}
	if res.Status != "all_done" {
		t.Fatalf("status = %q, want all_done (files: %+v)", res.Status, res.Files)
	}
	f := res.Files[0]
	if !f.Done || !f.Exists || f.Bytes != 5*4096 {
		t.Fatalf("file status = %+v, want done with %d bytes", f, 5*4096)
	}
	if f.ProbedOK != nil {
		t.Fatalf("non-video file should not be probed, got ProbedOK=%v", *f.ProbedOK)
	}
}

func TestAwaitExports_VideoProbeGatePasses(t *testing.T) {
	media := &mockMediaClient{probeResult: &AssetInfo{Video: &VideoInfo{DurationSeconds: 5}}}
	e := newTestEngine(media, nil, &mockPremiereClient{})
	path := filepath.Join(t.TempDir(), "out.mp4")
	if err := os.WriteFile(path, make([]byte, 1024), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	res, err := e.AwaitExports(context.Background(), []string{path}, 0.2, 10, 0.1)
	if err != nil {
		t.Fatalf("AwaitExports: %v", err)
	}
	f := res.Files[0]
	if res.Status != "all_done" || !f.Done {
		t.Fatalf("want all_done, got %q (%+v)", res.Status, f)
	}
	if f.ProbedOK == nil || !*f.ProbedOK {
		t.Fatalf("video file should have ProbedOK=true, got %+v", f.ProbedOK)
	}
}

func TestAwaitExports_VideoProbeFailureNeverDone(t *testing.T) {
	media := &mockMediaClient{probeErr: fmt.Errorf("moov atom not found")}
	e := newTestEngine(media, nil, &mockPremiereClient{})
	path := filepath.Join(t.TempDir(), "out.mp4")
	if err := os.WriteFile(path, make([]byte, 1024), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	res, err := e.AwaitExports(context.Background(), []string{path}, 0.2, 1, 0.1)
	if err != nil {
		t.Fatalf("AwaitExports: %v", err)
	}
	f := res.Files[0]
	if res.Status != "timeout" || f.Done {
		t.Fatalf("unprobeable video must never be done, got %q (%+v)", res.Status, f)
	}
	if !f.Stable {
		t.Fatalf("size stability should be reported honestly even when the probe fails: %+v", f)
	}
	if f.ProbedOK == nil || *f.ProbedOK {
		t.Fatalf("want ProbedOK=false, got %+v", f.ProbedOK)
	}
}

func TestAwaitExports_MissingPathTimesOutPartial(t *testing.T) {
	e := newTestEngine(&mockMediaClient{}, nil, &mockPremiereClient{})
	res, err := e.AwaitExports(context.Background(), []string{"/nonexistent/nowhere.mp4"}, 0.2, 1, 0.2)
	if err != nil {
		t.Fatalf("timeout must not be an error, got %v", err)
	}
	f := res.Files[0]
	if res.Status != "timeout" || f.Exists || f.Done {
		t.Fatalf("want timeout with exists=false done=false, got %q (%+v)", res.Status, f)
	}
	if f.Note == "" {
		t.Fatalf("pending file should carry an explanatory note")
	}
}

func TestAwaitExports_MixedPaths(t *testing.T) {
	e := newTestEngine(&mockMediaClient{}, nil, &mockPremiereClient{})
	dir := t.TempDir()
	good := filepath.Join(dir, "done.bin")
	if err := os.WriteFile(good, make([]byte, 512), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	missing := filepath.Join(dir, "never.bin")

	res, err := e.AwaitExports(context.Background(), []string{good, missing}, 0.2, 1, 0.1)
	if err != nil {
		t.Fatalf("AwaitExports: %v", err)
	}
	if res.Status != "timeout" {
		t.Fatalf("overall status should be timeout when any file is unfinished, got %q", res.Status)
	}
	if !res.Files[0].Done || res.Files[1].Done {
		t.Fatalf("per-file statuses wrong: %+v", res.Files)
	}
}

func TestAwaitExports_EmptyPathsIsError(t *testing.T) {
	e := newTestEngine(&mockMediaClient{}, nil, &mockPremiereClient{})
	if _, err := e.AwaitExports(context.Background(), nil, 3, 10, 2); err == nil {
		t.Fatalf("empty paths must error")
	}
}

func TestAwaitExports_ContextCancel(t *testing.T) {
	e := newTestEngine(&mockMediaClient{}, nil, &mockPremiereClient{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()
	res, err := e.AwaitExports(ctx, []string{"/nonexistent/never.bin"}, 3, 30, 0.1)
	if err != nil {
		t.Fatalf("cancel must not be an error, got %v", err)
	}
	if res.Status != "cancelled" {
		t.Fatalf("status = %q, want cancelled", res.Status)
	}
	if res.WaitedSecs > 5 {
		t.Fatalf("cancel should return promptly, waited %.1fs", res.WaitedSecs)
	}
}

func TestExportStatus_GrowthRate(t *testing.T) {
	e := newTestEngine(&mockMediaClient{}, nil, &mockPremiereClient{})
	dir := t.TempDir()
	growing := filepath.Join(dir, "growing.mp4")
	static := filepath.Join(dir, "static.mp4")
	if err := os.WriteFile(growing, make([]byte, 1024), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(static, make([]byte, 1024), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	appendEvery(t, growing, make([]byte, 8192), 100*time.Millisecond, 8)

	res, err := e.ExportStatus(context.Background(), []string{growing, static, filepath.Join(dir, "missing.mp4")})
	if err != nil {
		t.Fatalf("ExportStatus: %v", err)
	}
	g, s, m := res.Files[0], res.Files[1], res.Files[2]
	if !g.Growing || g.GrowthBytesPerSec <= 0 {
		t.Fatalf("growing file misreported: %+v", g)
	}
	if s.Growing || !s.Stable {
		t.Fatalf("static file misreported: %+v", s)
	}
	if m.Exists || m.Done {
		t.Fatalf("missing file misreported: %+v", m)
	}
	for _, f := range res.Files {
		if f.Done {
			t.Fatalf("a snapshot must never certify completion: %+v", f)
		}
	}
}
