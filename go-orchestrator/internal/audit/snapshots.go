package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CaptureFunc produces the current timeline state as JSON (the unwrapped
// snapshotTimeline payload). Injected so this package needs no dependency on
// the orchestrator.
type CaptureFunc func(ctx context.Context) (string, error)

// StoredSnapshot is a persisted timeline snapshot plus provenance.
type StoredSnapshot struct {
	CID      string          `json:"cid"`
	Session  string          `json:"session,omitempty"`
	TS       string          `json:"ts"`
	Timeline json.RawMessage `json:"timeline"`
}

// SnapshotStore persists pre-mutation timeline snapshots keyed by
// correlation ID under <dir>/snapshots/YYYY-MM-DD/. This is the baseline
// store that timeline diffing needs: without a stored "before", there is
// nothing to compare the live timeline against.
type SnapshotStore struct {
	dir     string
	session string
	capture CaptureFunc
}

// NewSnapshotStore creates a store rooted at auditDir. Returns nil when
// auditDir is empty or capture is nil.
func NewSnapshotStore(auditDir, session string, capture CaptureFunc) *SnapshotStore {
	if auditDir == "" || capture == nil {
		return nil
	}
	return &SnapshotStore{dir: filepath.Join(auditDir, "snapshots"), session: session, capture: capture}
}

// SnapshotBefore captures the live timeline and persists it for cid,
// returning the file path recorded in the audit line.
func (s *SnapshotStore) SnapshotBefore(ctx context.Context, cid string) (string, error) {
	if s == nil {
		return "", nil
	}
	timeline, err := s.capture(ctx)
	if err != nil {
		return "", fmt.Errorf("capture timeline: %w", err)
	}
	if !json.Valid([]byte(timeline)) {
		return "", fmt.Errorf("capture timeline: non-JSON payload (%s)", Truncate(timeline, 120))
	}

	now := time.Now()
	dayDir := filepath.Join(s.dir, now.Format("2006-01-02"))
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		return "", fmt.Errorf("snapshot dir: %w", err)
	}

	stored := StoredSnapshot{
		CID:      cid,
		Session:  s.session,
		TS:       now.UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		Timeline: json.RawMessage(timeline),
	}
	data, err := json.Marshal(stored)
	if err != nil {
		return "", fmt.Errorf("snapshot marshal: %w", err)
	}

	path := filepath.Join(dayDir, cid+"-before.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("snapshot write: %w", err)
	}
	return path, nil
}

// Load returns the stored snapshot for a correlation ID, searching the last
// seven day-directories.
func (s *SnapshotStore) Load(cid string) (*StoredSnapshot, error) {
	if s == nil {
		return nil, fmt.Errorf("snapshot store disabled")
	}
	for i := 0; i < 7; i++ {
		day := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		path := filepath.Join(s.dir, day, cid+"-before.json")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var stored StoredSnapshot
		if err := json.Unmarshal(data, &stored); err != nil {
			return nil, fmt.Errorf("snapshot parse %s: %w", path, err)
		}
		return &stored, nil
	}
	return nil, fmt.Errorf("no snapshot found for correlation ID %s", cid)
}

// LatestBefore returns the most recent stored snapshot for a session (any
// session when the tag is empty), searching the last seven days.
func (s *SnapshotStore) LatestBefore(session string) (*StoredSnapshot, error) {
	if s == nil {
		return nil, fmt.Errorf("snapshot store disabled")
	}
	for i := 0; i < 7; i++ {
		day := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		dayDir := filepath.Join(s.dir, day)
		entries, err := os.ReadDir(dayDir)
		if err != nil {
			continue
		}
		type candidate struct {
			path string
			mod  time.Time
		}
		var cands []candidate
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), "-before.json") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			cands = append(cands, candidate{path: filepath.Join(dayDir, e.Name()), mod: info.ModTime()})
		}
		sort.Slice(cands, func(a, b int) bool { return cands[a].mod.After(cands[b].mod) })
		for _, c := range cands {
			data, err := os.ReadFile(c.path)
			if err != nil {
				continue
			}
			var stored StoredSnapshot
			if err := json.Unmarshal(data, &stored); err != nil {
				continue
			}
			if session == "" || stored.Session == session {
				return &stored, nil
			}
		}
	}
	return nil, fmt.Errorf("no stored snapshot found (session %q)", session)
}

// Capture runs the injected capture function directly (used by diff tools to
// get the "live" side of a comparison).
func (s *SnapshotStore) Capture(ctx context.Context) (string, error) {
	if s == nil {
		return "", fmt.Errorf("snapshot store disabled")
	}
	return s.capture(ctx)
}
