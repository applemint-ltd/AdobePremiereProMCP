package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Record is one persisted audit line describing a single MCP tool call.
// Lines are size-capped by the caller (args/result summaries truncated) so a
// full record stays well under the size macOS guarantees to be an atomic
// O_APPEND write — several MCP server processes (one per headless claude
// turn) may append to the same file concurrently.
type Record struct {
	TS             string   `json:"ts"`
	CID            string   `json:"cid"`
	Session        string   `json:"session,omitempty"`
	ClaudeSession  string   `json:"claude_session,omitempty"`
	Tool           string   `json:"tool"`
	Args           string   `json:"args,omitempty"`
	Status         string   `json:"status"` // "ok" | "error"
	Error          string   `json:"error,omitempty"`
	DurationMs     int64    `json:"duration_ms"`
	ESCalls        []ESCall `json:"es_calls,omitempty"`
	Mutating       bool     `json:"mutating"`
	SnapshotBefore string   `json:"snapshot_before,omitempty"`
	SnapshotError  string   `json:"snapshot_error,omitempty"`
	ResultSummary  string   `json:"result_summary,omitempty"`
}

// Auditor appends Records to daily JSONL files under dir. A nil Auditor (or
// one constructed with an empty dir) is a no-op, so wiring stays simple in
// tests and when auditing is disabled.
type Auditor struct {
	dir           string
	session       string
	claudeSession string
}

// NewAuditor creates an Auditor writing to dir. Returns nil if dir is empty.
// The directory is created on first use, not here, so construction never
// fails.
func NewAuditor(dir, session, claudeSession string) *Auditor {
	if dir == "" {
		return nil
	}
	return &Auditor{dir: dir, session: session, claudeSession: claudeSession}
}

// Session returns the session tag this auditor stamps on records.
func (a *Auditor) Session() string {
	if a == nil {
		return ""
	}
	return a.session
}

// Dir returns the audit directory, or "" when auditing is disabled.
func (a *Auditor) Dir() string {
	if a == nil {
		return ""
	}
	return a.dir
}

// Record appends one line to today's audit file. Errors are returned for the
// caller to log; auditing must never fail the tool call itself.
func (a *Auditor) Record(rec Record) error {
	if a == nil {
		return nil
	}
	if rec.TS == "" {
		rec.TS = time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
	}
	if rec.Session == "" {
		rec.Session = a.session
	}
	if rec.ClaudeSession == "" {
		rec.ClaudeSession = a.claudeSession
	}

	if err := os.MkdirAll(a.dir, 0o755); err != nil {
		return fmt.Errorf("audit dir: %w", err)
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("audit marshal: %w", err)
	}
	line = append(line, '\n')

	path := filepath.Join(a.dir, "audit-"+time.Now().Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("audit open: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("audit write: %w", err)
	}
	return nil
}

// Filter narrows a Query. Zero values match everything.
type Filter struct {
	Session string    // exact session tag
	Tool    string    // exact tool name
	CID     string    // exact correlation ID
	Since   time.Time // records at/after this instant
	Limit   int       // keep only the most recent N (0 = all)
}

// Query scans today's and yesterday's audit files (oldest first) and returns
// matching records in chronological order.
func (a *Auditor) Query(f Filter) ([]Record, error) {
	if a == nil {
		return nil, nil
	}
	var out []Record
	days := []string{
		time.Now().AddDate(0, 0, -1).Format("2006-01-02"),
		time.Now().Format("2006-01-02"),
	}
	for _, day := range days {
		path := filepath.Join(a.dir, "audit-"+day+".jsonl")
		recs, err := readRecords(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		out = append(out, recs...)
	}

	filtered := out[:0]
	for _, r := range out {
		if f.Session != "" && r.Session != f.Session {
			continue
		}
		if f.Tool != "" && r.Tool != f.Tool {
			continue
		}
		if f.CID != "" && r.CID != f.CID {
			continue
		}
		if !f.Since.IsZero() {
			if ts, err := time.Parse("2006-01-02T15:04:05.000Z07:00", r.TS); err == nil && ts.Before(f.Since) {
				continue
			}
		}
		filtered = append(filtered, r)
	}
	if f.Limit > 0 && len(filtered) > f.Limit {
		filtered = filtered[len(filtered)-f.Limit:]
	}
	return filtered, nil
}

// Recent returns the most recent n records for a session (all sessions when
// the tag is empty), in chronological order.
func (a *Auditor) Recent(n int, session string) ([]Record, error) {
	return a.Query(Filter{Session: session, Limit: n})
}

func readRecords(path string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var r Record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			// A torn or corrupt line must not hide the rest of the log.
			continue
		}
		out = append(out, r)
	}
	return out, sc.Err()
}

// Truncate caps s at max bytes, annotating how much was dropped.
func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return fmt.Sprintf("%s...(+%d bytes)", s[:max], len(s)-max)
}
