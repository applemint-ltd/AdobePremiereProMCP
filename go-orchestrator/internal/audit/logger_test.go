package audit

import (
	"fmt"
	"sync"
	"testing"
)

func TestAuditorRecordAndQuery(t *testing.T) {
	dir := t.TempDir()
	a := NewAuditor(dir, "thread-1", "sess-abc")

	if err := a.Record(Record{CID: "aaa111", Tool: "premiere_ping", Status: "ok", DurationMs: 5}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := a.Record(Record{CID: "bbb222", Tool: "premiere_insert_clip", Status: "error", Error: "boom", Mutating: true}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	recs, err := a.Query(Filter{Session: "thread-1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("want 2 records, got %d", len(recs))
	}
	if recs[0].CID != "aaa111" || recs[1].CID != "bbb222" {
		t.Fatalf("wrong order/content: %+v", recs)
	}
	if recs[0].Session != "thread-1" || recs[0].ClaudeSession != "sess-abc" {
		t.Fatalf("session stamping missing: %+v", recs[0])
	}

	byCID, err := a.Query(Filter{CID: "bbb222"})
	if err != nil || len(byCID) != 1 || byCID[0].Error != "boom" {
		t.Fatalf("CID filter: %v %+v", err, byCID)
	}
}

func TestAuditorConcurrentAppends(t *testing.T) {
	dir := t.TempDir()
	const writers, perWriter = 8, 50

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			// Separate Auditor per goroutine mimics separate processes.
			a := NewAuditor(dir, fmt.Sprintf("s%d", w), "")
			for i := 0; i < perWriter; i++ {
				if err := a.Record(Record{CID: fmt.Sprintf("c%d-%d", w, i), Tool: "premiere_ping", Status: "ok"}); err != nil {
					t.Errorf("Record: %v", err)
				}
			}
		}(w)
	}
	wg.Wait()

	a := NewAuditor(dir, "", "")
	recs, err := a.Query(Filter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(recs) != writers*perWriter {
		t.Fatalf("lost/corrupted lines: want %d, got %d", writers*perWriter, len(recs))
	}
}

func TestNilAuditorIsNoop(t *testing.T) {
	var a *Auditor
	if err := a.Record(Record{Tool: "x"}); err != nil {
		t.Fatalf("nil Record: %v", err)
	}
	if recs, err := a.Query(Filter{}); err != nil || recs != nil {
		t.Fatalf("nil Query: %v %v", recs, err)
	}
}

func TestTruncate(t *testing.T) {
	if got := Truncate("hello", 10); got != "hello" {
		t.Fatalf("no-op truncate: %q", got)
	}
	got := Truncate("abcdefghij", 4)
	if got != "abcd...(+6 bytes)" {
		t.Fatalf("truncate: %q", got)
	}
}
